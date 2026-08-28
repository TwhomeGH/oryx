# Fork 定位與贊助管道說明

> 本文件回答一個關鍵問題：**你正在使用的這個 Oryx 和原項目是什麼關係？贊助應該給誰？**

---

## 1. 這個項目是什麼

本倉庫 `TwhomeGH/oryx` 是 [ossrs/oryx](https://github.com/ossrs/oryx) 的**社區維護分支（fork）**。

- **上游（原項目）**：[ossrs/oryx](https://github.com/ossrs/oryx) 及其背後整個 [SRS](https://github.com/ossrs/srs) 生態，由 SRS 團隊維護。
- **本 fork**：因上游疑似停止維護，本項目持續套用上游未處理的 PR、修復問題，並進行獨立演化。

兩者是**獨立項目**，只是代碼有共同祖先。本 fork 不追求與上游同步，會按本項目自己的路線發展。

## 2. 贊助管道明確區分

**這是本 fork 最想強調的一點：不要贊助錯地方。**

| 你想要的 | 該去哪裡 |
|---|---|
| 支持**本 fork**（TwhomeGH/oryx）的維護與開發 | **Twitch 頻道**：<https://www.twitch.tv/coffeelatte0709/about>（訂閱 / 打賞 / bits 皆可） |
| 支持**原項目 SRS** 的開發 | 原項目 [OpenCollective](https://opencollective.com/srs-server) |

> ⚠️ 贊助給原項目的 OpenCollective **不會**直接資助本 fork 的維護工作。
> 如果希望本 fork 繼續改進，請透過上面的 Twitch 管道支持。

## 3. 為什麼要區分

- **本 fork 的維護是獨立工作**：套用上游 PR、修 bug、加功能、寫文檔，都是本項目維護者投入的時間。
- **贊助原項目可能不會改善本 fork**：原項目用你的錢去改進 SRS 本體，本 fork 雖然也受益於 SRS 核心，
  但本 fork 特有的修復與功能不會因此得到資源。
- **「贊助原始項目，但本體沒改進」**的錯位情況正是本文件要避免的 — 明確管道，讓贊助者知道錢去了哪、支持了什麼。

## 4. 兩者關係（技術面）

- 本 fork 基於 SRS / FFmpeg / React.js / Go 建構，核心架構與上游一致。
- 本 fork **選擇性套用**上游有價值的 PR（用 `scripts/pick-pr.ps1` 挑取），不盲目追隨上游主線。
- 上游的教學、FAQ 大部分仍適用於本 fork（功能基礎相同），但有本 fork 特有差異時以本 repo 的 `docs/` 為準。

## 5. 其他支持方式（不花錢）

- 提 Issue / 提 PR / 參與討論 — 這是最好的貢獻。
- 在本項目 [GitHub](https://github.com/TwhomeGH/oryx) 給 Star。
- 翻譯或修正文檔、UI 文案。

## 6. GitHub 倉庫設定（維護者手動操作）

以下設定在 **GitHub 網頁**上，不是代碼，需維護者手動更新：

- **About → Website**：目前指向原 SRS 官網（`ossrs.io`）。若希望本 fork 的 GitHub 首頁不誤導用戶，
  可改為留空，或指到本 fork 的文檔 / 維護者個人頁。
- **About → Description**：可補充「Community-maintained fork of ossrs/oryx」字樣，與上游區分。
- **Funding 按鈕**：由 `.github/FUNDING.yml` 自動產生，已指向本 fork 的 Twitch 管道。

> 這些是「讓 GitHub 首頁清楚標示本 fork 獨立定位」的最後一步，建議有空時手動調整。
