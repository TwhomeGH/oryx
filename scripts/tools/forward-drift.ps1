<#
.SYNOPSIS
  觀察轉推目標的媒體時鐘漂移，判斷是固定延遲還是持續失步。

.DESCRIPTION
  定期讀取 SRS_FORWARD_METRICS 歷史，印出目前的 drift、歷史最小/最大值、fps 與碼率。
  重點是「最小值」是否隨時間上升：固定延遲會維持同一個區間，真正的 A/V 失步會讓
  最小值單調變大。

.PARAMETER Platform
  目標平台名稱（例如 bilibili、forwarding-obs-test）。省略時取第一個 running 目標。

.PARAMETER Container
  Docker 容器名稱，預設 oryx。

.PARAMETER Endpoint
  Oryx 平台網址，預設自動偵測容器對外埠。

.PARAMETER IntervalSec
  取樣間隔秒數，預設 30。

.PARAMETER Count
  取樣次數，預設 10（30s x 10 = 5 分鐘）。

.EXAMPLE
  pwsh scripts/tools/forward-drift.ps1

.EXAMPLE
  pwsh scripts/tools/forward-drift.ps1 -Platform bilibili -Count 20 -IntervalSec 60
#>
param(
    [string]$Platform = '',
    [string]$Container = 'oryx',
    [string]$Endpoint = '',
    [int]$IntervalSec = 30,
    [int]$Count = 10
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

if (-not $Endpoint) {
    $port = (docker port $Container 2022 2>$null | Select-Object -First 1) -replace '.*:', ''
    if (-not $port) { $port = '2022' }
    $Endpoint = "http://localhost:$port"
}

function Get-ApiHeaders {
    $secret = docker exec $Container redis-cli hget SRS_PLATFORM_SECRET token
    return @{Authorization = "Bearer $secret"}
}

$headers = Get-ApiHeaders
if (-not $Platform) {
    $all = (Invoke-RestMethod -Method Post -Uri "$Endpoint/terraform/v1/ffmpeg/forward/streams" -Headers $headers -ContentType 'application/json' -Body '{"action":"query"}').data
    $running = $all | Where-Object { $_.status -eq 'running' } | Select-Object -First 1
    if (-not $running) { throw '沒有 running 的轉推目標；請先啟動來源與目標。' }
    $Platform = $running.platform
}

Write-Host "觀察目標: $Platform  ($Endpoint)  每 ${IntervalSec}s x $Count" -ForegroundColor Cyan
$previousMin = $null
for ($i = 1; $i -le $Count; $i++) {
    $res = Invoke-RestMethod -Method Post -Uri "$Endpoint/terraform/v1/ffmpeg/forward/secret" `
        -Headers $headers -ContentType 'application/json' `
        -Body (@{action = 'metrics'; platform = $Platform} | ConvertTo-Json -Compress)
    $samples = @($res.data)
    if ($samples.Count -eq 0) {
        Write-Host ("{0}) 尚無樣本" -f $i) -ForegroundColor Yellow
        Start-Sleep -Seconds $IntervalSec
        continue
    }

    $drifts = $samples | ForEach-Object { [double]$_.drift }
    $min = ($drifts | Measure-Object -Minimum).Minimum
    $max = ($drifts | Measure-Object -Maximum).Maximum
    $last = $samples[$samples.Count - 1]

    $trend = ''
    if ($null -ne $previousMin) {
        if ($min -gt $previousMin + 0.5) { $trend = '  <-- 最小值上升，疑似真漂移' }
        elseif ($min -lt $previousMin - 0.5) { $trend = '  <-- 最小值下降' }
    }
    $previousMin = $min

    Write-Host ("{0,2}) drift_now={1,6:N2}s  range={2,6:N2}..{3,6:N2}  fps={4,5:N1}  kbps={5,6:N0}  samples={6}{7}" -f `
        $i, [double]$last.drift, $min, $max, [double]$last.fps, [double]$last.kbps, $samples.Count, $trend)

    if ($i -lt $Count) { Start-Sleep -Seconds $IntervalSec }
}

Write-Host ''
Write-Host '判讀：range 的最小值若維持不變 => 固定管線延遲（正常）；' -ForegroundColor Gray
Write-Host '      最小值持續上升 => 音畫失步，需要檢查時間戳處理。' -ForegroundColor Gray