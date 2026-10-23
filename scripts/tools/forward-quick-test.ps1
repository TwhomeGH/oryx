#!/usr/bin/env pwsh
#
# 轉推快速測試工具 (forward quick test)
#
# 一鍵驗證「來源 → 轉推到本機目標 → 品質指標與診斷」，不需要外部平台。
# 適合快速自我檢查：程式是否運作、來源中斷時是否不斷線、指標長怎樣。
#
# 用法:
#   pwsh scripts/tools/forward-quick-test.ps1              # 正常流程（預設會開 ffplay 預覽）
#   pwsh scripts/tools/forward-quick-test.ps1 -Outage      # 加上「來源中斷」示範
#   pwsh scripts/tools/forward-quick-test.ps1 -NoPlay      # 不開預覽（無顯示環境/CI）
#   pwsh scripts/tools/forward-quick-test.ps1 -Keep        # 保留設定，方便自己點 UI 看
#
[CmdletBinding()]
param(
    [string]$Endpoint = "",
    [string]$Container = "oryx",
    [string]$Source = "",
    [string]$Rtmp = "",
    [switch]$Outage,
    [switch]$Play = $true,
    [switch]$NoPlay,
    [switch]$Normalize,
    [switch]$Slate,
    [switch]$Keep,
    [switch]$Compare
)

if ($NoPlay) { $Play = $false }

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not $Source) { $Source = Join-Path $RepoRoot "test\source.200kbps.768x320.flv" }

$script:Token = ""
$script:SourceProc = $null
$script:PlayProc = $null
$script:Platform = ""
$script:ComparePlatform = ""

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
    foreach ($platform in @($script:Platform, $script:ComparePlatform) | Where-Object { $_ }) {
        $b = $list | Where-Object { $_.platform -eq $platform }
        if (-not $b) { Write-Host "   [$platform] (no status yet)"; continue }
        $m = $b.metrics
        $line = "   {0} status={1} mode={2} fallback={3} restarts={4}" -f $b.label, $b.status, $b.programMode, $b.fallback, $b.restartCount
        if ($m) {
            $line += " | avgFps={0:N1} noOutput={1:N1}s drift={2:N1}s reconnects={3}" -f $m.avgFps, $m.noOutputSeconds, $m.mediaClockDriftSec, $m.relayReconnects
        }
        $color = if ($b.fallback) { 'Yellow' } elseif ($b.status -eq 'running') { 'Green' } elseif ($b.status -in @('retrying', 'degraded')) { 'Red' } else { 'Gray' }
        Write-Host $line -ForegroundColor $color
        if ($b.events) {
            $b.events | Select-Object -Last 3 | ForEach-Object { Write-Host ("   event {0} {1} {2}" -f $_.at, $_.name, $_.detail) -ForegroundColor DarkGray }
        }
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
    if ($script:Token -and -not $Keep) {
        foreach ($p in @($script:Platform, $script:ComparePlatform) | Where-Object { $_ }) {
            try { Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{ action = "delete"; platform = $p } | Out-Null } catch { }
        }
    }
}

# --- Preflight ---
Step "檢查前置條件"
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) { Die "找不到 ffmpeg，請先安裝（winget install Gyan.FFmpeg）。" }
if ($Play -and -not (Get-Command ffplay -ErrorAction SilentlyContinue)) {
    Write-Host "   [注意] 找不到 ffplay，預覽已停用（不影響測試；可用 -NoPlay 明確關閉）。" -ForegroundColor DarkYellow
    $Play = $false
}
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
# -Keep reuses one fixed set of targets so repeated runs do not pile up new "TEST" entries; a
# transient run uses the PID so parallel/unattended runs never collide.
$suffix = if ($Keep) { "keep" } else { "$PID" }
$script:Platform = "forwarding-quick-$suffix"
$script:SrcID = "quick-src-$suffix"
$script:TgtID = "quick-tgt-$suffix"

