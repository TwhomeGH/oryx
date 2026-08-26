# PR 急救指南：把改到亂七八糟的分支整理乾淨

> 適用對象：提 PR 的人。特別是那種「邊寫邊修、不知不覺 commit 爆量、
> 混進無關改動、訊息全是 fix/xxx/test」的分支。
> 實際案例參考：[OpenParsec#43](https://github.com/hugeBlack/OpenParsec/pull/43)
> （作者本人親身災難，後來整理成這篇方法論）。

---

## 1. PR 為什麼會亂？先對號入座

| 症狀 | 成因 |
|---|---|
| 60 筆 commit，其中 20 筆叫 "fix"、"test2" | 把 commit 當「本機存檔」用 |
| 一個 PR 同時改了三件事 | 主題沒切，順手修 |
| 跟上游 main 差了 200 個 commit | 開分支後從沒同步過 |
| review 意見修完又冒出新問題 | 在同一分支上繼續疊，沒有隔離 |

**心法**：commit 是給 reviewer 看的「步驟」，不是給你自己看的存檔點。
一個 PR 一個主題；步驟之間應該能獨立看懂。

## 2. 急救流程總覽

```
備份 → 盤點 → 重整歷史 → 同步基底 → 安全強推
 ①      ②        ③           ④         ⑤
```

每一步都先有退路（備份分支），才動歷史。

## 3. 用工具半自動完成（推薦）

倉庫附帶 `scripts/clean-commits.ps1`，**任何 git 專案都能用**：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/clean-commits.ps1
```

選單功能與手工等價指令：

| 選單 | 功能 | 等價手工指令 |
|---|---|---|
| 1 | 現況總覽 | `git log --oneline` / `git status` |
| 2 | 自動建備份分支 | `git branch backup-<名>-<時間>` |
| 3 | 互動重整最近 N 筆 | `git rebase -i HEAD~N` |
| 4 | 工作區改動併入某筆舊提交 | `git commit --fixup <sha>` + `rebase -i --autosquash` |
| 5 | 揉合最近 N 筆為一筆 | `git reset --soft HEAD~N` + `git commit` |
| 6 | 同步基底分支 | `git fetch origin` + `git rebase origin/main` |
| 7 | 安全強推 | `git push --force-with-lease` |

工具在每次改歷史前自動建備份分支；fixup 流程用
`GIT_SEQUENCE_EDITOR=':'` 讓 autosquash 全自動接受，不需要碰編輯器。

## 4. 核心武器詳解（想手工操作時）

### 4.1 `git rebase -i HEAD~N`：互動重整

打開的待辦清單裡，把開頭動詞改掉即可：

| 動詞 | 效果 |
|---|---|
| `pick` | 原樣保留 |
| `reword` | 改動不變，改訊息 |
| `squash` | 併入上一筆，兩條訊息合併讓你編輯 |
| `fixup` | 併入上一筆，丟棄這條訊息 |
| `drop` | 刪掉這筆 |

行**順序**也可以直接調換＝重新排序提交。

### 4.2 fixup + autosquash：最常用的「補漏」手法

reviewer 說「第 3 筆少了一行」，補完後：

```bash
git add -A
git commit --fixup <第3筆的sha>
GIT_SEQUENCE_EDITOR=: git rebase -i --autosquash <第3筆sha>~1
```

新 commit 會自動飛到目標旁邊並被吸收，全程免編輯器。

### 4.3 `reset --soft`：重新分組

```bash
git reset --soft HEAD~8    # 8 筆全部退回暫存區，改動一個不少
git commit -m "feat: 完整的主題描述"
```

要拆更細就 `git add -p` 按 hunk 分批加入再多次 commit。

### 4.4 cherry-pick：只撿需要的改動

```bash
git fetch upstream pull/43/head     # 或任意來源分支
git log FETCH_HEAD --oneline -5    # 找出要的那幾筆
git cherry-pick <sha>               # 逐筆撿；加 -x 會在訊息註明來源
# 衝突時：解決 → git add . → git cherry-pick --continue
# 反悔：  git cherry-pick --abort
```

## 5. 與基底同步＋強推規範

- 同步一律 **rebase** 不要 merge（避免「Merge branch 'main' of…」雜訊）
- 衝突逐筆解決：改檔 → `git add` → `git rebase --continue`
- 強推**必須**用 `--force-with-lease`（遠端若被別人更新過會拒絕，防誤殺）
- **絕對不要**強推共享分支（別人基於它工作）；自己的 feature 分支隨便推

## 6. 預防勝於急救

1. 開分支就先 `git fetch && git rebase origin/main`
2. 小步提交，但**推送前**用本文工具整理一次
3. PR 描述寫清楚主題邊界——發現超出範圍的改動，切出去另開 PR
