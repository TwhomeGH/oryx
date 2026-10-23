# Measure the latency the Oryx forward itself adds: the video PTS lag of the forwarded target
# behind the program stream (the source as seen by the fanout). This excludes the platform's own
# latency, so you can tell how much of the end-to-end delay is Oryx's.
#
# Usage:
#   pwsh scripts/tools/forward-latency.ps1 -TestSource                   # start a local test source
#   pwsh scripts/tools/forward-latency.ps1 -Source live/livestream       # use an existing source
#   pwsh scripts/tools/forward-latency.ps1 -Source live/livestream -Samples 10 -IntervalSec 2
#
# Requires a running Oryx container (default: oryx). The script creates a temporary local forward
# target, samples the PTS of the program and the target, then removes it.

param(
    [string]$Container = "oryx",
    [string]$Endpoint = "",
    [string]$Source = "",
    [switch]$TestSource,
    [int]$Samples = 5,
    [int]$IntervalSec = 3,
    # Opt-in: temporarily disable real targets for the test and restore them afterwards.
    [switch]$TakeOverRealTargets
)

$script:RestoreTargets = @()

$ErrorActionPreference = "Stop"

function Get-DockerPort([string]$name, [int]$containerPort) {
    $line = (docker port $name $containerPort 2>$null | Select-Object -First 1)
    if (-not $line) { return "" }
    $parts = $line.Split(":")
    return $parts[$parts.Length - 1]
}
function Get-RedisField([string]$hash, [string]$field) {
    return (docker exec $Container redis-cli hget $hash $field 2>$null).Trim()
}

if (-not $Endpoint) {
    $api = Get-DockerPort $Container 2022
    if (-not $api) { $api = 2022 }
    $Endpoint = "http://localhost:$api"
}
$secret = Get-RedisField "SRS_PLATFORM_SECRET" "token"
if (-not $secret) { throw "cannot read SRS_PLATFORM_SECRET from container $Container" }
$pub = Get-RedisField "SRS_AUTH_SECRET" "pubSecret"
$headers = @{ Authorization = "Bearer $secret" }

$pidStr = "$PID"
$srcStream = "latency-src-$pidStr"
$tgtStream = "latency-tgt-$pidStr"
$tgtPlatform = "forwarding-latency-$pidStr"
$secretValue = $tgtStream
$target = @{
    action = "update"; platform = $tgtPlatform; server = "rtmp://localhost/live/"; secret = $secretValue
    stream = $srcStream
    enabled = $true; custom = $true; label = "TEST latency-$pidStr"; failover = ""; normalize = $false
}
function Api([string]$path, $body) {
    return Invoke-RestMethod -Method Post -Uri "$Endpoint$path" -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json -Compress)
}

"endpoint=$Endpoint  pubSecret=($($pub.Length) bytes)"
try {
    $m = (Api "/terraform/v1/ffmpeg/forward/enabled" @{ action = "query" }).data
    if ($m -and $m.enabled -eq $false) {
        Write-Warning "global forward switch is OFF; enable it in the UI (or the API) or the forward will not run."
    }
} catch { }

    # Safety guard: never forward the test source to a real platform. Any enabled non-TEST target
    # would receive it, so abort unless the operator explicitly opts in.
    $allTargets = (Api "/terraform/v1/ffmpeg/forward/streams" @{ action = "query" }).data
    $foreignEnabled = @($allTargets | Where-Object { $_.enabled -and $_.label -notlike "TEST *" })
    if ($foreignEnabled.Count -gt 0) {
        $list = ($foreignEnabled | ForEach-Object { "  - $($_.platform)  label=$($_.label)" }) -join "`n"
        if (-not $TakeOverRealTargets) {
            throw ("測試已中止：以下正式轉播目標仍為啟用狀態，測試流量會被推到真實平台。`n$list`n`n" +
                "請先停用它們，或加上 -TakeOverRealTargets 讓測試暫時停用並在結束後復原。")
        }
        Write-Host "==> 注意：以下正式轉播目標將被【暫時停用】，測試結束後自動復原：" -ForegroundColor Yellow
        Write-Host $list -ForegroundColor Yellow
        $answer = Read-Host "   確認暫時停用？(y/N)"
        if ($answer -notmatch '^(y|Y)') { throw "已取消。" }
        foreach ($t in $foreignEnabled) {
            Api "/terraform/v1/ffmpeg/forward/secret" @{ action = "toggle"; platform = $t.platform; enabled = $false } | Out-Null
            $script:RestoreTargets += $t.platform
            Write-Host "   已暫時停用 $($t.platform)" -ForegroundColor DarkYellow
        }
    }
