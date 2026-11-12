# Forward Program Bus (轉推不斷線)

> 目標：多平台轉推（forward）時，**來源中斷不應讓下游（Twitch/YouTube/…）斷線**。
> 本文記錄經實測驗證的設計與根因。

## 1. 問題

下游 `forward` 把本機來源流轉推到目標平台。若來源 encoder 抖動/重連，本機來源消失，
原本實作是一條「來源活著才跑、來源一消失就殺」的衍生 ffmpeg，於是下游立即斷線。
N 個平台就放大成 N 次斷線。

## 2. 根因（實測）

在原設計（relay 讀 RTMP 來源、轉推 fanout 讀 program）下，實測發現：

1. 來源 encoder 斷線 → 來源 RTMP EOF。
2. **relay 讀的是 RTMP 輸入，而 FFmpeg 對 RTMP 沒有 auto-reconnect**，因此 relay 進程
   直接退出。
3. relay 是 program 的 publisher，它一退出，**program 就被 SRS 取消發布**。
4. 約 0.9s 後，fanout（program 的 consumer）收到 EOF 並退出（SRS 日誌 `client disconnect
   peer. ret=1007`），下游斷線。

> SRS 源碼 `SrsLiveSource::on_unpublish()`（`trunk/src/app/srs_app_source.cpp`）本身
> **不會**主動斷開 `consumers`；問題不在 SRS 踢人，而在「program 失去了 publisher」。

結論：**要讓下游不斷線，program 就絕不能失去它的 publisher。**

## 3. 設計

核心：**program 的 relay 與下游的 fanout 都「用 HTTP-FLV + reconnect 讀取」**，把 publisher
連線與來源解耦。

- **program relay** 用 HTTP-FLV 讀取來源（`http://localhost:2022/<app>/<stream>.flv`）。來源
  離線時，reconnect 讓 **relay 進程與其對 program 的 publish 連線一直活著**（只是暫時沒有
  資料），program 維持「已發布」，SRS 不會斷開 fanout。來源回來時 relay 自動續傳。
- **fanout 也用 HTTP-FLV + reconnect 讀取 program**。這讓 fanout 能承受 **program publisher
  的切換**（例如 slate 模式）：切換只讓 fanout 的 *輸入* 短暫 EOF 並重連，而其對下游的
  *輸出* 連線不動 → 下游不斷線。

reconnect 參數：`-reconnect 1 -reconnect_at_eof 1 -reconnect_streamed 1
-reconnect_on_http_error 4xx,5xx -reconnect_delay_max <N>`。用 `4xx,5xx`（而非僅 404）是因為
平台把 `/live/*.flv` 反向代理到 SRS，串流不存在時可能回 **502**。

### 兩種 failover 模式

每個 forward 目標有 `failover` 設定：

- `hold`（預設，空值同義）：來源離線時下游**凍結在最後一幀**（hold-last-frame），即
  「複製最後一個可用畫面」。**零轉碼**（全鏈 `-c copy`）。
- `slate`：來源離線時，ProgramBus 把 program 的 publisher 由 live relay 切成**預生成的備用
  畫面**（純色 + 靜音 mp4，`-stream_loop -1 -c copy`），來源回來再切回 live。因為 fanout 用
  HTTP-FLV+reconnect，切換造成的空窗只讓 fanout 重連輸入，**下游輸出連線不斷**。slate 本身
  也是 `-c copy`，**不轉碼**。

### 元件

- `platform/forward-program.go`
  - `ProgramWorker`：管理所有 program bus（引用計數 `Ensure/Get/Release`）。
  - `ProgramBus`：為每個來源維護 program。`hold` 時常駐 relay；`slate` 時在 live relay 與
    slate 之間切換（`forwardSlateDownDelay=2s` 去抖）。每秒輪詢 `SRS_STREAM_ACTIVE` 更新來源
    在線狀態。
  - slate 資產：`ensureForwardSlate` 首次產生 `oryx-forward-slate.mp4`（1280x720 H.264 + AAC）。
- `platform/forward.go`
  - `ForwardWorker` 持有 `programWorker`。
  - `ForwardTask` 首次選中來源時 `bindProgram`，之後只讀 program；不再因來源消失而停 fanout。
  - fanout 啟動前 `waitStreamReady` 先等 program 就緒：HTTP-FLV 若對「尚未發布」的流發起
    **初始**連線會硬失敗，而 FFmpeg 不會自動重試初始連線，故必須先等到 200。
  - fanout 心跳放寬：`forwardFanoutNoUpdateTime = 2 * time.Minute`
    （`FFmpegHeartbeat.MaxNoUpdateTime`）。因來源離線時 fanout 合法地不產出幀，不能像一般
    任務 10s 就殺。
  - 來源選擇 `ownOutputStream` guard：不選到自己的輸出流（避免自迴圈）。
  - `Restart()` 重置 `restartCount`（任務重啟視為全新開始）。
