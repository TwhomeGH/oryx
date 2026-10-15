#!/usr/bin/env pwsh
#
# 轉推快速測試工具 (forward quick test)
#
# 一鍵驗證「來源 → 轉推到本機目標 → 品質指標與診斷」，不需要外部平台。
# 適合快速自我檢查：程式是否運作、來源中斷時是否不斷線、指標長怎樣。
#
# 用法:
#   pwsh scripts/tools/forward-quick-test.ps1              # 快樂路徑
#   pwsh scripts/tools/forward-quick-test.ps1 -Outage      # 加上「來源中斷」示範
#   pwsh scripts/tools/forward-quick-test.ps1 -Keep        # 保留設定，方便自己點 UI 看
#
[CmdletBinding()]
param(
    [string]$Endpoint = "http://localhost:2022",
    [string]$Container = "oryx",
    [string]$Source = "",
    [string]$Rtmp = "rtmp://localhost:1935",
    [switch]$Outage,
    [switch]$Keep
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not $Source) { $Source = Join-Path $RepoRoot "test\source.200kbps.768x320.flv" }

$script:Token = ""
$script:SourceProc = $null
$script:Platform = ""

function Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }

function Die($msg) { Write-Host "ERROR: $msg" -ForegroundColor Red; exit 1 }

# Call a platform management API with the bearer token, returning the `data` field.
function Api($method, $path, $body) {
    $headers = @{ Authorization = "Bearer $script:Token" }
    $uri = "$Endpoint$path"
    if ($null -ne $body) {
        return (Invoke-RestMethod -Method $method -Uri $uri -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json -Compress)).data
    }
    return (Invoke-RestMethod -Method $method -Uri $uri -Headers $headers).data
}

function Get-RedisField($hash, $field) {
    $v = (docker exec $Container redis-cli hget $hash $field 2>$null)
    if ($v) { return ("$v").Trim() }
    return ""
}

function Show-Status() {
    $list = Api "GET" "/terraform/v1/ffmpeg/forward/streams" $null
    $b = $list | Where-Object { $_.platform -eq $script:Platform }
    if (-not $b) { Write-Host "   (no status yet)"; return }
    $m = $b.metrics
    $line = "   status={0} mode={1} fallback={2} restarts={3}" -f $b.status, $b.programMode, $b.fallback, $b.restartCount
    if ($m) {
        $line += " | avgFps={0:N1} noOutput={1:N1}s drift={2:N1}s reconnects={3}" -f $m.avgFps, $m.noOutputSeconds, $m.mediaClockDriftSec, $m.relayReconnects
    }
    Write-Host $line
    if ($b.events) {
        $b.events | Select-Object -Last 3 | ForEach-Object { Write-Host ("   event {0} {1} {2}" -f $_.at, $_.name, $_.detail) -ForegroundColor DarkGray }
    }
}

function Start-Source() {
    $url = "$Rtmp/live/$($script:SrcID)?secret=$($script:Pub)"
    $err = Join-Path $env:TEMP "forward-quick-test-src.log"
    $script:SourceProc = Start-Process ffmpeg -ArgumentList @(
        "-re", "-stream_loop", "-1", "-i", $Source, "-c", "copy", "-f", "flv", $url
    ) -PassThru -WindowStyle Hidden -RedirectStandardError $err
}

function Stop-Source() {
    if ($script:SourceProc -and -not $script:SourceProc.HasExited) {
        Stop-Process -Id $script:SourceProc.Id -Force -ErrorAction SilentlyContinue
    }
    $script:SourceProc = $null
}

function Cleanup() {
    Stop-Source
    if ($script:Token -and $script:Platform -and -not $Keep) {
        try {
            Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
                action = "update"; platform = $script:Platform; server = "rtmp://localhost/live/"; secret = "quick-test"; enabled = $false; custom = $true; label = "quick-test"
            } | Out-Null
        } catch { }
    }
}

# --- Preflight ---
Step "檢查前置條件"
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) { Die "找不到 ffmpeg，請先安裝（winget install Gyan.FFmpeg）。" }
if (-not (Test-Path $Source)) { Die "找不到來源檔：$Source" }
$running = (docker ps --filter "name=^/$Container$" --format "{{.Names}}")
if (-not $running) { Die "找不到執行中的容器 '$Container'。請先啟動平台，或改用 -Container <名稱>。" }

Step "讀取平台密鑰（容器 $Container）"
$script:Token = Get-RedisField "SRS_PLATFORM_SECRET" "token"
$script:Pub = Get-RedisField "SRS_AUTH_SECRET" "pubSecret"
if (-not $script:Token) { Die "讀不到 SRS_PLATFORM_SECRET。平台是否已啟動？" }

$script:Platform = "bilibili"
$script:SrcID = "quick-src-$PID"
$script:TgtID = "quick-tgt-$PID"

Write-Host "   endpoint=$Endpoint  source=$Source"
Write-Host "   pubSecret=($($script:Pub.Length) bytes)  apiSecret=($($script:Token.Length) bytes)"

try {
    Step "啟動本機來源 $Rtmp/live/$($script:SrcID)"
    Start-Source
    Start-Sleep -Seconds 3

    Step "啟用轉推：本機目標 rtmp://localhost/live/$($script:TgtID)"
    Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
        action = "update"; platform = $script:Platform; server = "rtmp://localhost/live/"; secret = "$($script:TgtID)?secret=$($script:Pub)"; enabled = $true; custom = $true; label = "quick-test"
    } | Out-Null

    Step "等待並觀察指標（約 12 秒）"
    for ($i = 0; $i -lt 4; $i++) { Start-Sleep -Seconds 3; Show-Status }

    if ($Outage) {
        Step "-Outage：中斷來源，觀察下游是否不斷線（應 fallback=True、restart 不變、noOutput 上升）"
        Stop-Source
        for ($i = 0; $i -lt 3; $i++) { Start-Sleep -Seconds 3; Show-Status }
        Step "-Outage：恢復來源"
        Start-Source
        for ($i = 0; $i -lt 3; $i++) { Start-Sleep -Seconds 3; Show-Status }
    }

    Step "完成。輸出可播放（HTTP-FLV）：$Endpoint/live/$($script:TgtID).flv"
    Write-Host "   UI 檢視：開啟 $Endpoint → 場景 → 轉推 → 展開「狀態」→ 該列『本機輸出進度』（含診斷時間線/近期日誌）。" -ForegroundColor Yellow
    if ($Keep) { Write-Host "   已保留設定（-Keep），要清理請自行停用該轉推。" -ForegroundColor Yellow }
} finally {
    if (-not $Keep) { Step "清理"; Cleanup } else { Stop-Source }
}
