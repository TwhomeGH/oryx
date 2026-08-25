# Forward（轉推）功能架構與代碼地圖

> 用途：作為後續 forward 線改造／設計時的查閱基準。行號以 commit `60c8fb8` 前後為準，改動後請同步更新。

---

## 1. 功能定位

把 Oryx 收到的串流**原樣轉推**到外部平台（微信視頻號 / B站 / 快手 / 自訂 RTMP/SRT 伺服器）。
核心特徵：**純流複製（`-c copy`），不轉編碼、近乎零 CPU**。

前端入口：`ui/src/pages/ScenarioForward.js`。

> 注意：UI 上「视频编码器 * 暂时只支持软件编码器」這句文案屬於**轉碼場景**
> （`ScenarioTranscode.js` + `locale.json:271-286`），與 forward 無關——forward 不碰編碼器。

---

## 2. 代碼地圖（全部集中在 `platform/forward.go`）

| 元件 | 行號 | 職責 |
|---|---|---|
| `ForwardWorker` | :30 | 全域單例，持有 tasks map（key=platform） |
| `Handle()` | :49-195 | 註冊兩個 HTTP API（見下） |
| `Start()` | :205 | 為 Redis 裡每份配置起一個 `ForwardTask` goroutine；監聽新配置 |
| `ForwardConfigure` | :314 | 配置結構（見下欄位） |
| `ForwardTask.Run()` | :483-581 | 主迴圈：選流 → 啟動轉推 → 失敗退避重試 |
| `doForward()` | :583-701 | **FFmpeg 命令構造與程序管理（核心）** |
| `Restart()` | :422 | 配置更新時原地重啟 |
| `queryFrame()/updateFrame()` | :450/:440 | ffmpeg stderr 日誌幀快取，供 API 查詢進度 |

### 配置結構 `ForwardConfigure`

```go
Platform string // 平台識別：wx / bilibili / kuaishou / forwarding-*（自訂）
Stream   string // 來源串流名；空字串=自動挑最新活躍串流
Server   string // 目標伺服器，如 rtmp://xxx/live 或 srt://...
Secret   string // 串流金鑰/流名，拼接到 Server 後
Enabled  bool
Customed bool // 是否自訂平台
Label    string
```

### HTTP API

| 端點 | 方法語義 | 說明 |
|---|---|---|
| `/terraform/v1/ffmpeg/forward/secret` | action=`update` / `delete` / 查詢 | update：寫入並合併配置到 Redis `SRS_FORWARD_CONFIG`（hash，key=platform），若任務存活則觸發 `Restart()`；delete：僅允許 `forwarding-*` 自訂配置，HDel 配置＋`RemoveTask()` 停止並移除任務；不帶 action 時回傳全部配置 |
| `/terraform/v1/ffmpeg/forward/streams` | 查詢 | 列出所有配置＋各任務運行狀態（pid/stream/frame log/ready 時間） |

平台白名單校驗：必須是 `wx|bilibili|kuaishou` 或以 `forwarding-` 開頭（自訂平台）。

---

## 3. 資料流

```
UI(ScenarioForward.js)
   │ POST /terraform/v1/ffmpeg/forward/secret {action:update, platform, server, secret...}
   ▼
ForwardWorker.Handle ──► Redis SRS_FORWARD_CONFIG (hash: platform → JSON)
   │                              ▲
   │ Start() 輪詢新配置             │ 讀取
   ▼                              │
ForwardTask.Run(platform) ◄───────┘
   │ selectActiveStream():
   │   Redis SRS_STREAM_ACTIVE (hash) ← SRS on_publish/on_unpublish 回呼維護
   │   有指定 Stream 名 → 精確匹配；否則取 Update 時間最新的串流
   ▼
doForward(input):
   ffmpeg -re -i rtmp://localhost/<app>/<stream> -c copy -f flv <Server+Secret>
   │
   ├─ FFmpegHeartbeat 解析 stderr（速度行/frame 日誌）→ updateFrame → API 可查
   └─ 程序退出 → cleanup(kill pid) → saveTask → Run() 外層 3.5s 退避後重試
```

