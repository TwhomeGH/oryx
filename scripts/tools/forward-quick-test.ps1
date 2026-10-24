<#
.SYNOPSIS
  一鍵啟動本機來源與轉推目標，快速確認轉推管線可運作。

.DESCRIPTION
  啟動一個測試來源（預設內建 200kbps 樣本），建立 TEST 目標，印出狀態/指標/事件，
  可選中斷來源（-Outage）、備援畫面（-Slate）、預覽（-Play）。

.PARAMETER Source
  測試來源的本機檔案路徑（舊名稱；-InputFile 為新版）。

.PARAMETER InputFile
  指定本機媒體檔作為測試來源（覆寫內建樣本）。建議用高碼率 CFR 樣本，才測得出畫質差異。

.PARAMETER Endpoint
  Oryx 平台網址，預設自動偵測容器對外埠。

.PARAMETER Container
  Docker 容器名稱，預設 oryx。

.PARAMETER Rtmp
  RTMP 推流位址，預設自動偵測。

.PARAMETER Outage
  測試來源中斷與恢復，觀察下游是否維持（hold 凍結 / slate 時鐘）。

.PARAMETER Play
  開啟 ffplay 預覽（預設開啟；-NoPlay 可關閉）。

.PARAMETER NoPlay
  不開啟預覽（適合 CI 或無顯示環境）。

.PARAMETER Normalize
  （已廢除）補強轉碼已改為全域設定，此參數不再影響。

.PARAMETER Slate
  使用 slate 備援模式（composite，顯示時鐘與說明文字）。

.PARAMETER Keep
  保留來源與目標，方便到 UI 查看。搭配 -KeepMinutes 自動停止。

.PARAMETER KeepMinutes
  -Keep 的保留時間（預設 10 分鐘，0 = 不限）。

.PARAMETER TakeOverRealTargets
  允許測試暫時停用非 TEST 的正式目標，結束後復原。預設關閉（有正式目標啟用時直接中止）。

.PARAMETER Compare
  對照模式：提示以全域 normalize 開/關各錄一次（per-target normalize 已無效）。

.EXAMPLE
  pwsh scripts/tools/forward-quick-test.ps1

.EXAMPLE
  pwsh scripts/tools/forward-quick-test.ps1 -InputFile "C:\videos\clean-60.mp4" -Keep -KeepMinutes 3

.EXAMPLE
  pwsh scripts/tools/forward-quick-test.ps1 -Slate -Outage -Play
#>
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
    # Explicit local media file to publish as the test source (overrides the built-in 200kbps sample).
    [string]$InputFile = "",
    # With -Keep, stop the source and the target after this many minutes (default 10, 0 = no limit).
    [int]$KeepMinutes = 10,
    [string]$Rtmp = "",
    [switch]$Outage,
    [switch]$Play,
    [switch]$NoPlay,
    [switch]$Normalize,
    [switch]$Slate,
    [switch]$Keep,
# Opt-in: temporarily disable real (non-TEST) forward targets for the duration of the test and
# restore them afterwards. Off by default so a test never touches production settings.
[switch]$TakeOverRealTargets,
    [switch]$Compare
)

# Preview is on by default; -NoPlay disables it (a switch cannot default to $true per PSScriptAnalyzer).
$preview = -not $NoPlay

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if ($InputFile) { $Source = $InputFile }
if (-not $Source) { $Source = Join-Path $RepoRoot "test\source.200kbps.768x320.flv" }
if (-not (Test-Path $Source)) { Die "找不到來源檔案：$Source" }

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
    foreach ($platform in @($script:Platform) | Where-Object { $_ }) {
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
        "-re", "-stream_loop", "-1", "-i", "`"$Source`"", "-c", "copy", "-f", "flv", $url
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
        foreach ($p in @($script:Platform) | Where-Object { $_ }) {
            try { Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{ action = "delete"; platform = $p } | Out-Null } catch { }
        }
    }
}

# --- Preflight ---
Step "檢查前置條件"
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) { Die "找不到 ffmpeg，請先安裝（winget install Gyan.FFmpeg）。" }
if ($preview -and -not (Get-Command ffplay -ErrorAction SilentlyContinue)) {
    Write-Host "   [注意] 找不到 ffplay，預覽已停用（不影響測試；可用 -NoPlay 明確關閉）。" -ForegroundColor DarkYellow
    $preview = $false
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
function Get-Port($containerPort, $scheme, $fallbackPort) {
    $m = docker port $Container $containerPort 2>$null | Select-Object -First 1
    if ($m) { return "$scheme`://localhost:$((($m) -split ':')[-1])" }
    return "$scheme`://localhost:$fallbackPort"
}
if (-not $Endpoint) { $Endpoint = Get-Port 2022 "http" 2022 }
if (-not $Rtmp) { $Rtmp = Get-Port 1935 "rtmp" 1935 }

