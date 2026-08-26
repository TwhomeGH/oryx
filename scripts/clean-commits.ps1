#
# clean-commits.ps1 - 通用 Git 提交整理交互工具（任何專案可用）
#
# 定位：把改到亂七八糟的分支整理成乾淨的 PR。
# 用法：powershell -ExecutionPolicy Bypass -File scripts/clean-commits.ps1
#

$ErrorActionPreference = 'Stop'

function Assert-InGitRepo {
    git rev-parse --is-inside-work-tree *> $null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[X] 目前目錄不是 git 倉庫" -ForegroundColor Red
        exit 1
    }
}

function Get-CurrentBranch {
    git rev-parse --abbrev-ref HEAD
}

function New-BackupBranch {
    $branch = Get-CurrentBranch
    $ts = Get-Date -Format 'yyyyMMdd-HHmmss'
    $name = ("backup-" + $branch + "-" + $ts) -replace '/', '-'
    git branch $name | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] 已建立備份分支 $name（出事可 git reset --hard $name 找回）" -ForegroundColor Green
    }
}

function Show-Overview {
    $branch = Get-CurrentBranch
    Write-Host ""
    Write-Host "=== 現況 ===" -ForegroundColor Cyan
    Write-Host "分支: $branch"
    Write-Host "--- 最近 10 筆提交 ---"
    git log --oneline -10
    Write-Host "--- 工作區狀態 ---"
    git status --short
    if (-not (git status --porcelain)) { Write-Host "(乾淨)" }
}

function Show-RebaseResult {
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] 完成。目前提交：" -ForegroundColor Green
        git log --oneline -8
    } else {
        Write-Host ""
        Write-Host "[X] 遇到衝突，照順序處理：" -ForegroundColor Red
        Write-Host "  1. git status                # 看哪些檔案衝突"
        Write-Host "  2. 編輯檔案解決衝突後 git add <檔案>"
        Write-Host "  3. git rebase --continue     # 繼續"
        Write-Host "  或反悔：git rebase --abort   # 完全還原"
    }
}

function Invoke-InteractiveRebase {
    $n = Read-Host "要重整最近幾筆提交？(數字)"
    if ($n -notmatch '^\d+$' -or [int]$n -lt 1) {
        Write-Host "[X] 請輸入正整數" -ForegroundColor Red
        return
    }
    New-BackupBranch
    Write-Host ""
    Write-Host "即將開啟編輯器互動重整。指令：pick=保留 reword=改訊息 squash=併入上筆(留訊息) fixup=併入上筆(丟訊息) drop=刪除" -ForegroundColor Yellow
    Write-Host "vim 的話：i 修改，Esc 後 :wq 離開"
    git rebase -i ("HEAD~" + $n)
    Show-RebaseResult
}

function Invoke-FixupIntoOlder {
    if (-not (git status --porcelain)) {
        Write-Host "[!] 工作區沒有未提交的改動" -ForegroundColor Yellow
        return
    }
    Write-Host ""
    Write-Host "最近 15 筆提交：" -ForegroundColor Cyan
    git log --oneline -15
    $idx = Read-Host "把目前改動併入第幾筆？(行號 1-15)"
    if ($idx -notmatch '^\d+$' -or [int]$idx -lt 1 -or [int]$idx -gt 15) {
        Write-Host "[X] 無效行號" -ForegroundColor Red
        return
    }
    $shas = git log --format='%H' -15
    $sha = $shas[[int]$idx - 1]
    New-BackupBranch
    git add -A
    git commit --fixup $sha | Out-Null
    # GIT_SEQUENCE_EDITOR=: 讓 autosquash 待辦自動接受，全程免編輯器
    $env:GIT_SEQUENCE_EDITOR = ':'
    git rebase -i --autosquash ($sha + "~1")
    Remove-Item Env:\GIT_SEQUENCE_EDITOR -ErrorAction SilentlyContinue
    Show-RebaseResult
}

function Invoke-SquashRecent {
    $n = Read-Host "把最近幾筆揉合成一筆？(至少 2)"
    if ($n -notmatch '^\d+$' -or [int]$n -lt 2) {
        Write-Host "[X] 至少 2 筆" -ForegroundColor Red
        return
    }
    Write-Host "將被揉合的提交：" -ForegroundColor Yellow
    git log --oneline -n ([int]$n)
    $msg = Read-Host "新的提交訊息"
    if (-not $msg.Trim()) { Write-Host "[X] 訊息不可空白" -ForegroundColor Red; return }
    New-BackupBranch
    git reset --soft ("HEAD~" + $n)
    git commit -m $msg
    Write-Host "[OK] 已揉合為一筆" -ForegroundColor Green
}


function Sync-WithBase {
    $base = Read-Host "基底分支名稱？（Enter = main）"
    if (-not $base.Trim()) { $base = 'main' }
    New-BackupBranch
    git fetch origin
    git rebase ("origin/" + $base)
    Show-RebaseResult
}

function Invoke-SafeForcePush {
    $remote = Read-Host "遠端名稱？（Enter = origin）"
    if (-not $remote.Trim()) { $remote = 'origin' }
    $branch = Get-CurrentBranch
    Write-Host ""
    Write-Host "[警告] 強推會改寫遠端歷史。確認沒有他人基於此分支工作，且已做過備份。" -ForegroundColor Yellow
    $c1 = Read-Host ("確定要強推 {0}/{1} ？(輸入 YES)" -f $remote, $branch)
    if ($c1 -ne 'YES') { Write-Host "[已取消]" -ForegroundColor Yellow; return }
    git push --force-with-lease $remote $branch
}

Assert-InGitRepo

while ($true) {
    Write-Host ""
    Write-Host "===== Git 提交整理工具 =====" -ForegroundColor Cyan
    Write-Host " [1] 現況總覽（log/status/diff）"
    Write-Host " [2] 備份當前狀態成備份分支"
    Write-Host " [3] 互動重整最近 N 筆（reword/squash/fixup/drop）"
    Write-Host " [4] 把工作區改動併入某筆舊提交（fixup 自動版）"
    Write-Host " [5] 揉合最近 N 筆為一筆（soft reset）"
    Write-Host " [6] 與基底分支同步（fetch + rebase）"
    Write-Host " [7] 安全強推（--force-with-lease）"
    Write-Host " [0] 離開"
    $choice = Read-Host "選擇"
    switch ($choice) {
        '1' { Show-Overview }
        '2' { New-BackupBranch }
        '3' { Invoke-InteractiveRebase }
        '4' { Invoke-FixupIntoOlder }
        '5' { Invoke-SquashRecent }
        '6' { Sync-WithBase }
        '7' { Invoke-SafeForcePush }
        '0' { exit 0 }
        default { Write-Host "[X] 無效選項" -ForegroundColor Red }
    }
}
