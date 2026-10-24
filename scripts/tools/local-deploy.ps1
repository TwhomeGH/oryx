#!/usr/bin/env pwsh
<#
.SYNOPSIS
    本地快速部署：建置 oryx:local、回報「目前容器實際用哪個 build」，並可一鍵重建。
.DESCRIPTION
    解決「改了碼、build 了，但容器還是跑舊 image」的困擾：
      * 建置時把 git 修訂與版本寫進映像標籤（oryx.revision/oryx.version）。
      * 回報正在執行的容器用的 image ID / revision / version，並和最新的 oryx:local 比對是否落後。
      * -Deploy 直接 docker compose up -d --force-recreate 以新映像重建（資料在 /data volume，不動）。

    設定（首次指定即記住）：帶 -ComposeDir 會把 compose 目錄（與 container/image）寫入
    scripts/tools/local-deploy.ini（已 gitignore），之後 -Deploy 不必再指定。

    用法：
      pwsh scripts/tools/local-deploy.ps1 -Deploy -ComposeDir "C:\path\to\compose"  # 首次：指定並記住
      pwsh scripts/tools/local-deploy.ps1 -Deploy                                   # 之後：直接用記住的目錄
      pwsh scripts/tools/local-deploy.ps1 -Check                                    # 只回報（不建置/不重建）
      pwsh scripts/tools/local-deploy.ps1 -ComposeDir "C:\other" -Save              # 只更新設定
      pwsh scripts/tools/local-deploy.ps1                                           # 建置 + 回報
.PARAMETER Image
    映像標籤，預設 oryx:local。
.PARAMETER Container
    容器名稱，預設 oryx。
.PARAMETER ComposeDir
    docker-compose 所在目錄；指定後會記住供下次使用。
.PARAMETER Check
    只檢查映像／容器／原始碼的版本落差，不建置也不部署。
.PARAMETER Deploy
    建置後以 --force-recreate 重建容器，確保套用新映像。
.PARAMETER Save
    儲存本次的 -ComposeDir 設定。
.PARAMETER DryRun
    只印出將執行的動作，不實際變更。
.PARAMETER Help
    顯示說明。
