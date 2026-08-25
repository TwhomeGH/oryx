<#
.SYNOPSIS
   從上游 ossrs/oryx 撿取（cherry-pick）指定 PR 的所有 commit 到本 fork。
.DESCRIPTION
   自動向 GitHub API 查詢 PR 的精確 commit 清單，逐一預覽、確認後依序 cherry-pick，
   最後可選擇推送。衝突時提供中文處理指引。

   用法：
     powershell -ExecutionPolicy Bypass -File scripts\pick-pr.ps1 247
     或在已允許腳本的環境： .\scripts\pick-pr.ps1 247

   選用參數：
     -UpstreamRepo owner/repo   上游倉庫（預設 ossrs/oryx）
     -Push                      完成後直接推送，不詢問
     -DryRun                    只顯示 PR 內容與 commit 清單，不實際套用
.EXAMPLE
   .\scripts\pick-pr.ps1 248 -DryRun     # 先看看 #248 改什麼
   .\scripts\pick-pr.ps1 248             # 確認後套用
#>
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [int]$Number,
    [string]$UpstreamRepo = 'ossrs/oryx',
    [switch]$Push,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Write-Step([string]$m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Write-Ok([string]$m)   { Write-Host "    [OK] $m" -ForegroundColor Green }
function Write-Bad([string]$m)  { Write-Host "    [失敗] $m" -ForegroundColor Red }

# ---------- 0. 環境檢查 ----------
git rev-parse --is-inside-work-tree *> $null
if ($LASTEXITCODE -ne 0) { Write-Bad '必須在 git 倉庫內執行'; exit 1 }

$remotes = @(git remote)
if ($remotes -notcontains 'upstream') {
    Write-Step "找不到 upstream remote，自動新增 https://github.com/$UpstreamRepo.git"
    git remote add upstream "https://github.com/$UpstreamRepo.git"
}

# ---------- 1. 向 GitHub API 查詢 PR 資訊 ----------
$headers = @{ 'User-Agent' = 'oryx-pick-pr'; 'Accept' = 'application/vnd.github+json' }
$apiBase = "https://api.github.com/repos/$UpstreamRepo/pulls/$Number"

Write-Step "查詢 PR #$Number 資訊..."
try {
    $pr = Invoke-RestMethod -Headers $headers -Uri $apiBase
    $commits = @(Invoke-RestMethod -Headers $headers -Uri "$apiBase/commits")
} catch {
    if ($_.Exception.Response -and $_.Exception.Response.StatusCode -eq 404) {
        Write-Bad "PR #$Number 在 $UpstreamRepo 不存在"
    } else {
        Write-Bad "GitHub API 查詢失敗：$($_.Exception.Message)"
        Write-Host '    （可能是網路問題或 API 速率限制，稍後再試）'
    }
    exit 1
}

$title  = $pr.title
$state  = $pr.state
$merged = $pr.merged
$url    = $pr.html_url

Write-Host ''
Write-Host ("PR #{0}: {1}" -f $Number, $title) -ForegroundColor Yellow
Write-Host ("狀態: {0}{1}   commits: {2}" -f $state, $(if ($merged) { '(已被上游合併!)' }), $commits.Count)
Write-Host ("網址: {0}" -f $url)

if ($merged) {
    Write-Host ''
    Write-Bad '這個 PR 已經被上游合併了，通常不需要再撿。仍要繼續請自行手動操作。'
    exit 1
}

# ---------- 2. 抓取 PR ref ----------
Write-Step "git fetch upstream pull/$Number/head"
git fetch upstream "pull/$Number/head"
if ($LASTEXITCODE -ne 0) { Write-Bad 'fetch 失敗'; exit 1 }

$fetchedTip = (git rev-parse FETCH_HEAD).Trim()
if ($fetchedTip -ne $commits[-1].sha) {
    Write-Host '    （提示：fetch 尖端與 API 清單最新 commit 不一致，可能清單有快取延遲，以實際內容為準）' -ForegroundColor DarkYellow
}

# ---------- 3. 預覽每個 commit ----------
Write-Host ''
Write-Host ('此 PR 共 {0} 個 commit（由舊到新）：' -f $commits.Count)
foreach ($c in $commits) {
    $shortMsg = ($c.commit.message -split "`r?`n")[0]
    Write-Host ''
    Write-Host ('  [' + $c.sha.Substring(0, 8) + '] ' + $shortMsg) -ForegroundColor White
    git show --stat --format='   作者: %an   日期: %ad' $c.sha | Out-Host
}

if ($DryRun) {
    Write-Host ''
    Write-Ok 'DryRun 模式結束，未做任何變更。去掉 -DryRun 即可實際套用。'
    exit 0
}

# ---------- 4. 確認 ----------
Write-Host ''
$branch = git branch --show-current
if ($branch -ne 'main') {
    $goOn = Read-Host "目前不在 main（在 $branch），確定要套用到 $branch 嗎？[y/N]"
    if ($goOn -notmatch '^(y|yes)$') { Write-Host '已取消。'; exit 0 }
}

$dirty = git status --porcelain
if ($dirty) {
    Write-Bad '工作區有未提交的變更，cherry-pick 可能失敗。'
    Write-Host '    請先 commit 或 stash 之後再執行本腳本。'
    git status --short | Out-Host
    exit 1
}

$ans = Read-Host ('要套用這 {0} 個 commit 嗎？[Y/n]' -f $commits.Count)
if ($ans -match '^(n|no)$') { Write-Host '已取消。'; exit 0 }

# ---------- 5. 依序 cherry-pick ----------
foreach ($c in $commits) {
    Write-Step ("git cherry-pick {0}  {1}" -f $c.sha.Substring(0, 8), ($c.commit.message -split "`r?`n")[0])
    git cherry-pick $c.sha
    if ($LASTEXITCODE -ne 0) {
        Write-Host ''
        Write-Bad '發生衝突！請依照以下步驟處理：'
        Write-Host @'
    1. 執行  git status  查看哪些檔案衝突
    2. 打開檔案解決 <<<<<<< ======= >>>>>>> 標記
    3. 解決後執行  git add <檔案>  再  git cherry-pick --continue
       （如果這個 PR 有多個 commit，--continue 後要對剩下的 commit 重複確認）
    4. 想全部放棄：git cherry-pick --abort 之後重新評估
'@
        exit 1
    }
    Write-Ok '完成'
}

# ---------- 6. 結果與推送 ----------
Write-Host ''
Write-Step ('全部套用完成，最新的 {0} 個 commit：' -f $commits.Count)
git log --oneline (- $commits.Count) | Out-Host

if (-not $Push) {
    $doPush = Read-Host '要推送到 origin 嗎？[y/N]'
    $Push = ($doPush -match '^(y|yes)$')
}
if ($Push) {
    Write-Step "git push origin $branch"
    git push origin $branch
    if ($LASTEXITCODE -eq 0) { Write-Ok '已推送' } else { Write-Bad '推送失敗，請檢查後手動 git push' }
} else {
    Write-Host '（未推送，需要時自己執行 git push origin ' -NoNewline
    Write-Host $branch -NoNewline
    Write-Host ' ）'
}
