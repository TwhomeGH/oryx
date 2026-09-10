# Synthetic migration rehearsal only. Never mounts or connects to production data.
param(
    [string]$SourceImage = 'ghcr.io/twhomegh/oryx:latest',
    [string]$TargetImage = 'oryx-valkey:candidate',
    [string]$GoImage = 'golang:1.26',
    [switch]$SkipBareBinaryCheck = $true
)
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$runId = 'oryx-redis-rehearsal-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$volumes = @()
$containers = @()

function Invoke-Docker([string[]]$Arguments) {
    $result = & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "docker failed ($LASTEXITCODE): $($Arguments -join ' ')" }
    return $result
}
function New-TestVolume([string]$Suffix) {
    $name = "$runId-$Suffix"
    Invoke-Docker @('volume', 'create', '--label', "oryx.redis-rehearsal=$runId", $name) | Out-Null
    $script:volumes += $name
    return $name
}
function Start-Redis([string]$Image, [string]$Volume, [string]$Suffix) {
    $name = "$runId-$Suffix"
    $script:containers += $name
    Invoke-Docker @('run', '-d', '--name', $name, '--label', "oryx.redis-rehearsal=$runId",
        '--network', 'none', '--user', '0', '--entrypoint', 'redis-server',
        '--mount', "type=bind,source=$repo/platform/containers/conf/redis.conf,target=/test.conf,readonly",
        '--mount', "type=volume,source=$Volume,target=/data/redis", $Image,
        '/test.conf', '--daemonize', 'no', '--pidfile', '/tmp/redis.pid',
        '--requirepass', 'isolated-upgrade-test') | Out-Null
    for ($i = 0; $i -lt 30; $i++) {
        $reply = & docker exec -e REDISCLI_AUTH=isolated-upgrade-test $name redis-cli --raw PING 2>$null
        if ($LASTEXITCODE -eq 0 -and $reply -eq 'PONG') { return $name }
        if ((Invoke-Docker @('inspect', '--format', '{{.State.Running}}', $name)) -ne 'true') {
            Invoke-Docker @('logs', $name) | Out-Host
            throw 'Redis failed to start with repository configuration'
        }
        Start-Sleep -Milliseconds 500
    }
    throw 'Redis readiness timeout'
}
function Probe([string]$Container, [string]$Mode) {
    Invoke-Docker @('run', '--rm', '--network', "container:$Container",
        '--mount', "type=volume,source=$script:probeVolume,target=/probe,readonly",
        $script:goId, '/probe/probe', $Mode) | Out-Host
}
function Stop-Redis([string]$Container) {
    Invoke-Docker @('stop', '--time', '30', $Container) | Out-Null
}

try {
    # Resolve local tags once. The caller pulls missing images explicitly.
    $sourceId = Invoke-Docker @('image', 'inspect', '--format', '{{.Id}}', $SourceImage)
    $targetId = Invoke-Docker @('image', 'inspect', '--format', '{{.Id}}', $TargetImage)
    $script:goId = Invoke-Docker @('image', 'inspect', '--format', '{{.Id}}', $GoImage)
    Write-Host "Source: $SourceImage $sourceId"
    Write-Host "Target: $TargetImage $targetId"
    $script:probeVolume = New-TestVolume 'probe'
    $baseline = New-TestVolume 'baseline'
    $candidate = New-TestVolume 'candidate'
    Invoke-Docker @('run', '--rm', '--network', 'none',
        '--mount', "type=bind,source=$repo,target=/repo,readonly",
        '--mount', "type=volume,source=$script:probeVolume,target=/probe",
        '-w', '/repo/platform', $script:goId, 'go', 'build', '-mod=vendor',
        '-o', '/probe/probe', '/repo/scripts/tools/redis-upgrade-probe.go') | Out-Host

    $old = Start-Redis $sourceId $baseline 'old'
    Probe $old 'seed'
    Stop-Redis $old
    # Copy only the synthetic Redis 5 snapshot; baseline remains untouched by target Redis.
    Invoke-Docker @('run', '--rm', '--network', 'none',
        '--mount', "type=volume,source=$baseline,target=/baseline,readonly",
        '--mount', "type=volume,source=$candidate,target=/candidate",
        $script:goId, 'cp', '/baseline/dump.rdb', '/candidate/dump.rdb') | Out-Null

    $new = Start-Redis $targetId $candidate 'new'
    Probe $new 'check'
    Probe $new 'write'
    Stop-Redis $new
    $restart = Start-Redis $targetId $candidate 'restart'
    Probe $restart 'check-written'
    Stop-Redis $restart
    $rollback = Start-Redis $sourceId $baseline 'rollback'
    Probe $rollback 'check'
    Stop-Redis $rollback

    # This diagnostic applies to official binaries, not a packaged candidate wrapper.
    if (-not $SkipBareBinaryCheck) {
        Invoke-Docker @('run', '--rm', '--network', 'none', '--entrypoint', 'cp',
            '--mount', "type=volume,source=$script:probeVolume,target=/probe", $targetId,
            '/usr/local/bin/redis-server', '/probe/redis-server') | Out-Null
        & docker run --rm --network none --entrypoint /probe/redis-server `
            --mount "type=volume,source=$script:probeVolume,target=/probe,readonly" $sourceId --version
        if ($LASTEXITCODE -ne 0) {
            Write-Host 'PACKAGING BLOCKED: official Redis binary cannot run in the current Oryx base.'
        } else {
            Write-Host 'Official Redis binary version check passed in current Oryx base; full runtime tests still required.'
        }
    }
    Write-Host 'PASS: synthetic RDB migration, target Redis restart, and rollback using untouched Redis 5 snapshot.'
} finally {
    # Remove only resources created by this invocation, after checking ownership labels.
    foreach ($name in $containers) {
        $owner = & docker inspect --format '{{index .Config.Labels "oryx.redis-rehearsal"}}' $name 2>$null
        if ($LASTEXITCODE -eq 0 -and $owner -eq $runId) { & docker rm -fv $name | Out-Null }
    }
    foreach ($name in $volumes) {
        $owner = & docker volume inspect --format '{{index .Labels "oryx.redis-rehearsal"}}' $name 2>$null
        if ($LASTEXITCODE -eq 0 -and $owner -eq $runId) { & docker volume rm $name | Out-Null }
    }
}
