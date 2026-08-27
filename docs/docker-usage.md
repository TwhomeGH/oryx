# Docker 使用說明

本文件說明如何使用**本專案（fork）自己發布的 Oryx Docker 映像**部署服務，
以及如何發布新版本、如何更新線上環境。

---

## 1. 映像資訊

| 項目 | 內容 |
|---|---|
| 映像位置 | `ghcr.io/twhomegh/oryx` |
| 版本標籤 | `v5.15.20` 這類版本號，對應 `platform/version.go` 的 `const version` |
| 浮動標籤 | `latest` 永遠指向 main 分支最新的成功建置 |
| 自動建置 | push 到 `main` 或打 `v*` 標籤時，由 `.github/workflows/docker-publish.yml` 自動建置推送 |
| 標籤策略 | `:版本號` 與 `:latest` 會被同版推送覆蓋；`:sha-xxxxxxx` 綁定 commit 不可變，供回退 |
| 套件清理 | `.github/workflows/package-cleanup.yml` 每日刪除無標籤殘留；`sha-*` 建置滾動保留最近 20 個（`:latest`/`:v版號` 受保護永不刪） |
| 支援架構 | 目前僅 `linux/amd64` |

> 注意：官方的 `ossrs/oryx` 與本 fork 的映像互不相干。本 fork 的 release workflow 已停用，
> 一律使用 GHCR 上的自有映像。

---

## 2. 首次使用前的準備

GHCR 的套件首次發布後預設是**私有的**，需要先改成公開才能匿名 `docker pull`：

1. 到 GitHub 倉庫頁面右側 **Packages** → 點擊 `oryx`
2. **Package settings** → 滾動到 **Danger Zone** → **Change visibility** → 選 **Public**

之後任何機器都可以直接拉取，不需登入。

---

## 3. 快速開始（單一容器）

```bash
docker run --restart always -d --name oryx \
  -v oryx-data:/data \
  -p 2022:2022 -p 2443:2443 -p 1935:1935 \
  -p 8000:8000/udp -p 10080:10080/udp \
  ghcr.io/twhomegh/oryx:v5.15.20
```

啟動後打開 <http://localhost:2022> 進入管理介面。

> 重要：一定要掛載 `/data`，否則容器重建後所有設定與錄製資料都會消失。

---

## 4. 使用 docker-compose（推薦）

實際使用的 `docker-compose.yml` 範例（Windows 主機，含自訂埠）：

```yaml
services:
  oryx:
    image: ghcr.io/twhomegh/oryx:v5.15.20
    container_name: oryx
    restart: always
    volumes:
      - "${USERPROFILE}/OneDrive/桌面/Work/ORXY:/data"
    ports:
      - "882:2022"        # Web 管理介面 (HTTP)
      - "633:2443"        # Web 管理介面 (HTTPS)
      - "1936:1935"       # RTMP 推/拉流
      - "19860:1985"      # SRS HTTP API
      - "8000:8000/udp"   # WebRTC (UDP)
      - "10080:10080/udp" # SRT
```

### 埠對照表

| 容器內埠 | 用途 | 協定 |
|---|---|---|
| 2022 | Web 管理介面（HTTP） | TCP |
| 2443 | Web 管理介面 / HTTPS-FLV 等 | TCP |
| 1935 | RTMP 推流、播放 | TCP |
| 1985 | SRS HTTP API | TCP |
| 8000 | WebRTC | UDP |
| 10080 | SRT | UDP |

左邊是主機埠，可自行更換（例如範例用 882 對應容器內的 2022）；
換埠後瀏覽器要改用新的位址，推流域名也要跟著調整。

### 啟動與停止

```bash
docker compose up -d        # 啟動
docker compose down         # 停止並移除容器（資料在 volume，不會丟）
docker compose logs -f      # 看即時日誌
docker compose ps           # 查看狀態
```

---

## 5. 更新版本

### 發布端（fork 維護者）

1. 修改 `platform/version.go`：

   ```go
   const version = "v5.16.0"
   ```

2. commit 後 push 到 `main` → GitHub Actions 自動建置並推送 `ghcr.io/twhomegh/oryx:v5.16.0` 和 `:latest`

### 部署端（跑服務的機器）

更新時只需改 compose 裡的 image tag，然後重建容器：

**固定版本（建議）** — 明確指定版號，升級可控、出問題好回退：

```yaml
services:
  oryx:
    image: ghcr.io/twhomegh/oryx:v5.16.0   # ← 改成新版號
```

**追 latest** — 每次 pull 都拿到最新版，但可能引入 breaking change：

```yaml
services:
  oryx:
    image: ghcr.io/twhomegh/oryx:latest     # ← 保持 latest
```

改完後執行：

```bash
docker compose pull && docker compose up -d
```

### 回退版本

> 完整的回退操作手冊（含 `sha-*` 標籤回退、緊急 tar 還原）見 [映像回退指南](rollback.md)。

把 compose 的 tag 改回舊版號再 `up -d` 即可，舊版映像還留在本機：

```bash
docker images | grep oryx          # 看本地有哪些版本
docker compose up -d               # 套用改回的舊版號
```

---

## 6. 資料備份

所有持久化資料都在 `/data` 掛載點（範例中是 `Work\ORXY` 資料夾），備份該資料夾即可。
搬移主機時整個資料夾複製過去、compose 起起來就完成遷移。
