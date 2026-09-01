# 本地打包 Docker 映像

> 目的：改完程式碼想馬上驗證，不用等 GitHub Actions 排隊 + 建置（20~40 分鐘）。

## 前置需求

- Docker Desktop（建議 WSL2 後端，記憶體配額 ≥ 8GB，UI 的 npm build 吃記憶體）
- 磁碟空間約 10GB（基底映像 + 建置快取）
- **不需要**在本機安裝 Node.js 或 Go——所有編譯都在容器內完成

> 工具鏈備註（2026-08）：UI 已從 CRA 遷移到 **Vite**（Node 22）。
> `ui/vite.config.js` 內含 `%PUBLIC_URL%` / `%REACT_APP_LOCALE%` 佔位符替換、
> `.js` 檔的 JSX loader 設定與 dev proxy，屬於遷移關鍵配置，請勿刪除。

## 一條指令打包

> **必須在 clone 下來的 oryx 倉庫根目錄執行**（有 Dockerfile 的地方，例如 `F:\oryx`）。
> 在放 docker-compose.yml 的部署資料夾執行會報
> `failed to read dockerfile: no such file or directory`。

```bash
cd F:\oryx          # 換成你的倉庫路徑
docker build -t oryx:local .
```

這會在容器內依序完成：npm 安裝與 lint → 建置 UI（單一 bundle，多語系於 runtime 載入）→ 編譯 Go 平台 →
打包最終映像。

| 情境 | 預計耗時 |
|---|---|
| 第一次（無快取） | 30~60 分鐘（下載基底映像 + 全量編譯） |
| 之後再打包（有快取，程式有改動） | 15~30 分鐘（基底層走快取，但 make 階段會重跑） |
| 完全沒改程式再打包 | < 1 分鐘（全部命中快取） |

> 注意：所有原始碼是在**同一個 RUN 層**裡編譯的，所以任何 Go/UI 檔案變動
> 都會重新觸發整個 make（含 UI）。Docker 快取主要省下的是基底映像、apt、
> youtube-dl 那些前置層。

> 2026-08：Dockerfile 已**移除 UPX 壓縮**（`upx --best --lzma` 那段）。
> 原因：UPX-LZMA 壓縮的 `srs`/`platform` binary 在程序啟動時自解壓，
> 在 GitHub Actions runner kernel 上會偶發 SIGSEGV（零輸出立即退出），
> 導致 CI 整合測試偶發失敗（`Start SRS failed`）。詳細見
> [local-test.md 常見問題](local-test.md)。

## 把本地映像套到 compose

### 方法 A：改名頂替（推薦，compose 不用動）

```bash
# 打包完成後，把本地映像掛上 compose 正在用的 tag
docker tag oryx:local ghcr.io/twhomegh/oryx:v5.15.20

# 重啟服務；--pull never 表示只用本地映像，不觸發遠端拉取
docker compose up -d --pull never
```

### 方法 B：compose 直接指到本地 tag

```yaml
services:
  oryx:
    image: oryx:local
```

```bash
docker compose up -d
```

## 進階：比照 CI 建置

Dockerfile 的 `make -j build` 會自己從 context 的原始碼建 platform 和 UI
（不依賴預先建好的 `ui/build`，避免快取/過時問題），所以不用先在本機建 UI：

```bash
docker build -t oryx:local .
```

BuildKit 會依輸入內容快取，UI 沒變的重複建置很快。

日常驗證用第一種就好，不必這麼麻煩。

## 匯出給其他機器

本地建好的映像可以用檔案方式搬移：

```bash
docker save -o oryx-local.tar ghcr.io/twhomegh/oryx:v5.15.20
# 複製 tar 到目標機器後
docker load -i oryx-local.tar
```

## 本地 vs CI 的分工

| 用途 | 用哪個 |
|---|---|
| 改碼後快速驗證、自用部署 | 本地打包（即建即用） |
| 正式發布版號、給其他機器匿名拉取 | push 觸發 CI（產 GHCR 版號映像 + latest） |

兩者不衝突：本地先驗證沒問題，再 push 讓 CI 出正式版。

## Windows 已知坑：符號連結

repo 內有 Git 符號連結（如 platform/objs -> containers/objs）。
Windows 未開啟開發者模式時，checkout 會把它變成「內容為目標路徑的普通檔案」，
被複製進映像後 SRS 啟動會報 \open pid file=./objs/srs.pid: Not a directory\。

修復（一次性，本機已做過）：

```powershell
Remove-Item platform\objs -Force
New-Item -ItemType Junction -Path platform\objs -Target platform\containers\objs
git update-index --skip-worktree platform/objs
```

永久方案：Windows 設定開啟「開發人員模式」+ git config --global core.symlinks true，
之後重新 checkout 即為真符號連結。
