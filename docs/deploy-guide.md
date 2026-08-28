# 部署指南

本文件是 Oryx 的部署入口。詳細操作（含本 fork 自有 GHCR 映像、版本更新、回退、備份）見下方各自獨立的文件。

## 快速開始（單一容器）

```bash
docker run --restart always -d --name oryx \
  -v oryx-data:/data \
  -p 2022:2022 -p 2443:2443 -p 1935:1935 \
  -p 8000:8000/udp -p 10080:10080/udp \
  ghcr.io/twhomegh/oryx:v5.15.20
```

啟動後打開 <http://localhost:2022> 進入管理介面。

> 重要：一定要掛載 `/data`，否則容器重建後所有設定與錄製資料都會消失。

### 主要埠

| 容器內埠 | 用途 | 協定 |
|---|---|---|
| 2022 | Web 管理介面（HTTP） | TCP |
| 2443 | Web 管理介面 / HTTPS-FLV 等 | TCP |
| 1935 | RTMP 推流、播放 | TCP |
| 1985 | SRS HTTP API | TCP |
| 8000 | WebRTC | UDP |
| 10080 | SRT | UDP |

> WebRTC WHIP 建議不要用 localhost / 127.0.0.1，改用私有 IP（如 `https://192.168.3.85`）、公網 IP 或域名。HTTPS 設定見 [教學](https://blog.ossrs.io/how-to-secure-srs-with-lets-encrypt-by-1-click-cb618777639f)。

### `/data` 數據目錄

| 子目錄 | 用途 |
|---|---|
| `.well-known` | Let's Encrypt ACME 驗證 |
| `config` | `.env`（密碼）、srs/redis/nginx/prometheus 設定、SSL 檔 |
| `dvr` | DVR 錄製檔 |
| `lego` | Let's Encrypt ACME 暫存 |
| `record` | 錄製檔 |
| `redis` | Redis 資料（推流密碼、錄製設定） |
| `signals` | 信號檔 |
| `upload` | 上傳檔 |
| `vlive` | 虛擬直播影片檔 |
| `transcript` | 字幕轉錄檔 |
| `nginx-cache` | Nginx 快取 |
| `srs-s3-bucket` | AWS S3 相容儲存掛載點 |

## 詳細文件

| 文件 | 內容 |
|---|---|
| [Docker 使用說明](docker-usage.md) | **本 fork 自有 GHCR 映像**、docker/compose 部署、埠對照、環境變數、版本更新與發布 |
| [本地打包 Docker 映像](local-build.md) | 本機直接建置映像並套用到 compose |
| [映像回退指南](rollback.md) | 新版出問題時用 sha 標籤/舊版號回退 |
| [硬體編碼配置](hardware-encoding.md) | GPU 轉碼（NVENC/QSV/VAAPI/AMF）在 compose 的透通配置 |

## 環境變數

> 完整環境變數清單見 [DEVELOPER.md](../DEVELOPER.md#environments)。

常用：

| 變數 | 預設 | 說明 |
|---|---|---|
| `MGMT_PASSWORD` | 自動生成 | 管理員密碼（也存於 `/data/config/.env`） |
| `REACT_APP_LOCALE` | `en` | UI 語言：`zh` / `en` / `ja` |