- `platform/srs-hooks.go`：`isForwardProgramStream` 對 `__forward_` 流直接跳過
  → **不寫入 `SRS_STREAM_ACTIVE`**，program 不會出現在串流清單或任何功能中。
- 認證：program publish URL 帶 `?secret=<pubSecret>` 以通過 on_publish（只驗證、不落 Redis）。
- UI：`ScenarioForward.js` 有 failover 選擇器；清單顯示 fallback 標記。

## 4. 落地狀態

### P1 ✅
常駐 relay（HTTP-FLV + reconnect）、`ForwardTask` 改讀 program、`__forward_` hook 隱藏、
fanout 心跳放寬、UI fallback 標記。

### P2 ✅
fanout 改讀 HTTP-FLV+reconnect（使 program 可切換 publisher 而下游不斷）、`waitStreamReady`、
自我輸出 guard、`RestartCount` 重置、狀態 API 暴露
`programMode/programFailover/fallback/programSwitches`。

### P3 ✅
`failover=slate` 可選備用畫面（publisher 切換式，零轉碼）。

## 5. 驗收（如何證明真的不斷線）

整合測試（需 `oryx:local` 容器 + `test/oryx.test.exe`，見 `docs/local-test.md`）：

1. `TestScenario_WithStream_PublishRtmpForwardKeepalive`（hold）：
   推來源 → `programMode=live`、`fallback=false`；**殺來源** → 仍 `status=running`、
   `programMode=live`、`fallback=true`，且 `restartCount` 不變（fanout 未重啟，下游未中斷）；
   再推來源 → `fallback=false`。
2. `TestScenario_WithStream_PublishRtmpForwardSlate`（slate）：
   殺來源 → `programMode=slate`、`fallback=true`，`restartCount` 不變；再推來源 →
   `programMode=live`。
3. `TestScenario_WithStream_PublishRtmpForwardPlatform`（基本轉推可播放）。

> `restartCount` 斷言用「觀測到的基準值不變」而非 `==0`：同一 task 跨測試會累積，且測試
> cleanup 殺 fanout 會被計為一次失敗。

三條測試皆在 `oryx:local` 容器實跑通過。

## 6. 已知限制

- 需來源可經 HTTP-FLV 取得（一般 RTMP/SRT 推流都會 mount `.flv`；純 WebRTC-only 可能沒有）。
- 停頓上限受 fanout 心跳（2 分鐘）與下游平台容忍度限制。
- 平台代理在串流未發布時可能回 502，故 reconnect 需含 `5xx`。
- slate／composite 的尺寸與幀率取自來源探測（`ffprobe`），探測失敗才退回 1280x720@25；換手瞬間若解析度改變，下游仍可能重建解碼器。
- 自動來源（`config.Stream` 空）目前「首次綁定後固定」，尚未跟隨最新來源。

## 7. 恢復：為何要重啟 fanout（實測修正）

隔離實驗發現：**FFmpeg 的 HTTP-FLV `-reconnect` 在串流被取消發布再重新發布後，不會恢復**
——consumer 會卡在失敗的重連迴圈（`Will reconnect … error=Input/output error`），即使直連
SRS:8080 也一樣。因此「靠 relay 的 HTTP-FLV 重連撐住 program」無法撐過恢復階段，fanout 也
不會自動讀到換 publisher 後的新內容（曾造成恢復後永久凍結）。

修正：
- `runHold`：來源恢復（sourceActive false→true）時**重啟 relay**，讓 program 乾淨地重新發布。
- `doForward`：偵測到 **program 換 publisher**（`programSwitches` 或 **`programGeneration`**
  變動）時**重啟 fanout**，以全新連線讀到恢復後的 program。

> `programGeneration` 是後來補上的關鍵：program 程序被 SIGKILL 後，重試會以**同一個模式**
> 重新發布，`programSwitches` 不會變動，fanout 因此永遠卡在已死的 HTTP-FLV 連線上（實測
> fps 凍結在 41.6 且永不恢復）。改為每次發布程序（含重試）都遞增 generation 後，實測
> `kill -9` 節目源 → **約 8 秒**恢復（`waiting_input` → `running`，fps 回到 60）。

代價：恢復時下游會**重連一次**；中斷期間下游仍**不斷線**（凍結最後一幀）。
整合測試 `TestScenario_WithStream_PublishRtmpForwardKeepalive` 已加入「輸出必須恢復」斷言。

## 8. 節目源保證什麼、不保證什麼

節目源（program bus）是**解耦用的穩定層**，不是「永遠不會壞」的備援流。把它誤解成後者，
會在下游定格或節目源程序崩潰時誤判「轉播沒問題」。

### 保證

