# Redis 架構與 Key 設計

> 用途：Oryx 平台後端用 Redis 儲存所有持久化狀態。本文件是**維護基準**——新增／修改 Key 前先查這裡，確保沿用既有命名與結構慣例。

## 1. 連線與基本設定

- 連線：`platform` 全域 `rdb`（`go-redis/v8`），host/port/password/db 由 `.env` 的 `REDIS_HOST`/`REDIS_PORT`/`REDIS_PASSWORD`/`REDIS_DATABASE` 控制。
- Redis 跑在**同一容器內**（`auto/start_redis` 啟動，`redis-server --daemonize yes --dir /data/redis`），資料落在掛載的 `/data/redis`，容器重建不丟（除非清 volume）。
- 存取一律用 `go-redis` 的 `redis.Nil` 判斷「key/field 不存在」——錯誤處理慣例：`err != nil && err != redis.Nil` 才是真錯誤。

## 2. Key 總覽（`platform/utils.go` 的 const 區塊）

所有 key 都是**大寫常數**，名稱即 Redis key 字串。分組如下：

### 2.1 系統與認證

| Key | 結構 | 用途 |
|---|---|---|
| `SRS_AUTH_SECRET` | Hash（field=`room-pub-{stream}` → publish secret） | 直播間推流鑒權密鑰 |
| `SRS_PLATFORM_SECRET` | Hash（`token`/`update`） | 平台 API secret，啟動時自動生成，`scripts/tools/secret.sh` 讀它寫入 `.env` |
| `SRS_SECRET_PUBLISH` | Hash | 推流 secret（stream key 尾綴） |
| `SRS_LOCALE` | String | 介面語言設定 |
| `SRS_FIRST_BOOT` | String | 首次啟動旗標 |
| `SRS_UPGRADING` / `SRS_UPGRADE_WINDOW` | String | 升級鎖與升級窗口 |
| `SRS_CACHE_BILIBILI` | String | Bilibili 相關快取 |
| `SRS_BEIAN` / `SRS_HTTPS` / `SRS_HTTPS_DOMAIN` | String | ICP 備案、HTTPS 設定 |
| `SRS_SYS_LIMITS` | Hash | 系統限制（vLive/camera 位元率上限等） |
| `SRS_SYS_OPENAI` | Hash | 全域 OpenAI 設定（secretKey/baseURL/organization） |

### 2.2 功能設定與任務（`*_CONFIG` / `*_TASK` 成對）

| Key | 結構 | 用途 |
|---|---|---|
| `SRS_FORWARD_CONFIG` / `SRS_FORWARD_TASK` | Hash（field=platform → JSON） | 多平台轉推：配置 + 任務 |
| `SRS_VLIVE_CONFIG` / `SRS_VLIVE_TASK` | Hash（field=platform → JSON） | 虛擬直播：配置 + 任務 |
| `SRS_CAMERA_CONFIG` / `SRS_CAMERA_TASK` | Hash | IP 攝影機 |
| `SRS_TRANSCODE_CONFIG` / `SRS_TRANSCODE_TASK` | Hash | 直播轉碼 |
| `SRS_TRANSCRIPT_CONFIG` / `SRS_TRANSCRIPT_TASK` | Hash（`global` → JSON） | AI 字幕：全域配置 + 任務 |
| `SRS_OCR_CONFIG` / `SRS_OCR_TASK` | Hash | OCR 辨識 |
| `SRS_DUBBING_PROJECTS` / `SRS_DUBBING_TASKS` | Hash | 配音（dubbing）專案 + 任務 |
| `SRS_ASR_BADWORD` | Hash（`global` → JSON） | ASR 過濾詞（badcase），**2026-08 新增** |

**結構慣例**：`*_CONFIG` 存使用者可編輯的設定（JSON 字串），`*_TASK` 存執行中任務狀態。CONFIG 的 field 是業務主鍵（platform/room/uuid），`TASK` 同步。部分 CONFIG 用 `global` 當單一 field（transcript/ocr/badword），因是全域單例。

### 2.3 錄製 / 雲儲存 / 點播

| Key | 結構 | 用途 |
|---|---|---|
| `SRS_RECORD_PATTERNS` | Hash | 本地錄製規則 |
| `SRS_RECORD_M3U8_WORKING` / `SRS_RECORD_M3U8_ARTIFACT` | Hash | 錄製中 m3u8 / 成品 artifact |
| `SRS_DVR_PATTERNS` / `SRS_DVR_M3U8_WORKING` / `SRS_DVR_M3U8_ARTIFACT` | Hash | 雲端 DVR（COS） |
| `SRS_VOD_PATTERNS` / `SRS_VOD_M3U8_WORKING` / `SRS_VOD_M3U8_ARTIFACT` | Hash | 雲端點播（VOD） |
| `SRS_VOD_COS_TOKEN` | String | VOD 上傳用的 COS token |

### 2.4 串流狀態與統計

| Key | 結構 | 用途 |
|---|---|---|
| `SRS_STREAM_ACTIVE` / `SRS_STREAM_SRT_ACTIVE` / `SRS_STREAM_RTC_ACTIVE` | Hash（field=streamURL → JSON） | 目前活躍串流（依協議分開） |
| `SRS_STAT_COUNTER` | Hash | 統計計數器（publish/play 次數） |
| `SRS_CONTAINER_DISABLED` | String | 停用的 container（功能） |
| `SRS_LIVE_ROOM` | Hash（field=roomUUID → JSON） | 直播間資料 |

## 3. 資料結構慣例

### 3.1 設定物件（`*_CONFIG`）

一律存 **JSON 字串**，Go 端對應結構體，帶 `Load()`/`Save()` 方法：

```go
// transcript.go
func (v *TranscriptConfig) Load(ctx context.Context) error {
    if b, err := rdb.HGet(ctx, SRS_TRANSCRIPT_CONFIG, "global").Result(); err != nil && err != redis.Nil {
        return errors.Wrapf(err, "hget %v global", SRS_TRANSCRIPT_CONFIG)
    } else if len(b) > 0 {
        if err := json.Unmarshal([]byte(b), v); err != nil { ... }
    }
    return nil
}
```

### 3.2 動態多語言 map（`SRS_ASR_BADWORD`，2026-08 引入）

`AsrBadWordConfig` 用 `map[string][]string`（語言碼 → 過濾詞陣列）取代固定欄位，**加語言零程式碼改動**——Redis 存什麼 key 就支援什麼語言：

```go
type AsrBadWordConfig struct {
    Badwords map[string][]string `json:"badwords"`
}
```

- 預設值在 `NewAsrBadWordConfig()`，未客製時 Load 不到 → 用預設。
- API：`POST /terraform/v1/ai-talk/badword/query` / `.../update`。
- **設計原則**：凡是「依語言／平台可擴展的清單」，優先考慮 map 而非固定欄位（對比舊的 `cnConsole`/`enConsole` 硬綁 zh/en 的失敗設計）。

## 4. 命名與擴展準則

1. **Key 一律用 `SRS_` 前綴的大寫常數**，集中在 `platform/utils.go` 的 const 區塊，並在 §2 表格補一行。
2. 設定用 `*_CONFIG`、任務用 `*_TASK`，成對出現。
3. Hash 的 field 是業務主鍵；「全域單例」型設定用 `global`。
4. 值能 JSON 就 JSON（方便除錯/遷移），別拆欄位存多個 key。
5. 刪除時用 `HDel` + `redis.Nil` 容錯；`*_TASK` 常與 worker goroutine 綁定，刪 config 後記得停任務。
6. **不要**重複造輪子——先查 §2 是否已有語意相同的 key。
