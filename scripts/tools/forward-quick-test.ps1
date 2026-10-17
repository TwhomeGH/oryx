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
    [string]$Endpoint = "",
    [string]$Container = "oryx",
    [string]$Source = "",
    [string]$Rtmp = "",
    [switch]$Outage,
    [switch]$Play,
    [switch]$Normalize,
    [switch]$Slate,
    [switch]$Keep
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not $Source) { $Source = Join-Path $RepoRoot "test\source.200kbps.768x320.flv" }

$script:Token = ""
$script:SourceProc = $null
$script:PlayProc = $null
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

# Wait until the target is actually publishing frames, so ffplay does not open too early (the
# stream is not live yet) and exit immediately.
function Wait-Target([int]$TimeoutSec = 30) {
    for ($i = 0; $i -lt $TimeoutSec; $i++) {
        $list = Api "GET" "/terraform/v1/ffmpeg/forward/streams" $null
        $b = $list | Where-Object { $_.platform -eq $script:Platform }
        if ($b -and $b.status -eq "running" -and $b.metrics -and [int]$b.metrics.frame -gt 0) { return $true }
        Start-Sleep -Seconds 1
    }
    return $false
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

function Stop-Play() {
    if ($script:PlayProc -and -not $script:PlayProc.HasExited) {
        Stop-Process -Id $script:PlayProc.Id -Force -ErrorAction SilentlyContinue
    }
    $script:PlayProc = $null
}

function Cleanup() {
    Stop-Source
    Stop-Play
    if ($script:Token -and $script:Platform -and -not $Keep) {
        try {
            Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
                action = "delete"; platform = $script:Platform
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

# Auto-detect the host ports from the container mapping, because the published ports vary (e.g.
# the default image maps 2022/1935, but a custom deploy may map them elsewhere).
function Detect-Port($containerPort, $scheme, $fallbackPort) {
    $m = docker port $Container $containerPort 2>$null | Select-Object -First 1
    if ($m) { return "$scheme`://localhost:$((($m) -split ':')[-1])" }
    return "$scheme`://localhost:$fallbackPort"
}
if (-not $Endpoint) { $Endpoint = Detect-Port 2022 "http" 2022 }
if (-not $Rtmp) { $Rtmp = Detect-Port 1935 "rtmp" 1935 }

# Use a dedicated custom target ("forwarding-" prefix so it can be deleted) instead of a
# built-in platform like bilibili, to never overwrite the user's real forward config.
$script:Platform = "forwarding-quick-$PID"
$script:SrcID = "quick-src-$PID"
$script:TgtID = "quick-tgt-$PID"

Write-Host "   endpoint=$Endpoint  rtmp=$Rtmp  source=$Source"
Write-Host "   pubSecret=($($script:Pub.Length) bytes)  apiSecret=($($script:Token.Length) bytes)"

try {
    Step "啟動本機來源 $Rtmp/live/$($script:SrcID)"
    Start-Source
    Start-Sleep -Seconds 3

    $fmode = if ($Slate) { "slate" } else { "hold" }
    Step "啟用轉推：本機目標 rtmp://localhost/live/$($script:TgtID) normalize=$([bool]$Normalize) failover=$fmode"
    Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
        action = "update"; platform = $script:Platform; server = "rtmp://localhost/live/"; secret = $script:TgtID; enabled = $true; custom = $true; label = "TEST quick"; normalize = [bool]$Normalize; failover = $(if ($Slate) { "slate" } else { "" })
    } | Out-Null

    $playURL = "$Endpoint/live/$($script:TgtID).flv"
    if ($Play) {
        Step "先等目標有畫面，再開 ffplay 預覽：$playURL"
        if (-not (Wait-Target 30)) { Write-Host "   [警告] 目標 30 秒內未就緒，仍嘗試播放" -ForegroundColor DarkYellow }
        $script:PlayErr = Join-Path $env:TEMP "forward-quick-test-play.log"
        $script:PlayProc = Start-Process ffplay -ArgumentList @(
            "-loglevel", "warning", "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
            "-window_title", "forward-tgt-$($script:TgtID)", "-i", $playURL
        ) -PassThru -RedirectStandardError $script:PlayErr
    }

    Step "等待並觀察指標（約 12 秒）"
    for ($i = 0; $i -lt 4; $i++) { Start-Sleep -Seconds 3; Show-Status }

    if ($Outage) {
        Step "-Outage：中斷來源，觀察下游是否不斷線（應 fallback=True、restart 不變、noOutput 上升）；failover=$fmode → $(if ($Slate) { '看到深灰底 + 跳動時鐘' } else { '畫面凍結在最後一格' })"
        Stop-Source
        for ($i = 0; $i -lt 3; $i++) { Start-Sleep -Seconds 3; Show-Status }
        Step "-Outage：恢復來源"
        Start-Source
        for ($i = 0; $i -lt 3; $i++) { Start-Sleep -Seconds 3; Show-Status }
    }

    if ($Play -and $script:PlayProc -and $script:PlayProc.HasExited) {
        Write-Host "   [注意] ffplay 已結束（可能沒抓到流），日誌：$($script:PlayErr)" -ForegroundColor DarkYellow
        if (Test-Path $script:PlayErr) { Get-Content $script:PlayErr -Tail 6 | ForEach-Object { Write-Host "     $_" -ForegroundColor DarkGray } }
    }

    Step "完成。輸出可播放（HTTP-FLV）：$playURL"
    Write-Host "   UI 檢視：開啟 $Endpoint → 場景 → 轉推 → 展開「狀態」→ 該列『本機輸出進度』（含診斷時間線/近期日誌）。" -ForegroundColor Yellow
    if ($Keep) { Write-Host "   已保留設定（-Keep），要清理請自行停用該轉推。" -ForegroundColor Yellow }
} finally {
    if (-not $Keep) { Step "清理"; Cleanup } else { Stop-Source; Stop-Play }
}