| 保證 | 機制 |
|---|---|
| 下游只讀一條穩定的「節目源」 | 各目標 fanout 全部讀 `__forward_<app>_<stream>`，不再各自綁來源 |
| 來源短暫消失時下游**不掉線** | relay 用 HTTP-FLV `-reconnect` 撐住，維持 program 的發布者身分 |
| 只有一條連線受來源影響（不是 N 條） | 來源中斷只影響 relay 這一段 |

### 不保證

| 情境 | 實際行為 |
|---|---|
| 來源中斷（`failover=hold`，預設） | 下游**凍結最後一格**：連線還在，但沒有新畫面 |
| 來源中斷（`failover=slate`） | 時鐘畫面持續輸出，但整條管線持續編碼（NVENC） |
| **來源缺席超過發布逾時** | 節目源讀不到輸入 → 沒有資料可送 → 仍會被判定死亡；`hold` 只保證**短於逾時**的中斷。SRS `publish.normal_timeout` 已放寬到 15 秒，且節目源停滯 10 秒會主動重建（事件 `program-stall`） |
| **節目源程序被殺／崩潰** | program 短暫消失，fanout 重建 → 約 **5–8 秒**中斷 |
| 來源內容損壞／時間戳跳躍 | **不修**，原樣轉送；跳躍超過門檻才自動重啟管線 |

### 成本（實測，copy 模式）

| 項目 | 數值 |
|---|---|
| program 程序 CPU | 約 0.4% |
| program 程序記憶體 | 約 43 MB |
| 轉碼模式（normalize／slate） | 使用 NVENC，約 20–40% GPU |

### 監測盲點

`hold` 模式讓來源的短暫中斷在**下游看不出來**（畫面定格但連線未斷），遠端平台的統計不會
記錄該次中斷。唯一可見依據是 Oryx 本地：

- 事件時間線：`program-reconnect`、`source-back`、`timestamp-jump`
- 指標：`mediaClockDriftSec`、`noOutputSeconds`、`relayReconnects`

### 已 CFR 的來源不轉碼

來源名目與平均幀率一致（或平均落在標準幀率）時，program 直接 `-c copy`：
不強制 `-r/CFR`，避免把一個本來正常的來源改成「時間軸超前牆鐘」而累積漂移
（實測強制 CFR 曾讓漂移累積到 **+132 秒**，改回 copy 後穩定在 ±0.2 秒）。

### VFR 來源：預設保留節奏

`normalizeMode`（設定 → 轉播備援 → 共享節目源時間軸）：

| 模式 | 行為 | 適用 |
|---|---|---|
| `auto`（預設） | CFR 來源 copy；VFR 來源**保留原節奏**（只修時間戳） | 一般情況 |
| `passthrough` | 明確保留來源節奏 | 需要原始 20/40/60 節奏 |
| `cfr` | 強制固定幀率（插入重複幀） | 平台不接受可變幀率 |
| `off` | 純 copy | 不需要任何處理 |

`auto` 另外會在偵測到**來源時間戳本身就是壞的**（FFmpeg 回報 `Non-monotonous DTS`、
`Invalid timestamps`、`Packet corrupt` 等）時，自動把該次節目源升級為 CFR，並記錄
`normalize-escalated` 事件（只升一次；`passthrough`/`cfr`/`off` 為明確選擇，不會被覆蓋）。

CFR 是**不可逆**的：重複幀插入後，原始 VFR 時間軸無法在下游還原。需要保留原始節奏
時請用 `auto`/`passthrough`，或改用錄影（DVR 以 `-c copy` 保存，不改變時間軸）。

### 原始時間軸留存（timing capture）

每次轉播都會把**來源封包的原始 PTS/DTS** 寫成壓縮檔，即使節目源走 CFR 轉碼也保留：

```
/data/forward-timing/<platform>/<YYYYMMDD-HHMMSS>/
  ├── packets.csv.gz   # stream_index,pts_time,dts_time,duration_time,flags
  └── meta.json        # 起訖時間、行數、大小、來源 app/stream
```

- 每個平台保留**最近 5 場**（新的一場開始時自動刪除更舊的）
- 只解封裝（ffprobe），不解碼，成本約一個額外 FLV 消費者
- 用途：CFR 轉碼後仍可追查來源原本的節奏、掉幀、時間戳跳變
- 查詢：`POST /terraform/v1/ffmpeg/forward/secret {"action":"timing","platform":"<name>"}`
- 匯出：`docker cp oryx:/data/forward-timing/<platform>/<run>/packets.csv.gz .` 後
  `zcat packets.csv.gz | head`

> 這份紀錄是「來源」的時間軸，不是送出去的時間軸；`auto`/`passthrough` 下兩者相同，
> `cfr` 下可据此比對「原始節奏 vs 固定格線」的差異。

