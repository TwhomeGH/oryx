<#
.SYNOPSIS
    一鍵更新 Oryx 產品版本號，可選同時升級 SRS 核心映像標籤；可一鍵完成一次發佈。
.DESCRIPTION
    產品版本號位於 platform/version.go；SRS 核心由根目錄 Dockerfile 的
    ossrs/srs:<tag> 基底映像決定。本工具可單獨或同時更新兩者。

    發佈模型：
      * push main        -> 只出開發映像（:sha-<commit>、:edge），不出 :latest / 版號。
      * 打 vX.Y.Z tag    -> 出正式映像（:vX.Y.Z、:latest）。
      * 因此「發佈」= bump 版號 + 打 tag；`-Release` 幫你一次做完（bump + commit + tag + push）。

    用法：
      powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -Minor -DryRun   # 預覽
      powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -Minor           # 只改檔，之後自行 commit/tag
      powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -Release         # 一鍵發佈：patch+1 -> commit -> tag -> push
      powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -Release -Minor  # 一鍵發佈（minor）
      powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -New v6.0.0 -SrsCore 7 -Release
      powershell -ExecutionPolicy Bypass -File scripts\bump-version.ps1 -SrsCore 7       # 只升 SRS 核心
#>
param(
    [string]$New,
    [switch]$Major,
    [switch]$Minor,
    [string]$SrsCore,
    [string]$Notes,
    [switch]$Release,
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
# -Release 預設 patch +1；否則只有明確要求 -New/-Major/-Minor 才動版號。
$bumpRequested = ($New -ne '') -or $Major -or $Minor -or $Release
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

# ---------- -Release 前置檢查（在動任何檔案之前） ----------
if ($Release) {
    if (-not $newVersion) { Write-Host '[失敗] -Release 需要版本變更（用 -Major/-Minor/-New）' -ForegroundColor Red; exit 1 }
    Write-Host "發佈版本: $newVersion" -ForegroundColor Cyan

    if ($DryRun) {
        Write-Host "[DryRun] 將執行：改版號 -> git commit -m `"Release $newVersion`" -> git tag $newVersion -> git push origin main $newVersion" -ForegroundColor Cyan
        exit 0
    }

    $branch = (git -C $repoRoot rev-parse --abbrev-ref HEAD).Trim()
    if ($branch -ne 'main') { Write-Host "[失敗] 目前分支是 '$branch'，請在 main 上發佈" -ForegroundColor Red; exit 1 }

    $dirty = (git -C $repoRoot status --porcelain)
    if ($dirty) { Write-Host '[失敗] 工作區不乾淨，請先 commit 或 stash 後再發佈' -ForegroundColor Red; git -C $repoRoot status --short; exit 1 }

    if ((git -C $repoRoot tag -l $newVersion)) { Write-Host "[失敗] tag $newVersion 已存在" -ForegroundColor Red; exit 1 }
}

# ---------- 執行檔案更新 ----------
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

# ---------- 一鍵發佈：commit + tag + push ----------
if ($Release) {
    $relFiles = @($changes | ForEach-Object { $_.File })

    # Add a changelog entry: the title plus the commits since the previous release, so the entry
    # is never just a bare title. Newest entry goes right after the header.
    $changelogFile = Join-Path $repoRoot 'docs\change-history.md'
    if ($Notes) {
        $prevTag = (git -C $repoRoot describe --tags --abbrev=0 2>$null)
        $range = if ($prevTag) { "$prevTag..HEAD" } else { 'HEAD' }
        $subjects = @(git -C $repoRoot log $range --no-merges --format='- %s' 2>$null)

        $entry = @("## $(Get-Date -Format 'yyyy.MM.dd HH:mm') $newVersion $Notes", '', '**類型**: 發佈', '')
        if ($subjects.Count -gt 0) { $entry += '### 修改'; $entry += $subjects; $entry += '' }
        $entry += '---'

        if (Test-Path $changelogFile) {
            $lines = Get-Content $changelogFile
            $first = ($lines | Select-String -Pattern '^## ' | Select-Object -First 1)
            if ($first) {
                $head = $lines[0..($first.LineNumber - 2)]
                $body = $lines[($first.LineNumber - 1)..($lines.Count - 1)]
            } else { $head = $lines; $body = @() }
        } else { $head = @('# Oryx 變更歷史', ''); $body = @() }

        $out = @($head) + @($entry) + @('') + @($body)
        [IO.File]::WriteAllText($changelogFile, (($out -join "`n") + "`n"), (New-Object System.Text.UTF8Encoding $false))
        Write-Host "已新增變更歷史：$Notes（含 $($subjects.Count) 筆提交）" -ForegroundColor Cyan
    }

    $addFiles = @($relFiles)
    if (Test-Path $changelogFile) { $addFiles += $changelogFile }
    git -C $repoRoot add -- $addFiles
    git -C $repoRoot commit -m "Release $newVersion"
    git -C $repoRoot tag $newVersion
    Write-Host '推送 main 與 tag（tag 會觸發 CI 發佈 :vX.Y.Z 與 :latest）...' -ForegroundColor Yellow
    git -C $repoRoot push origin main
    git -C $repoRoot push origin $newVersion
    Write-Host "完成：已發佈 $newVersion" -ForegroundColor Green
    exit 0
}

# ---------- 非 -Release：只改檔並給後續指引 ----------
Write-Host ''
Write-Host '後續操作建議：' -ForegroundColor Yellow
$files = ($changes | ForEach-Object { $_.File }) -join ' '
if ($newVersion) {
    Write-Host "  git add $files"
    Write-Host "  git commit -m `"Release $newVersion`""
    Write-Host '  git push origin main                # 出開發映像（:sha-*、:edge）'
    Write-Host "  git tag $newVersion && git push origin $newVersion   # 出正式映像（:$newVersion、:latest）"
    Write-Host '  或直接下次用: scripts\bump-version.ps1 -Release 一鍵完成'
} else {
    Write-Host "  git add $files"
    Write-Host '  git commit -m "chore: switch srs core"'
    Write-Host '  git push origin main'
}
Write-Host '  部署機: docker compose pull && docker compose up -d'
