# 教學：如何撿起（Cherry-Pick）上游的 PR 並合併

本文件說明如何把 [ossrs/oryx](https://github.com/ossrs/oryx) 上游尚未被合併的 PR，逐步套用到我們自己的 fork 上。
整個流程的核心是 **fetch 上游的 PR ref → 挑出 commit → cherry-pick 到自己的分支**。

> **懶人首選**：專案內建了互動式腳本 `scripts/pick-pr.ps1`，自動查詢 PR 的精確 commit 清單、
> 逐一預覽、確認後套用、詢問推送，衝突時給中文指引：
>
> ```powershell
> # 只看不改（先評估這個 PR 改什麼）
> powershell -ExecutionPolicy Bypass -File scripts\pick-pr.ps1 247 -DryRun
>
> # 實際套用（會逐步跟你確認）
> powershell -ExecutionPolicy Bypass -File scripts\pick-pr.ps1 247
> ```
>
> 手動流程仍然值得讀懂——腳本只是幫你把下面每一步串起來，出狀況時你才知道發生什麼事。

---

## 0. 名詞解釋

| 名詞 | 意思 |
|---|---|
| upstream | 上游原始倉庫（`ossrs/oryx`） |
| origin | 你自己的 fork（`TwhomeGH/oryx`） |
| PR ref | GitHub 為每個開啟中的 PR 自動產生的引用：`refs/pull/<編號>/head`，指向該 PR 分支的最新 commit |
| cherry-pick | 「撿櫻桃」：只把指定的某幾個 commit 複製到目前分支，而不是合併整個分支 |

為什麼用 cherry-pick 而不是 merge？
因為我們的 fork 是獨立維護，只想挑選有價值的修復，一次一個 PR，
不會把上游 main 的所有歷史（包含不需要的改動）一起帶進來。

---

## 1. 前置設定（只需做一次）

確認 upstream remote 已存在：

```bash
git remote -v
```

如果沒有 upstream，新增它：

```bash
git remote add upstream https://github.com/ossrs/oryx.git
git fetch upstream
```

---

## 2. 找到目標 PR

到上游的 PR 頁面瀏覽：<https://github.com/ossrs/oryx/pulls>

每個 PR 網址都有一個編號，例如 <https://github.com/ossrs/oryx/pull/248> 就是 **PR #248**。
在 PR 頁面可以確認：

- 標題與說明（改了什麼、為什麼改）
- 有幾個 commit（頁面顯示 "N commits"）
- 改動哪些檔案（Files changed 頁籤）

> 小技巧：想快速看純改動內容，可在 PR 網址後面加 `.diff` 或 `.patch`，
> 例如 <https://github.com/ossrs/oryx/pull/248.diff>，就能直接看到完整差異文字。

---

## 3. 把 PR 的 commits 抓下來

GitHub 會把每個開啟中的 PR 放在 `pull/<編號>/head` 這個 ref 上，直接 fetch 即可（不需要知道作者的 fork 網址）：

```bash
git fetch upstream pull/248/head
```

抓完後 `FETCH_HEAD` 就指向 PR 的最新 commit。先看看這個 PR 有哪些 commit：

```bash
# 列出 PR 最新 N 個 commit（N = PR 頁面顯示的 commit 數）
git log --oneline FETCH_HEAD~2..FETCH_HEAD
```

輸出範例（PR #248 有 2 個 commit）：

```
066ace6 upgrade actions from v3 to v4
583e2c4 replace action runner from ubuntu-20.04 -> ubuntu-22.04
```

也可以先預覽整個 PR 的改動，確認沒問題再撿：

```bash
git diff FETCH_HEAD~2..FETCH_HEAD --stat   # 只看改了哪些檔案
git diff FETCH_HEAD~2..FETCH_HEAD          # 看完整內容
```

---

## 4. Cherry-pick 到自己的分支

確認目前在 `main` 且工作區乾淨：

```bash
git switch main
git status
```

依「由舊到新」的順序逐個撿（多個 commit 時順序很重要）：

```bash
git cherry-pick 583e2c4 066ace6
```

單一 commit 的 PR 只要撿一個：

```bash
git cherry-pick <SHA>
```

完成後用 `git log --oneline` 檢查，commit 會帶著原作者的署名進到我們的歷史裡。

### 如果發生衝突（conflict）

代表本地檔案和 PR 基於的版本有出入。處理方式：

```bash
git status                # 看哪些檔案衝突
# 手動編輯衝突檔案，解決 <<<<<<< ======= >>>>>>> 標記
git add <解決完的檔案>
git cherry-pick --continue
```

中途反悔想全部放棄：

```bash
git cherry-pick --abort
```

---

## 5. 驗證

撿完後一定要驗證改動確實生效、沒有殘留舊內容。
例如 PR #248 是把 workflow 從 ubuntu-20.04 升級，就搜尋確認沒有殘留：

```bash
rg "ubuntu-20\.04" .github/workflows    # 沒有任何輸出 = 乾淨
```

如果是程式碼類的 PR，再跑對應的建置或測試。

---

## 6. 推送到自己的 fork

```bash
git push origin main
```

---

## 7. 完整流程速查（以 PR #248 為例）

```bash
git fetch upstream pull/248/head        # 1. 抓 PR ref
git log --oneline FETCH_HEAD~2..FETCH_HEAD   # 2. 看 commit 清單
git switch main && git status           # 3. 確認分支乾淨
git cherry-pick 583e2c4 066ace6         # 4. 由舊到新撿上來
rg "ubuntu-20\.04" .github/workflows    # 5. 驗證無殘留
git push origin main                    # 6. 推送
```

---

## 8. 其他替代做法

| 做法 | 指令 | 適用情境 |
|---|---|---|
| 套用 patch 檔 | `curl -L <PR網址>.diff \| git apply` | 不想要原署名、想自己重寫 commit 時；缺點是不保留作者資訊 |
| 直接手動改 | 照 `.diff` 內容自己編輯檔案 | PR 很小（改一兩行）、cherry-pick 衝突難解時最省事 |
| merge PR 分支 | `git merge FETCH_HEAD` | 想保留 PR 完整歷史；但會把該分支所有祖先 commit 關聯進來，獨立 fork 不建議 |

日常維護以 **cherry-pick 為主**，其他做法當備援。

---

## 9. 實戰記錄

### 案例 A：多個 commit 的 PR（#248）

PR 頁面顯示 **2 commits**，一個改 runner、一個升級 actions 版本：

```bash
git fetch upstream pull/248/head
git log --oneline FETCH_HEAD~2..FETCH_HEAD
# 066ace6 upgrade actions from v3 to v4        ← 新
# 583e2c4 replace action runner ...            ← 舊
git cherry-pick 583e2c4 066ace6                # 由舊到新依序列出
```

### 案例 B：單一 commit 的 PR（#247）

只改一行設定檔（SRS HTTP API 移除 127.0.0.1 綁定），一個 commit：

```bash
git fetch upstream pull/247/head
git log --oneline -3 FETCH_HEAD     # 尖端是 b608793，只有這一個是 PR 自己的 commit
git show b608793                    # 預覽：確認就那一行
git cherry-pick b608793             # 撿上來
```

### 判斷「哪些 commit 屬於這個 PR」

最可靠的方式：**PR 頁面寫了幾個 commits，就從 `FETCH_HEAD` 往回數幾個**：

```bash
git log --oneline FETCH_HEAD -<commit數>
```

再往下看到的（如 `Update README.md`）就是上游 main 的歷史，**不屬於**這個 PR。

### 千萬不要拿兩個分支尾端直接 diff

```bash
# ✗ 錯誤示範：會混入大量不相干內容
git diff HEAD..FETCH_HEAD

# ✓ 正確：看單一 commit 的實際改動
git show <SHA>
```

原因：PR 分支是從上游較舊的位置切出去的，兩邊尾端比較時，
我們自己的所有修改都會被算成「差異」，看起來嚇人但完全不是 PR 的內容。

---

## 10. 常見錯誤對照表

| 錯誤訊息 / 現象 | 原因 | 正確做法 |
|---|---|---|
| `fatal: invalid refspec 'https://...'` | 把 PR 網址直接貼給 fetch | fetch 要用 refspec 格式：`git fetch upstream pull/<編號>/head` |
| `git log FETCH_HEAD~N..FETCH_HEAD` 沒輸出 | 語法或範圍問題 | 改用 `git log --oneline FETCH_HEAD -N` 直接列尖端 |
| diff 出現一大堆莫名檔案 | 拿分支尾端互相比較 | 用 `git show <SHA>` 看 commit 本身 |
| `error: could not apply ...` | cherry-pick 衝突 | 解衝突 → `git add` → `--continue`；反悔就 `--abort` |
| cherry-pick 完忘記驗證 | — | 一定要搜尋確認舊內容清乾淨、跑相關測試 |

> 小抄：整個流程背不起來沒關係，只要記住一句話——
> 「fetch 抓 PR、log 找 SHA、show 看內容、pick 撿上來」。
