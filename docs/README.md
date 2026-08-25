# Docs

本目录用于存放本项目新增的技术文档（Markdown），例如架构说明、部署指南、功能设计、修复记录等。

## 索引

- [教學：如何撿起（Cherry-Pick）上游的 PR 並合併](cherry-pick-pr-guide.md) — 從上游 ossrs/oryx 挑選未合併的 PR，逐步套用到本 fork 的完整流程與衝突處理。
- [Docker 使用說明](docker-usage.md) — 本 fork 自有映像（GHCR）的部署、埠對照、版本發布與更新回退流程。
- [SRS 分離部署配置說明](srs-host-separation.md) — SRS_HOST / SRS_HTTP_STREAM_PORT 環境變數，把 SRS 拆出單獨機器跑的配置方法。
- [本地打包 Docker 映像](local-build.md) — 不等 CI，本機一條指令建置映像並套用到 compose 的流程。
- [映像回退指南](rollback.md) — 新版出問題時，用 sha 標籤/舊版號/tar 快速退回正常狀態的操作手冊。
- [Forward 轉推功能架構](forward-architecture.md) — 轉推線的代碼地圖、FFmpeg 命令剖析、Redis 資料流與後續設計候選方向。
- [版本體系與升級指南](version-and-upgrade.md) — 產品版本 vs SRS 核心兩套版本號、SRS 主伺服器升級步驟、bump-version 一鍵工具。