Write-Host "   endpoint=$Endpoint  rtmp=$Rtmp  source=$Source"
Write-Host "   pubSecret=($($script:Pub.Length) bytes)  apiSecret=($($script:Token.Length) bytes)"

try {
    Step "啟動本機來源 $Rtmp/live/$($script:SrcID)"
    Start-Source
    Start-Sleep -Seconds 3

    $fmode = if ($Slate) { "slate" } else { "hold" }
    $label = if ($Compare) { "TEST quick (copy)" } elseif ($Normalize) { "TEST quick (normalize)" } else { "TEST quick" }
    Step "啟用轉推：本機目標 rtmp://localhost/live/$($script:TgtID)（$label）failover=$fmode"
    Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
        action = "update"; platform = $script:Platform; server = "rtmp://localhost/live/"; secret = $script:TgtID; enabled = $true; custom = $true; label = $label; normalize = (-not $Compare -and [bool]$Normalize); failover = $(if ($Slate) { "slate" } else { "" })
    } | Out-Null

    if ($Compare) {
        $script:CompareTgtID = "quick-cmp-$suffix"
        $script:ComparePlatform = "forwarding-quick-cmp-$suffix"
        Step "啟用對照目標：rtmp://localhost/live/$($script:CompareTgtID)（TEST quick (normalize)：開啟補強轉碼 CFR）"
        Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
            action = "update"; platform = $script:ComparePlatform; server = "rtmp://localhost/live/"; secret = $script:CompareTgtID; enabled = $true; custom = $true; label = "TEST quick (normalize)"; normalize = $true; failover = ""
        } | Out-Null
    }

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

    if ($Compare) {
        Step "等待並觀察指標（約 12 秒）：對照「copy 不轉碼」與「normalize CFR 補強」的 avgFps / drift / noOutput"
    } else {
        Step "等待並觀察指標（約 12 秒）"
    }
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

    Step "完成。轉推輸出（HTTP-FLV）：$playURL"
    if ($Compare) {
        $flvA = "$Endpoint/live/$($script:TgtID).flv"
        $flvB = "$Endpoint/live/$($script:CompareTgtID).flv"
        Write-Host ""
        Write-Host "   畫質對比用這兩個網址（複製即可）：" -ForegroundColor Cyan
        Write-Host "     copy      (A): $flvA" -ForegroundColor Green
        Write-Host "     normalize (B): $flvB" -ForegroundColor Green
        Write-Host "   指令：" -ForegroundColor Cyan
        Write-Host "     pwsh scripts/tools/forward-quality-compare.ps1 -A `"$flvA`" -B `"$flvB`" -Seconds 20" -ForegroundColor Gray
    }
    if (-not $Play -and -not $NoPlay) {
        Write-Host "   看畫面：預設會自動開 ffplay 預覽；若沒開（找不到 ffplay），可把上面的 HTTP-FLV 網址貼到播放器。" -ForegroundColor Yellow
    }
    if ($Keep) {
        Write-Host "   已保留設定（-Keep）：UI 檢視請開 $Endpoint → 場景 → 轉推 → 展開「狀態」→ 該列『本機輸出進度』（含診斷時間線/近期日誌）。" -ForegroundColor Yellow
    } else {
        Write-Host "   測試設定已清理（目標已刪除）；若想在 UI 看診斷，請加 -Keep 重跑一次。" -ForegroundColor DarkGray
    }
} finally {
    if (-not $Keep) {
        Step "清理"; Cleanup
    } else {
        # -Keep keeps the source AND the target so you can inspect/compare them afterwards;
        # only the ffplay preview is stopped.
        Stop-Play
        Step "已保留來源與目標（-Keep）：來源持續推流，可繼續用播放器或畫質對比工具觀察。"
        Write-Host "   停止來源：$Rtmp/live/$($script:SrcID) 或關閉此終端機前自行結束 ffmpeg。" -ForegroundColor Yellow
    }
}
