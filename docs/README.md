# Docs

本目录用于存放本项目新增的技术文档（Markdown），例如架构说明、部署指南、功能设计、修复记录等。

## 索引

- [PR 急救指南](pr-hygiene.md) — 分支改到亂七八糟時的重整手法（rebase -i/fixup/squash）＋通用交互工具 clean-commits.ps1。
- [硬體編碼（GPU 轉碼）配置指南](hardware-encoding.md) — compose 透通配置、組件頁卡片判讀表與排錯。
- [教學：如何撿起（Cherry-Pick）上游的 PR 並合併](cherry-pick-pr-guide.md) — 從上游 ossrs/oryx 挑選未合併的 PR，逐步套用到本 fork 的完整流程與衝突處理。
- [Docker 使用說明](docker-usage.md) — 本 fork 自有映像（GHCR）的部署、埠對照、版本發布與更新回退流程。
- [SRS 分離部署配置說明](srs-host-separation.md) — SRS_HOST / SRS_HTTP_STREAM_PORT 環境變數，把 SRS 拆出單獨機器跑的配置方法。
- [本地打包 Docker 映像](local-build.md) — 不等 CI，本機一條指令建置映像並套用到 compose 的流程。
- [映像回退指南](rollback.md) — 新版出問題時，用 sha 標籤/舊版號/tar 快速退回正常狀態的操作手冊。
- [Forward 轉推功能架構](forward-architecture.md) — 轉推線的代碼地圖、FFmpeg 命令剖析、Redis 資料流與後續設計候選方向。
- [版本體系與升級指南](version-and-upgrade.md) — 產品版本 vs SRS 核心兩套版本號、SRS 主伺服器升級步驟、bump-version 一鍵工具。
- [串流查詢 API：單流精確查詢](streams-query-api.md) — streams/query 支援 vhost/app/stream 參數，精確查詢單路串流狀態。
- [AI 服務擴展性指南](ai-model-config.md) — 接上本地 LLM（Ollama/LM Studio）或第三方 OpenAI 相容服務，自訂 ASR/聊天模型。
- [前端組件與頁面維護指南](frontend-guide.md) — 頁面骨架、排版慣例、i18n、能力探測卡片模式、Vite 特殊事項與常見炸點。
- [播放器頁面排版與 CSS 修改指南](player-layout-guide.md) — player.css 架構、本地預覽模式、常見排版修改範例。
- [本地整合測試教學](local-test.md) — 在本機跑 CI 同款整合測試的完整流程、偵錯旗標與常見問題排解。
- [API 認證架構與 Status Code 說明](api-auth-architecture.md) — middleware 認證流程、HTTP status code 對應邏輯、init/login 端點的無 auth 設計。
- [CodeQL 安全漏洞修復說明](security-fix-codeql.md) — 路徑穿越與反射型 XSS 修復細節、影響端點與安全建議。