# Use a dedicated custom target ("forwarding-" prefix so it can be deleted) instead of a
# built-in platform like bilibili, to never overwrite the user's real forward config.
# -Keep reuses one fixed set of targets so repeated runs do not pile up new "TEST" entries; a
# transient run uses the PID so parallel/unattended runs never collide.
$suffix = if ($Keep) { "keep" } else { "$PID" }
$script:Platform = "forwarding-quick-$suffix"
$script:SrcID = "quick-src-$suffix"
$script:TgtID = "quick-tgt-$suffix"

# Safety guard: a test stream must never reach a real platform. If any non-test target is enabled
# (Twitch/YouTube/...), the program would be forwarded to it as soon as the source publishes.
$allTargets = Api "GET" "/terraform/v1/ffmpeg/forward/streams" $null
# Identify test targets by the "TEST " label convention (used by quick-test, latency and
# quality-compare), not by platform prefix, so other tools' targets are not mistaken for real ones.
$foreignEnabled = @($allTargets | Where-Object { $_.enabled -and $_.label -notlike "TEST *" })
if ($foreignEnabled.Count -gt 0) {
    $list = ($foreignEnabled | ForEach-Object { "  - $($_.platform)  label=$($_.label)" }) -join "`n"
    if (-not $TakeOverRealTargets) {
        Die ("測試已中止：以下正式轉播目標仍為啟用狀態，測試流量會被推到真實平台。`n$list`n`n" +
            "請先停用它們，或加上 -TakeOverRealTargets 讓測試暫時停用並在結束後復原。")
    }
    Write-Host "==> 注意：以下正式轉播目標將被【暫時停用】，測試結束後自動復原：" -ForegroundColor Yellow
    Write-Host $list -ForegroundColor Yellow
    $answer = Read-Host "   確認暫時停用？(y/N)"
    if ($answer -notmatch '^(y|Y)') { Die "已取消。" }
    $script:RestoreTargets = @()
    foreach ($t in $foreignEnabled) {
        Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{ action = "toggle"; platform = $t.platform; enabled = $false } | Out-Null
        $script:RestoreTargets += $t.platform
        Write-Host "   已暫時停用 $($t.platform)" -ForegroundColor DarkYellow
    }
}

Write-Host "   endpoint=$Endpoint  rtmp=$Rtmp  source=$Source"
Write-Host "   pubSecret=($($script:Pub.Length) bytes)  apiSecret=($($script:Token.Length) bytes)"