#>
param(
    [string]$Image,
    [string]$Container,
    [string]$ComposeDir,
    [switch]$Check,
    [switch]$Deploy,
    [switch]$Save,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$iniPath = Join-Path $PSScriptRoot 'local-deploy.ini'

function Step($m) { Write-Host "==> $m" -ForegroundColor Cyan }

# --- Load saved settings (gitignored ini) ---
$saved = @{}
if (Test-Path $iniPath) {
    foreach ($line in Get-Content $iniPath) {
        if ($line -match '^\s*([^#=]+?)\s*=\s*(.*?)\s*$') { $saved[$Matches[1]] = $Matches[2] }
    }
}
if (-not $Image) { $Image = if ($saved['image']) { $saved['image'] } else { 'oryx:local' } }
if (-not $Container) { $Container = if ($saved['container']) { $saved['container'] } else { 'oryx' } }
if (-not $ComposeDir -and $saved['composeDir']) { $ComposeDir = $saved['composeDir'] }

# Persist when ComposeDir is explicitly provided, or on -Save.
if ($Save -or $PSBoundParameters.ContainsKey('ComposeDir')) {
    $ini = "# local-deploy 設定（自動產生，已 gitignore）。`ncomposeDir=$ComposeDir`ncontainer=$Container`nimage=$Image`n"
    [IO.File]::WriteAllText($iniPath, $ini, (New-Object System.Text.UTF8Encoding $false))
    Write-Host "已儲存設定到 $iniPath（composeDir=$ComposeDir）" -ForegroundColor Green
    if ($Save) { exit 0 }
}

# --- Read the source version + revision so the image carries them as labels ---
$version = 'unknown'
if (Test-Path (Join-Path $repoRoot 'platform\version.go')) {
    $m = (Get-Content (Join-Path $repoRoot 'platform\version.go') -Raw) -match 'const version = "([^"]+)"'
    if ($m) { $version = $Matches[1] }
}
$revision = (git -C $repoRoot rev-parse HEAD 2>$null); if (-not $revision) { $revision = 'unknown' }
$revision = $revision.Trim()
Write-Host "source: version=$version revision=$($revision.Substring(0, [Math]::Min(12, $revision.Length)))"

# --- Build the local image with build labels ---
if (-not $Check) {
    Step "建置 $Image（含 revision/version 標籤）"
    if ($DryRun) {
        Write-Host "[DryRun] docker build -t $Image --label oryx.revision=$revision ... ."
    } else {
        docker build -t $Image `
            --label "oryx.revision=$revision" `
            --label "oryx.version=$version" `
            $repoRoot
        if ($LASTEXITCODE -ne 0) { Write-Host '[失敗] docker build 失敗' -ForegroundColor Red; exit 1 }
    }
}

# --- Describe an image: id + revision + version labels ---
function Describe-Image($ref) {
    $id = $ref
    if (-not $ref.StartsWith('sha256:')) {
        $id = (docker image inspect $ref --format '{{.Id}}' 2>$null)
    }
    if (-not $id) { return $null }
    $rev = (docker image inspect $id --format '{{index .Config.Labels "oryx.revision"}}' 2>$null)
    $ver = (docker image inspect $id --format '{{index .Config.Labels "oryx.version"}}' 2>$null)
    $created = (docker image inspect $id --format '{{.Created}}' 2>$null)
    return [pscustomobject]@{ Id = $id; Rev = $rev; Ver = $ver; Created = $created }
}

$local = Describe-Image $Image
$running = $null
if ((docker ps -a --filter "name=^/$Container$" --format '{{.Names}}') -eq $Container) {
    $runningId = (docker inspect $Container --format '{{.Image}}' 2>$null)
    $runningImage = (docker inspect $Container --format '{{.Config.Image}}' 2>$null)
    $running = Describe-Image "$runningId"
    if ($running) { $running | Add-Member -NotePropertyName Ref -NotePropertyValue $runningImage }
}

Step "目前狀態"
# Compare the build target against the CURRENT source revision: "一致" between image and
# container alone can be mistaken for "running the latest local code", so a source change that
# has not been rebuilt is reported explicitly.
$targetStale = $local -and $local.Rev -and ($local.Rev -ne $revision)
if ($local) {
    $tf = if ($targetStale) { '落後源碼 -> 需重建' } else { "= 源碼 $($revision.Substring(0, [Math]::Min(12, $revision.Length)))" }
    Write-Host ("  build target : {0}  ({1})  rev={2}  version={3}  -> {4}" -f $Image, $local.Id.Substring(7, 12), $(if ($local.Rev) { $local.Rev.Substring(0, [Math]::Min(12, $local.Rev.Length)) } else { 'n/a' }), $local.Ver, $tf) -ForegroundColor $(if ($targetStale) { 'Yellow' } else { 'Green' })
} else {
    Write-Host "  build target : （尚無 $Image，需先建置）" -ForegroundColor Yellow
}
if ($running) {
    $stale = $local -and ($running.Id -ne $local.Id)
    $flag = if ($stale) { '落後 build target（需 -Deploy 重建）' } elseif ($targetStale) { '= build target，但源碼已更新（需重建）' } else { '= 最新源碼' }
    Write-Host ("  容器 {0,-6}: image={1}  ({2})  rev={3}  version={4}  -> {5}" -f $Container, $running.Ref, $running.Id.Substring(7, 12), $(if ($running.Rev) { $running.Rev.Substring(0, [Math]::Min(12, $running.Rev.Length)) } else { 'n/a（舊 build 無標籤）' }), $running.Ver, $flag) -ForegroundColor $(if ($stale -or $targetStale) { 'Yellow' } else { 'Green' })
} else {
    Write-Host "  找不到執行中的容器 '$Container'。" -ForegroundColor DarkYellow
}

# --- Recreate via docker compose ---
if ($Deploy) {
    if (-not $ComposeDir) {
        Write-Host "[失敗] 未指定 compose 目錄（用 -ComposeDir 指定一次即會記住）" -ForegroundColor Red; exit 1
    }
    $composeFile = Join-Path $ComposeDir 'docker-compose.yml'
    if (-not (Test-Path $composeFile)) { Write-Host "[失敗] 找不到 $composeFile（用 -ComposeDir 指定）" -ForegroundColor Red; exit 1 }

    Step "docker compose up -d --force-recreate（重建容器，/data 不動）: $ComposeDir"
    if ($DryRun) {
        Write-Host "[DryRun] cd $ComposeDir && docker compose up -d --force-recreate"
    } else {
        Push-Location $ComposeDir
        try {
            docker compose up -d --force-recreate
            if ($LASTEXITCODE -ne 0) { Write-Host '[失敗] docker compose 失敗' -ForegroundColor Red; exit 1 }
        } finally { Pop-Location }
        $ok = Describe-Image (docker inspect $Container --format '{{.Image}}' 2>$null)
        if ($ok -and $local -and $ok.Id -eq $local.Id) {
            Write-Host "完成：$Container 現在跑的就是最新 build（rev=$($local.Rev.Substring(0, [Math]::Min(12, $local.Rev.Length))), version=$($local.Ver)）。" -ForegroundColor Green
        } else {
            Write-Host "完成重建（請再跑一次 -Check 確認）。" -ForegroundColor Yellow
        }
    }
} elseif ($targetStale) {
    Write-Host "提示：build target 落後源碼，跑一次本腳本（不加 -Check）即會重建最新映像。" -ForegroundColor Yellow
} elseif ($running -and $local -and $running.Id -ne $local.Id) {
    Write-Host "提示：容器落後，加 -Deploy 即可用最新 build 重建。" -ForegroundColor Yellow
}
