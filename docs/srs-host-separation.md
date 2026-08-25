# SRS 分離部署配置說明

> 對應上游 PR [#228](https://github.com/ossrs/oryx/pull/228)（已由本 fork 套用）。

## 這是什麼功能

Oryx 預設架構是「單容器全包」：Oryx 平台（Go）和 SRS 伺服器跑在同一個容器內，
平台透過 `127.0.0.1` 存取本機的 SRS。

套用 #228 之後，**SRS 的位址改成可配置**——你可以把 SRS 拆到另一台機器或另一個
容器跑，Oryx 平台遠端管理它。適合的場景：

- SRS 要吃滿頻寬/CPU，不想跟管理平台搶資源
- 一台平台管理多台邊緣 SRS
- 想升級 SRS 版本但不想動整個 Oryx 映像（搭配自訂 SRS 映像）

---

## 環境變數

| 變數 | 預設值 | 說明 |
|---|---|---|
| `SRS_HOST` | `127.0.0.1` | SRS 所在主機位址（IP 或域名），平台會用它存取 HTTP API (1985) 和 HLS 串流 (8080) |
| `SRS_HTTP_STREAM_PORT` | `8080` | SRS HTTP 服務埠，用於下載 HLS TS 分段 |

> 單容器部署**兩個都不用設**，行為與舊版完全相同。

---

## 分離部署配置範例

假設 SRS 跑在 `192.168.0.200`，Oryx 平台跑在另一台機器：

```yaml
services:
  oryx:
    image: ghcr.io/twhomegh/oryx:v5.15.20
    container_name: oryx
    restart: always
    environment:
      - "SRS_HOST=192.168.0.200"
      - "SRS_HTTP_STREAM_PORT=8080"
    volumes:
      - "${USERPROFILE}/OneDrive/桌面/Work/ORXY:/data"
    ports:
      - "882:2022"        # Web 管理介面
      - "633:2443"        # HTTPS
```

### 對端 SRS 的要求

遠端 SRS 必須：

1. **HTTP API (1985) 對平台開放**——平台要呼叫 `/api/v1/raw?rpc=reload`、
   clients 查詢等 API。注意 1985 是強大的 raw API，**不要暴露到公網**，
   只允許平台所在內網存取。
2. **HTTP 服務 (8080) 對平台開放**——錄製/DVR/VOD 功能靠它下載 TS 分段。
3. RTMP/WebRTC/SRT 等串流埠按實際使用開放。

### 錄製行為的變化

- 舊版：直接讀取 SRS 寫在本機磁碟的 TS 檔（所以必須同機）
- 新版：先確認平台本地有沒有該檔案，沒有就從 `http://SRS_HOST:SRS_HTTP_STREAM_PORT/<url>` 下載，
  以記憶體傳遞給錄製/DVR/VOD/轉錄/OCR 各 worker

因此分離部署時，錄製功能會產生「SRS → 平台」的一次 HTTP 下載流量，
屬正常現象。

---

## 其他附帶修正（同 PR）

| 修正 | 效果 |
|---|---|
| on_hls 路徑安全檢查 | 防 directory traversal 攻擊（解決 CodeQL 掃描告警） |
| SRS reload 失敗容錯 | reload 失敗只記警告日誌，不再中斷設定流程 |
| Redis 位址也可配置（`REDIS_HOST`/`REDIS_PORT` 已有預設值） | 既有能力，一併整理 |

## 驗證方式

分離部署啟動後：

```bash
# 在平台機上確認能打到遠端 SRS API
curl http://192.168.0.200:1985/api/v1/status

# 推流後看平台日誌有沒有 on_hls 下載記錄
docker logs oryx --tail 100 | grep -E "on_hls|Download ts"
```