try {
    Step "啟動本機來源 $Rtmp/live/$($script:SrcID)"
    Start-Source
    Start-Sleep -Seconds 3

    $fmode = if ($Slate) { "slate" } else { "hold" }
    $label = "TEST quick"
    Step "啟用轉推：本機目標 rtmp://localhost/live/$($script:TgtID)（$label）failover=$fmode"
    Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{
        action = "update"; platform = $script:Platform; stream = $script:SrcID; server = "rtmp://localhost/live/"; secret = $script:TgtID; enabled = $true; ended = $false; endedAt = 0; custom = $true; label = $label; normalize = [bool]$Normalize; failover = $(if ($Slate) { "slate" } else { "" })
    } | Out-Null

    if ($Compare) {
        # normalize is a GLOBAL program-layer option now (per-target normalize is ignored), so the
        # comparison must toggle it and record the same target twice, not create two targets.
        Step "對照模式：將依序錄製「全域 normalize 關閉」與「開啟」兩段相同目標輸出"
    }

    $playURL = "$Endpoint/live/$($script:TgtID).flv"
    if ($preview) {
        Step "先等目標有畫面，再開 ffplay 預覽：$playURL"
        if (-not (Wait-Target 30)) { Write-Host "   [警告] 目標 30 秒內未就緒，仍嘗試播放" -ForegroundColor DarkYellow }
        $script:PlayErr = Join-Path $env:TEMP "forward-quick-test-play.log"
        $script:PlayProc = Start-Process ffplay -ArgumentList @(
            "-loglevel", "warning", "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
            "-window_title", "forward-tgt-$($script:TgtID)", "-i", $playURL
        ) -PassThru -RedirectStandardError $script:PlayErr
    }

    if ($Compare) {
        Step "等待並觀察指標（約 12 秒）"
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

    if ($preview -and $script:PlayProc -and $script:PlayProc.HasExited) {
        Write-Host "   [注意] ffplay 已結束（可能沒抓到流），日誌：$($script:PlayErr)" -ForegroundColor DarkYellow
        if (Test-Path $script:PlayErr) { Get-Content $script:PlayErr -Tail 6 | ForEach-Object { Write-Host "     $_" -ForegroundColor DarkGray } }
    }

    Step "完成。轉推輸出（HTTP-FLV）：$playURL"
    if ($Compare) {
        $flvA = "$Endpoint/live/$($script:TgtID).flv"
        Write-Host ""
        Write-Host "   normalize 已移到全域設定，無法用兩個目標同時比較。" -ForegroundColor Yellow
        Write-Host "   請用同一個目標、切換全域開關各錄一次："; 
        Write-Host "     1. 設定 → 串流引擎 → 轉播備援：關閉『對共享節目源做補強轉碼』"
        Write-Host "     2. 錄一段：pwsh scripts/tools/forward-quality-compare.ps1 -A `"$flvA`" -B `"$flvA`" -Seconds 20"
        Write-Host "        （或直接把網址 $flvA 給播放器/錄製工具）"
        Write-Host "     3. 開啟該選項，再錄一次，然後比對兩段錄檔"
        Write-Host "   目標網址：$flvA" -ForegroundColor Green
    }
    if (-not $preview) {
        Write-Host "   看畫面：預設會自動開 ffplay 預覽；若沒開（找不到 ffplay），可把上面的 HTTP-FLV 網址貼到播放器。" -ForegroundColor Yellow
    }
    if ($Keep) {
        Write-Host "   已保留設定（-Keep）：UI 檢視請開 $Endpoint → 場景 → 轉推 → 展開「狀態」→ 該列『本機輸出進度』（含診斷時間線/近期日誌）。" -ForegroundColor Yellow
        if ($KeepMinutes -gt 0) {
            Write-Host "   約 $KeepMinutes 分鐘後自動停止來源與目標（-KeepMinutes 0 可取消限制）。" -ForegroundColor DarkGray
            $srcPid = if ($script:SourceProc) { $script:SourceProc.Id } else { 0 }
            Start-Job -ScriptBlock {
                param($secs, $srcPid, $endpoint, $token, $platform)
                Start-Sleep -Seconds $secs
                if ($srcPid -gt 0) { try { Stop-Process -Id $srcPid -Force -ErrorAction SilentlyContinue } catch { } }
                if ($platform) {
                    $h = @{ Authorization = "Bearer $token" }
                    $body = @{ action = 'delete'; platform = $platform } | ConvertTo-Json -Compress
                    try { Invoke-RestMethod -Method Post -Uri "$endpoint/terraform/v1/ffmpeg/forward/secret" -Headers $h -ContentType 'application/json' -Body $body | Out-Null } catch { }
                }
            } -ArgumentList ($KeepMinutes * 60), $srcPid, $Endpoint, $script:Token, $script:Platform | Out-Null
        }
    } else {
        Write-Host "   測試設定已清理（目標已刪除）；若想在 UI 看診斷，請加 -Keep 重跑一次。" -ForegroundColor DarkGray
    }
} finally {
    # Restore any real targets the test temporarily disabled.
if ($script:RestoreTargets -and $script:RestoreTargets.Count -gt 0) {
    foreach ($p in $script:RestoreTargets) {
        Api "POST" "/terraform/v1/ffmpeg/forward/secret" @{ action = "toggle"; platform = $p; enabled = $true } | Out-Null
        Write-Host "   已復原 $p" -ForegroundColor DarkYellow
    }
    $script:RestoreTargets = @()
}

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
