#!/usr/bin/env pwsh
#
# 轉推畫質對比 (forward-quality-compare)
#
# 對兩個輸出流（例如 copy 與 normalize，或不同碼率）取同一段時間，計算 PSNR/SSIM 並輸出截圖，
# 用來量化「補強轉碼」的畫質損失。
#
# 用法:
#   pwsh scripts/tools/forward-quality-compare.ps1 -A http://host/a.flv -B http://host/b.flv
#   pwsh scripts/tools/forward-quality-compare.ps1 -A ... -B ... -Seconds 20 -Out D:\tmp\cmp
#
[CmdletBinding()]
param(
    [string]$A = "",
    [string]$B = "",
    # Create two TEST targets from this production source (e.g. live/livestream) and clean them up.
    [string]$Source = "",
    [int]$Seconds = 15,
    [string]$Out = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not $Out) { $Out = Join-Path $RepoRoot ".tmp\forward-quality-compare" }
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

function Section($msg) { Write-Host ""; Write-Host "==> $msg" -ForegroundColor Cyan }

if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Host "找不到 ffmpeg。" -ForegroundColor Red; exit 1
}

# Build the comparison targets from a production source when -Source is given.
if ($Source) {
    if (-not $env:ORYX_API) { $env:ORYX_API = "http://localhost:882" }
    $api = $env:ORYX_API.TrimEnd('/')
    $token = docker exec oryx redis-cli hget SRS_PLATFORM_SECRET token
    if (-not $token) { Write-Host "讀不到平台密鑰（容器 oryx 未執行？）" -ForegroundColor Red; exit 1 }
    $hdr = @{ Authorization = "Bearer $token" }
    $suffix = Get-Random -Maximum 99999
    $pA = "forwarding-qcA-$suffix"
    $pB = "forwarding-qcB-$suffix"
    function NewTarget($platform, $label, $normalize) {
        $body = @{
            action   = "update"
            platform = $platform
            stream   = ($Source -split '/')[-1]
            server   = "rtmp://localhost/live"
            secret   = $platform
            enabled  = $true
            custom   = $true
            label    = $label
            failover = "hold"
            normalize = $normalize
        } | ConvertTo-Json -Compress
        Invoke-RestMethod -Method Post -Uri "$api/terraform/v1/ffmpeg/forward/secret" -Headers $hdr -ContentType "application/json" -Body $body | Out-Null
    }
    # Safety guard: creating targets from a live source would also forward it to any other enabled
    # real target. Abort unless the operator explicitly opts in.
    $all = (Invoke-RestMethod -Method Post -Uri "$api/terraform/v1/ffmpeg/forward/streams" -Headers $hdr -ContentType "application/json" -Body '{"action":"query"}').data
    $foreign = @($all | Where-Object { $_.enabled -and $_.label -notlike "TEST *" })
    if ($foreign.Count -gt 0) {
        $list = ($foreign | ForEach-Object { "  - $($_.platform)  label=$($_.label)" }) -join "`n"
        Write-Host "測試已中止：以下正式轉播目標仍為啟用狀態，測試流量會被推到真實平台。" -ForegroundColor Red
        Write-Host $list -ForegroundColor Yellow
        Write-Host "請先停用它們再重跑。" -ForegroundColor Red
        exit 1
    }

    Section "建立對比目標（來源：$Source）"
    NewTarget $pA "TEST qc copy" $false
    NewTarget $pB "TEST qc normalize" $true
    Write-Host "  $pA (copy)" -ForegroundColor Green
    Write-Host "  $pB (normalize)" -ForegroundColor Green
    $A = "$api/live/$pA.flv"
    $B = "$api/live/$pB.flv"
    Write-Host "  等待出流（最多 40 秒）..." -ForegroundColor DarkGray
    $ready = $false
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Seconds 2
        $st = (Invoke-RestMethod -Method Post -Uri "$api/terraform/v1/ffmpeg/forward/streams" -Headers $hdr -ContentType "application/json" -Body '{"action":"query"}').data |
            Where-Object { $_.platform -in @($pA, $pB) }
        $running = @($st | Where-Object { $_.status -eq 'running' }).Count
        if ($running -eq 2) { $ready = $true; break }
        if ($i -eq 9 -or $i -eq 19) {
            Write-Host "    目前狀態：$((($st | ForEach-Object { "$($_.platform)=$($_.status)" }) -join ', '))" -ForegroundColor DarkYellow
        }
    }
    if (-not $ready) {
        Write-Host "  兩邊未同時進入 running。請確認來源 '$Source' 正在推流，或先看控制台的轉播狀態。" -ForegroundColor Red
    }
    $cleanup = $true
}

