<#
.SYNOPSIS
   一鍵更新 Oryx 產品版本號，可選同時升級 SRS 核心映像標籤。
.DESCRIPTION
   產品版本號位於 platform/version.go；SRS 核心由根目錄 Dockerfile 的
   ossrs/srs:<tag> 基底映像決定。本工具可單獨或同時更新兩者。

   用法：
     powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1              # patch 自動 +1
     powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -Minor       # minor +1, patch 歸零
     powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -Major       # major +1, 其餘歸零
     powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -New v6.0.0  # 直接指定
     powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -SrsCore 7   # 只升 SRS 核心
     powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -New v5.16.0 -SrsCore v7.0.157 -DryRun
#>
param(
    [string]$New,
    [switch]$Major,
    [switch]$Minor,
    [string]$SrsCore,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$repoRoot    = Split-Path -Parent $PSScriptRoot
$versionFile = Join-Path $repoRoot 'platform\version.go'
$dockerfile  = Join-Path $repoRoot 'Dockerfile'

if (-not (Test-Path $versionFile)) { Write-Host "[失敗] 找不到 $versionFile" -ForegroundColor Red; exit 1 }

# ---------- 讀取目前產品版本 ----------
$content = Get-Content $versionFile -Raw
if ($content -notmatch 'const version = "(v\d+)\.(\d+)\.(\d+)"') {
    Write-Host '[失敗] version.go 中找不到 const version = "vX.Y.Z"' -ForegroundColor Red
    exit 1
}
$maj, $min, $pat = $Matches[1], [int]$Matches[2], [int]$Matches[3]
$majNum = [int]($maj -replace '^v', '')
Write-Host "目前產品版本: v$majNum.$min.$pat"

# ---------- 計算新版本 ----------
# Only bump product version when explicitly requested (-New/-Major/-Minor).
$bumpRequested = ($New -ne '') -or $Major -or $Minor
$newVersion = $null
if ($bumpRequested) {
if ($New) {
    if ($New -notmatch '^v?\d+\.\d+\.\d+$') {
        Write-Host "[失敗] 版本格式應為 vX.Y.Z，收到: $New" -ForegroundColor Red; exit 1
    }
    $newVersion = "v$($New -replace '^v', '')"
} else {
    $newMaj, $newMin, $newPat = $majNum, $min, $pat
    if     ($Major) { $newMaj++; $newMin = 0; $newPat = 0 }
    elseif ($Minor) { $newMin++; $newPat = 0 }
    else            { $newPat++ }
    $newVersion = "v$newMaj.$newMin.$newPat"
}
}

# ---------- 執行更新 ----------
$changes = @()

if ($newVersion) {
$newContent = $content -replace 'const version = "[^"]+"', "const version = `"$newVersion`""
if ($newContent -ne $content) {
    $changes += @{ File = $versionFile; Desc = "產品版本 -> $newVersion"; Content = $newContent }
}
}

if ($SrsCore) {
    $df = Get-Content $dockerfile -Raw
    if ($df -notmatch "FROM \$\{ARCH\}ossrs/srs:[^ \r\n]+ AS srs") {
        Write-Host '[失敗] Dockerfile 中找不到 ossrs/srs:<tag> AS srs 行' -ForegroundColor Red
        exit 1
    }
    $newDf = $df -replace '(FROM \$\{ARCH\}ossrs/srs):[^ \r\n]+( AS srs)', "`$1:$SrsCore`$2"
    if ($newDf -ne $df) {
        $changes += @{ File = $dockerfile; Desc = "SRS 核心 -> ossrs/srs:$SrsCore"; Content = $newDf }
    } else {
        Write-Host "[略過] SRS 核心已是 ossrs/srs:$SrsCore，無需變更" -ForegroundColor DarkYellow
    }
}

if ($changes.Count -eq 0) {
    Write-Host '沒有任何變更。'; exit 0
}

foreach ($c in $changes) {
    if ($DryRun) {
        Write-Host "[DryRun] $($c.Desc)" -ForegroundColor Cyan
    } else {
        [IO.File]::WriteAllText($c.File, $c.Content, (New-Object System.Text.UTF8Encoding $false))
        Write-Host "[OK] $($c.File) — $($c.Desc)" -ForegroundColor Green
    }
}

# ---------- 後續指引 ----------
Write-Host ''
if ($changes.Count -eq 0) { Write-Host '沒有任何變更需要提交。'; exit 0 }
Write-Host '後續操作建議：' -ForegroundColor Yellow
$files = ($changes | ForEach-Object { $_.File } ) -join ' '
if ($DryRun) { $files = '<相關檔案>' }
Write-Host "  git add $files"
if ($newVersion) {
    Write-Host "  git commit -m `"chore: release $newVersion`""
    Write-Host '  git push origin main                # 觸發 CI 建置 :latest 與版號映像'
    Write-Host "  git tag $newVersion && git push origin $newVersion   # (可選) 另出 tag 映像"
} else {
    Write-Host '  git commit -m "chore: switch srs core"'
    Write-Host '  git push origin main                # 觸發 CI 建置映像'
}
Write-Host '  部署機: docker compose pull && docker compose up -d'