### 關鍵 Redis keys

| Key | 類型 | 內容 |
|---|---|---|
| `SRS_FORWARD_CONFIG` | hash | platform → ForwardConfigure JSON |
| `SRS_STREAM_ACTIVE` | hash | 由 SRS http hooks 維護的活躍串流清單 |
| 任務狀態 | hash | 各 task 序列化 JSON（pid/input/output/frame 等） |

---

## 4. FFmpeg 命令剖析（doForward :608-634）

```bash
ffmpeg
  -re                                # 按即時速率讀取
  [-rtsp_transport tcp]              # 僅當輸入是 rtsp://
  -i rtmp://localhost/<app>/<stream> # 本機 SRS（容器內 localhost）
  -c copy                            # ★ 核心：零轉碼純複製
  -f flv                             # rtmp(s):// 輸出用 flv
  [ -pes_payload_size 0 -f mpegts ]  # srt:// 輸出改用 mpegts
  <outputURL>                        # config.Server + config.Secret 拼接
```

- 輸出 URL 中 `localhost` 一律替換為 `localhost`（容器內語義保留）
- 程序管理：`exec.CommandContext` + `cmd.StderrPipe()`；
  `FFmpegHeartbeat.Polling()` 持續解析日誌判活，異常觸發 cancel → `cmd.Wait()`
- 重啟策略：正常結束後 300ms 輪詢間隔；出錯退避 3500ms；有 PID 時額外睡 1s 防止過快重啟

### 已知行為限制（設計改造的切入點）

1. **無轉碼能力**：來源 H.265/VP9 推到只收 H.264 的平台會直接失敗（copy 不轉換）
2. **自訂目標按需增減**：UI 改為「新增轉播目標」按鈕動態加入空槽（受 env.forwardLimit 上限），每個自訂槽有刪除按鈕（action=delete）；內建三平台不可刪。歷史遺留的英文標籤槽位可手動改名或直接刪除
3. **選流規則簡單**：空 Stream 名時「挑最新」可能選錯來源
4. **與 SRS 原生 forward 的取捨**：SRS 本身有協定層 forward（不經 ffmpeg、更省資源），目前未使用

---

## 5. 對照組：Transcode 線（結構高度相似）

| | Forward | Transcode |
|---|---|---|
| 後端檔案 | `forward.go` | `trancode.go`（注意檔名拼寫） |
| 核心函數 | `doForward()` :583 | `doTranscode()` :425 |
| FFmpeg 編碼 | `-c copy` 純複製 | `-vcodec libx264 -profile:v -preset:v -b:v -r 25 -g 50 -bf 0 -acodec aac...`（WebRTC 友善參數） |
| UI | `ScenarioForward.js` | `ScenarioTranscode.js`（「暫時只支持軟件編碼器」文案在這） |
| 配置 Redis key | `SRS_FORWARD_CONFIG` | `SRS_TRANSCODE_*` |

兩者的命令骨架（-re/-i/-rtsp_transport/flv|mpegts 封裝/heartbeat/退避重啟）幾乎相同，
**若要給 forward 加可選轉碼，可直接借鏡 trancode.go 的編碼參數段**。

---

## 6. 全專案 ffmpeg 呼叫點速查

| 檔案 | 位置 | 用途 | 轉碼 |
|---|---|---|---|
| `forward.go` | :634 | 轉推 | ✗ copy |
| `trancode.go` | :485 | 轉碼 | ✓ libx264/aac |
| `virtual-live-stream.go` | :1204 | 虛擬直播 | ✓ |
| `camera-live-stream.go` | :859 | 攝影機推流 | ✓ |
| `dvr-local-disk.go` | :1042 | HLS ts → MP4 封裝 | ✗ copy |
| `dubbing.go` | 多處 | 配音混流 | 部分 |
| `transcript.go` | :1616/:1862 | 轉錄抽音訊 | 抽軌 |
| `ocr.go` | :1150 | OCR 幀抽取 | 抽幀 |
| `ai-talk.go` | :63 | AI 對話音視訊 | ✓ |

