# 關於本專案

本倉庫是 **ossrs/oryx** 的社區維護分支。由於上游項目疑似停止維護，本分支會持續
套用上游未處理的 PR、修復問題，並進行獨立維護。

## 維護者

- GitHub：[TwhomeGH](https://github.com/TwhomeGH)
- 問題回報：[GitHub Issues](https://github.com/TwhomeGH/oryx/issues)

## 文檔

技術文檔位於倉庫的 [docs/](https://github.com/TwhomeGH/oryx/tree/main/docs) 目錄：

- 本地建置與部署
- 版本升級與回滾
- AI 服務擴展（本地 LLM）
- 轉發架構說明

## 上游社區

本項目基於 [ossrs/oryx](https://github.com/ossrs/oryx)，上游官方渠道：

- Discord: <https://discord.gg/bQUPDRqy79>
- Twitter: <https://twitter.com/srs_server>
- GitHub: <https://github.com/ossrs/oryx>

## 維護者社群

- 直播／Discord 群：<https://www.twitch.tv/coffeelatte0709/about>

## 贊助與支持

> ⚠️ 本 fork 與原項目 [ossrs/oryx](https://github.com/ossrs/oryx) 是獨立項目，**贊助管道完全分開**。
> 想支持本 fork 的維護，請使用下方管道；想支持原 SRS 則前往其 [OpenCollective](https://opencollective.com/srs-server)。

- 支持本 fork：<https://www.twitch.tv/coffeelatte0709/about>（訂閱／打賞／bits）
- 詳細說明見 [Fork 定位與贊助管道](https://github.com/TwhomeGH/oryx/blob/main/docs/fork-sponsorship.md)

## 自訂此頁面

這是預設內容。在資料卷中建立 `contact.md` 即可替換本頁：

1. 在 docker-compose 掛載的 data 目錄下新增 `contact.md`
2. 內容使用 Markdown 格式，支援表格、連結、圖片（圖片請使用完整 URL）
3. 重新整理頁面即可生效，無需重建映像

### 多語言（可選）

- 英文介面優先讀取 `contact.en.md`；不存在時直接顯示 `contact.md` 內容
- `contact.en.md` 支援**段落級覆蓋**：只需寫出要翻譯的段落（以 `## ` 分段），
  第 N 個段落會覆蓋主檔的第 N 段，其餘沿用主檔原文
