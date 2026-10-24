<#
.SYNOPSIS
  比較兩條流的畫質（PSNR / SSIM / VMAF），用於評估補強轉碼或碼率調整的損失。

.DESCRIPTION
  同時錄製兩條流（避免動態來源錄到不同內容），以 -ss 搜尋最佳時間偏移後，對整段做
  多幀平均。VMAF 為主要指標（人眼感知），PSNR/SSIM 為輔。也輸出拼貼圖供人工複核。

.PARAMETER A
  參考流（通常是不轉碼的 copy）。與 -B 成對使用。

.PARAMETER B
  待比較流（通常是 normalize 或不同碼率）。與 -A 成對使用。

.PARAMETER Source
  用正式來源（app/stream，例如 live/livestream）自動建立 copy/normalize 兩個 TEST 目標，
  比對完自動刪除。會先檢查是否有非 TEST 的目標啟用，避免測試流量外推。

.PARAMETER Seconds
  每邊錄製秒數（預設 15）。越長越能涵蓋大動態，但耗時更久。

.PARAMETER Out
  輸出目錄（A.mp4/B.mp4、A-tiles.png/B-tiles.png 拼貼圖）。
  預設 <repo>\.tmp\forward-quality-compare。

.EXAMPLE
  pwsh scripts/tools/forward-quality-compare.ps1 -A "http://localhost:882/live/copy.flv" -B "http://localhost:882/live/norm.flv"

.EXAMPLE
  pwsh scripts/tools/forward-quality-compare.ps1 -Source live/livestream -Seconds 20

.NOTES
  SRS HTTP-FLV 網址需以 .flv 結尾（缺少時工具會自動補上）。
#>
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

Section "同時錄製 ${Seconds}s（兩條流同一個 wall clock 起點）"
# Record both streams at the same time: recording them one after another samples different
# content of a moving source, so the comparison would be meaningless.
$fileA = Join-Path $Out "A.mp4"
$fileB = Join-Path $Out "B.mp4"
$jobA = Start-Job -ScriptBlock { param($u,$f,$s) & ffmpeg -hide_banner -loglevel error -y -t $s -i $u -c copy $f 2>&1 } -ArgumentList $A, $fileA, $Seconds
$jobB = Start-Job -ScriptBlock { param($u,$f,$s) & ffmpeg -hide_banner -loglevel error -y -t $s -i $u -c copy $f 2>&1 } -ArgumentList $B, $fileB, $Seconds
Wait-Job $jobA, $jobB | Out-Null
Remove-Job $jobA, $jobB
if (-not (Test-Path $fileA) -or (Get-Item $fileA).Length -lt 1024) { Write-Host "  A 錄製失敗（檔案過小）" -ForegroundColor Red }
if (-not (Test-Path $fileB) -or (Get-Item $fileB).Length -lt 1024) { Write-Host "  B 錄製失敗（檔案過小）" -ForegroundColor Red }

Section "尋找內容對齊偏移（兩條流管線延遲不同）"
# The two pipelines start at different points of the source, so frame N of A and frame N of B are
# different pictures. Search a wide offset window and keep the shift with the highest PSNR.
$bestOffset = 0.0
$bestPsnr = -1.0
$coarseList = 0.0, 0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0, 12.0, 15.0
foreach ($sec in $coarseList) {
    $probe = & ffmpeg -hide_banner -loglevel info -ss $sec -i $fileB -i $fileA `
        -t 8 -lavfi "[0:v]setpts=PTS-STARTPTS[a];[1:v]setpts=PTS-STARTPTS[b];[a][b]psnr" -f null - 2>&1
    $line = $probe | Where-Object { $_ -match "PSNR .*average:" } | Select-Object -Last 1
    if ($line -and $line -match "average:([0-9.]+)") {
        $v = [double]$Matches[1]
        if ($v -gt $bestPsnr) { $bestPsnr = $v; $bestOffset = $sec }
    }
}
# Second pass: refine +/-0.4s in 0.1s steps around the coarse best.
$coarseBest = $bestOffset
foreach ($delta in -0.4, -0.3, -0.2, -0.1, 0.1, 0.2, 0.3, 0.4) {
    $sec = [Math]::Round($coarseBest + $delta, 1)
    if ($sec -lt 0) { continue }
    $probe = & ffmpeg -hide_banner -loglevel info -ss $sec -i $fileB -i $fileA `
        -t 8 -lavfi "[0:v]setpts=PTS-STARTPTS[a];[1:v]setpts=PTS-STARTPTS[b];[a][b]psnr" -f null - 2>&1
    $line = $probe | Where-Object { $_ -match "PSNR .*average:" } | Select-Object -Last 1
    if ($line -and $line -match "average:([0-9.]+)") {
        $v = [double]$Matches[1]
        if ($v -gt $bestPsnr) { $bestPsnr = $v; $bestOffset = $sec }
    }
}
Write-Host ("  最佳偏移：{0:N1}s（對齊後 PSNR {1:N2} dB）" -f $bestOffset, $bestPsnr) -ForegroundColor Cyan
if ($bestPsnr -lt 25) {
    Write-Host "  警告：即使最佳偏移下 PSNR 仍偏低，兩條流可能不是同一段內容（請確認來源與目標設定）。" -ForegroundColor Yellow
}

Section "VMAF（人眼感知，主要指標）"
# VMAF tolerates sub-frame timing far better than PSNR/SSIM, so it is the primary number.
$vmaf = & ffmpeg -hide_banner -loglevel info -ss $bestOffset -i $fileB -i $fileA `
    -lavfi "[0:v]setpts=PTS-STARTPTS[a];[1:v]setpts=PTS-STARTPTS[b];[a][b]libvmaf" -f null - 2>&1
$vmafLine = $vmaf | Where-Object { $_ -match "VMAF score" } | Select-Object -Last 1
if ($vmafLine -and $vmafLine -match "VMAF score:\s*([0-9.]+)") {
    $score = [double]$Matches[1]
    $verdict = if ($score -ge 93) { "幾乎無感" } elseif ($score -ge 80) { "輕微差異" } elseif ($score -ge 60) { "可接受" } else { "明顯差異" }
    Write-Host ("  VMAF: {0:N2}（{1}）" -f $score, $verdict) -ForegroundColor Cyan
} else {
    Write-Host "  這個 ffmpeg 沒有 libvmaf，改用 PSNR/SSIM。" -ForegroundColor Yellow
}

Section "量測 PSNR / SSIM（整段多幀平均）"
# Average over the whole aligned segment, not a single frame.
$metrics = & ffmpeg -hide_banner -loglevel info -ss $bestOffset -i $fileB -i $fileA `
    -lavfi "[0:v]setpts=PTS-STARTPTS[a];[1:v]setpts=PTS-STARTPTS[b];[a][b]psnr" `
    -lavfi "[0:v]setpts=PTS-STARTPTS[a2];[1:v]setpts=PTS-STARTPTS[b2];[a2][b2]ssim" -f null - 2>&1
$found = $metrics | Where-Object { $_ -match "PSNR|SSIM" }
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
Write-Host "VMAF：≥93 幾乎無感、≥80 輕微差異、≥60 可接受、<60 明顯差異（人眼感知，比 PSNR 可靠）。" -ForegroundColor Yellow

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