# SRS HTTP-FLV requires the .flv suffix; accept a URL without it and fix it up.
if (-not $A -or -not $B) {
    Write-Host "請提供 -A/-B，或使用 -Source <app/stream> 由工具建立對比目標。" -ForegroundColor Red
    exit 1
}
if ($A -notmatch "\.(flv|m3u8|ts)(\?|$)") { $A = "$A.flv" }
if ($B -notmatch "\.(flv|m3u8|ts)(\?|$)") { $B = "$B.flv" }
Write-Host "A = $A"
Write-Host "B = $B"

New-Item -ItemType Directory -Force -Path $Out | Out-Null

Section "錄製 ${Seconds}s 成檔（避免 live 串流時序對不上）"
$fileA = Join-Path $Out "A.mp4"
$fileB = Join-Path $Out "B.mp4"
& ffmpeg -hide_banner -loglevel error -y -t $Seconds -i $A -c copy $fileA 2>&1 | Select-Object -Last 2
& ffmpeg -hide_banner -loglevel error -y -t $Seconds -i $B -c copy $fileB 2>&1 | Select-Object -Last 2
if (-not (Test-Path $fileA) -or (Get-Item $fileA).Length -lt 1024) { Write-Host "  A 錄製失敗（檔案過小）" -ForegroundColor Red }
if (-not (Test-Path $fileB) -or (Get-Item $fileB).Length -lt 1024) { Write-Host "  B 錄製失敗（檔案過小）" -ForegroundColor Red }

Section "量測 PSNR / SSIM（對已錄檔，逐幀對齊）"
# Compare the recorded files so both start at their own first frame.
$metrics = & ffmpeg -hide_banner -loglevel info -i $fileA -i $fileB `
    -lavfi "[0:v]setpts=PTS-STARTPTS[a];[1:v]setpts=PTS-STARTPTS[b];[a][b]psnr" `
    -lavfi "[0:v]setpts=PTS-STARTPTS[a2];[1:v]setpts=PTS-STARTPTS[b2];[a2][b2]ssim" -f null - 2>&1
$found = $metrics | Where-Object { $_ -match 'PSNR|SSIM' }
if (-not $found) {
    Write-Host "  讀不到 PSNR/SSIM：請確認兩個網址都可播放（HTTP-FLV 需 .flv 結尾，且來源仍在推流）。" -ForegroundColor Red
    $metrics | Select-Object -Last 8 | ForEach-Object { Write-Host "  $_" -ForegroundColor DarkGray }
} else {
    $found | ForEach-Object { Write-Host "  $_" -ForegroundColor Green }
}

Section "輸出多幀拼貼（各 30 格，可肉眼比對）"
# 30 tiles across the recording; the recorded files are seekable so this is deterministic.
$tiles = "fps=30/$Seconds,scale=320:-1,tile=6x5:padding=2:margin=2"
& ffmpeg -hide_banner -loglevel error -y -i $fileA -vf $tiles -frames:v 1 -update 1 (Join-Path $Out "A-tiles.png") 2>&1 | Out-Null
& ffmpeg -hide_banner -loglevel error -y -i $fileB -vf $tiles -frames:v 1 -update 1 (Join-Path $Out "B-tiles.png") 2>&1 | Out-Null
Write-Host "  A-tiles.png / B-tiles.png（6x5 拼貼）"
Write-Host "  A: $(Join-Path $Out 'A.png')"
Write-Host "  B: $(Join-Path $Out 'B.png')"

Section "檔案大小"
$pngs = @(Get-ChildItem $Out -Filter *.png -ErrorAction SilentlyContinue)
Write-Host "  錄製檔：A.mp4 = $([math]::Round((Get-Item $fileA -ErrorAction SilentlyContinue).Length/1KB)) KB, B.mp4 = $([math]::Round((Get-Item $fileB -ErrorAction SilentlyContinue).Length/1KB)) KB"
if ($pngs.Count -eq 0) {
    Write-Host "  沒有產生截圖（兩邊都讀不到畫面）。" -ForegroundColor Red
} else {
    $pngs | ForEach-Object { "  {0}  {1:N0} bytes" -f $_.Name, $_.Length }
}

Write-Host ""
Write-Host "提示：PSNR 越高越好（>35dB 通常難以察覺差異）；SSIM 越接近 1 越好。" -ForegroundColor Yellow

if ($cleanup) {
    Section "清理對比目標"
    foreach ($p in @($pA, $pB)) {
        try {
            $body = @{ action = "delete"; platform = $p } | ConvertTo-Json -Compress
            Invoke-RestMethod -Method Post -Uri "$api/terraform/v1/ffmpeg/forward/secret" -Headers $hdr -ContentType "application/json" -Body $body | Out-Null
            Write-Host "  已刪除 $p" -ForegroundColor DarkGray
        } catch { Write-Host "  刪除 $p 失敗：$_" -ForegroundColor Red }
    }
}