---

## 7. B 方向深入分析：效能與穩定性（2026-08 討論結論：優先）

### 7.1 現況盘点——heartbeat 其實已經很成熟

`FFmpegHeartbeat`（utils.go:1732 起）現有能力：

| 能力 | 實現 |
|---|---|
| 存活偵測 | 解析 ffmpeg 每秒 cycle log 的 `time=`/`speed=`；`time=` 不變計數、10 秒無更新直接 kill 重啟 |
| 速度異常 | 連續過快（如 >1.5x，追幀）／過慢（0.5x）分別計數，可由輸入 URL query 覆蓋閾值 |
| 正常退出識別 | 「Exiting normally」判定，避免誤判為故障 |
| 最長時長保護 | `max-stream-duration` 參數可定時切斷 |
| 觀測性 | parsed/failed/notChanged 等計數＋extraLogs 收集，透過 `/forward/streams` 曝露 |

**結論：單程序層面的穩定性已經夠用，真正的短板在「任務生命週期策略」和「目標端健康」。**

### 7.2 兩條改造軌道

#### 軌道一（主力）：強化 ffmpeg 轉推的穩定性

外部平台（wx/B站/快手）的 ingest 都要求 `rtmp://server/app + 串流金鑰(secret)`，
而 **SRS 原生 forward 只會用「同名流」推送、無法改寫串流名/金鑰**——
這是硬傷，決定了對外平台只能繼續走 ffmpeg。可做的改進：

| # | 改進 | 現況 → 目標 |
|---|---|---|
| B1 | 指數退避重啟 | 固定 3.5s → 指數退避（3.5s→7s→14s…上限 60s）＋抖動，避免打死故障中的目標伺服器 |
| B2 | 失敗熔斷與狀態 | 無限重試 → 連續 N 次快速失敗後進入「熔斷」狀態，API 標記 `degraded=true`，間隔性半開探測 |
| B3 | 目標預檢 | 啟動前對 output host:port 做 TCP connect 探測（2s timeout），失敗直接標記原因，不浪費一次 ffmpeg 啟動 |
| B4 | 可觀測性增強 | `/forward/streams` 增加 `restartCount`、`lastError`、連續失敗次數欄位 |
| B5 | 輸入斷流感知 | 來源流 on_unpublish 時主動停掉任務（目前靠 ffmpeg 自然 EOF＋心跳兜底，延遲較高） |

#### 軌道二（選配）：自控目標走 SRS 原生 forward

適用場景：轉推到**自己可控的另一台 SRS／邊緣節點**（不需要金鑰改寫、接受同名流）。

- 實現掛點現成：`srsGenerateConfig()`（utils.go:625）本來就會重寫
  `containers/data/config/srs.vhost.conf` include 檔並呼叫 raw API `rpc=reload`
- 做法：vhost conf 追加 `forward <dest1> [dest2...];` 指令（多目的地支援程度需在
  SRS 5 上實測驗證），配置存 Redis，變更時觸發 regenerate+reload
- 收益：零額外程序、協定層直轉、SRS 自帶斷線重連；代價：
  - 僅 RTMP（無 SRT 輸出）
  - reload 是全域行為，需確認對在推流的影響
  - 無 per-task 幀日誌，觀測性下降（可用 SRS callback 彌補）
- UI 需區分「平台型（ffmpeg）」vs「中繼型（native）」兩種目標類型

### 7.3 建議實施順序

1. **B3 目標預檢**（小，立即可做）——✅ 已實作：`ProbeTCPServer()`（utils.go），
   `doForward()` 在啟動 ffmpeg 前以 2s timeout 對輸出位址做 TCP 撥測，
   失敗快速返回明確原因，交由既有 3.5s 退避重試循環處理
2. **B1+B2 退避與熔斷**（中，核心穩定性收益）
3. **B4 觀測欄位**（小，隨 B2 順手做）
4. **B5 來源斷流感知**（中）
5. **軌道二 native forward**（大，獨立 feature flag，驗證 SRS5 多目的地後再上）
