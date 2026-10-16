# 變更歷史（changelogkit）

本 fork 使用 **changelogkit**（ChangeLogTools）管理改動歷史。設定在 repo 根目錄 `.changelog.toml`，
歷史檔為 `docs/change-history.md`（最新在最上）。

## 日常使用

```bash
changelogkit                     # 啟動本機網頁 GUI（瀏覽／新增／編輯／刪除）
changelogkit add "一句話標題" [--type 修復] [--file a/b.go] [--date YYYY.MM.DD]
changelogkit list [--grep X] [--since YYYY.MM.DD]
changelogkit export _site        # 匯出唯讀靜態站
```

未安裝時用 `PYTHONPATH=<ChangeLogTools>/src python -m changelogkit …`。
`--locale zh-TW|zh-CN|ja|en` 可切換 CLI／網頁語系。

## 發佈時自動帶入

`scripts/bump-version.ps1 -Release -Notes "本次重點"` 會先呼叫 `changelogkit add` 追加一筆，
再 bump／commit／tag／push，讓每個版本都附帶改動歷史（找不到 changelogkit 時會略過並提醒）。

## 條目格式（最新在最上）

<!-- markdownlint-disable MD046 -->

````markdown
## YYYY.MM.DD HH:MM 一句話標題
**類型**: 修復 · **檔案**: `a/b.go`

### 問題 - 摘要
### 根因 - 摘要
### 修改 - 核心手法
- 其他改動

**相關文件**:
- [說明](version-and-upgrade.md)

---
````

<!-- markdownlint-enable MD046 -->

相對連結以「該檔所在目錄」為基準自動改寫成站內連結（動態站→檔案檢視頁、靜態站→匯出的文件頁）。

## 設定重點（`.changelog.toml`）

| 欄位 | 值 |
|---|---|
| `title` | 網頁標題 |
| `history` | `docs/change-history.md` |
| `exclude` | `platform/vendor/**`、`ui/node_modules/**`、`ui/build/**`、`releases/**`（開發索引排除建置產物） |