try {
    if ($TestSource) {
        $Source = "live/$srcStream"
        $rtmpPort = Get-DockerPort $Container 1935
        if (-not $rtmpPort) { $rtmpPort = 1935 }
        $srcURL = if ($pub) { "rtmp://localhost:$rtmpPort/$Source`?secret=$pub" } else { "rtmp://localhost:$rtmpPort/$Source" }
        Write-Host "starting test source $Source (host ffmpeg -> port $rtmpPort)"
        $srcLog = Join-Path $env:TEMP "forward-latency-src.log"
        $script:srcProc = Start-Process ffmpeg -ArgumentList @(
            "-re", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25",
            "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
            "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-g", "50", "-pix_fmt", "yuv420p",
            "-c:a", "aac", "-f", "flv", $srcURL
        ) -PassThru -WindowStyle Hidden -RedirectStandardError $srcLog
        Start-Sleep -Seconds 3
    }

    if (-not $Source) { throw "provide -Source app/stream or -TestSource" }
    $parts = $Source.Split("/")
    if ($parts.Length -ne 2) { throw "source must be app/stream, e.g. live/livestream" }
    $app = $parts[0]; $stream = $parts[1]
    $progStream = "__forward_${app}_${stream}"


    Write-Host "creating local forward target '$tgtStream'"
    Api "/terraform/v1/ffmpeg/forward/secret" $target | Out-Null

    Write-Host "waiting for the forward to run..."
    $ready = $false
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Seconds 2
        $st = (Api "/terraform/v1/ffmpeg/forward/streams" @{ action = "query" }).data
        $row = $st | Where-Object { $_.platform -eq $tgtPlatform }
        if ($row -and $row.status -eq "running" -and $row.fallback -ne $true) { $ready = $true; break }
    }
    if (-not $ready) { Write-Warning "forward not ready after 40s; sampling anyway" }
    Start-Sleep -Seconds 5

    $progUrl = "http://localhost:8080/$app/$progStream.flv"
    $tgtUrl = "http://localhost:8080/$app/$tgtStream.flv"
    $latencies = @()
    for ($i = 1; $i -le $Samples; $i++) {
        $sh = @"
rm -f /tmp/_lp /tmp/_lt
stdbuf -oL timeout 5 ffprobe -v error -select_streams v:0 -show_entries packet=pts_time -of csv=p=0 '$progUrl' 2>/dev/null | tail -1 > /tmp/_lp &
stdbuf -oL timeout 5 ffprobe -v error -select_streams v:0 -show_entries packet=pts_time -of csv=p=0 '$tgtUrl' 2>/dev/null | tail -1 > /tmp/_lt &
wait
cat /tmp/_lp; cat /tmp/_lt
"@
        # PowerShell here-strings use CRLF; sh -c would treat the trailing \r as part of the
        # command (e.g. "cat /tmp/_lp\r"), so normalize to LF before running.
        $sh = $sh.Replace("`r`n", "`n")
        if ($env:LATENCY_DEBUG) { Write-Host "SH<<$sh>>" }
        $raw = (docker exec $Container sh -c $sh) -split "`n" | Where-Object { $_.Trim() -ne "" }
        if ($env:LATENCY_DEBUG) { Write-Host "RAW<<$($raw -join ' | ')>>" }
        if ($raw.Count -lt 2) { Write-Warning "sample $i failed"; continue }
        $p = [double]($raw[0].Trim()); $t = [double]($raw[1].Trim())
        $d = $p - $t
        $latencies += $d
        "{0,2}. program={1,10:N3}s  target={2,10:N3}s  forward latency={3,6:N2}s" -f $i, $p, $t, $d
        if ($i -lt $Samples) { Start-Sleep -Seconds $IntervalSec }
    }

    if ($latencies.Count -gt 0) {
        $avg = ($latencies | Measure-Object -Average).Average
        $min = ($latencies | Measure-Object -Minimum).Minimum
        $max = ($latencies | Measure-Object -Maximum).Maximum
        $sorted = @($latencies | Sort-Object)
        $mid = [int][math]::Floor($sorted.Count / 2)
        $median = if ($sorted.Count % 2 -eq 1) { $sorted[$mid] } else { ($sorted[$mid - 1] + $sorted[$mid]) / 2 }
        ""
        "forward latency: median={0:N2}s  avg={1:N2}s  min={2:N2}s  max={3:N2}s  (samples={4})" -f $median, $avg, $min, $max, $latencies.Count
        "note: this excludes the platform's own latency; add that for the viewer-side delay."
    }
} finally {
    Write-Host "cleaning up..."
    try { Api "/terraform/v1/ffmpeg/forward/secret" @{ action = "delete"; platform = $tgtPlatform } | Out-Null } catch { }
    if ($script:RestoreTargets -and $script:RestoreTargets.Count -gt 0) {
        foreach ($p in $script:RestoreTargets) {
            try { Api "/terraform/v1/ffmpeg/forward/secret" @{ action = "toggle"; platform = $p; enabled = $true } | Out-Null } catch { }
            Write-Host "   已復原 $p" -ForegroundColor DarkYellow
        }
        $script:RestoreTargets = @()
    }
    if ($script:srcProc) { try { Stop-Process -Id $script:srcProc.Id -Force -ErrorAction SilentlyContinue } catch { } }
}
