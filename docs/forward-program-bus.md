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
- slate 固定 1280x720；來源解析度不同時切換瞬間下游可能觸發解碼器重建。
- 自動來源（`config.Stream` 空）目前「首次綁定後固定」，尚未跟隨最新來源。

## 7. 恢復：為何要重啟 fanout（實測修正）

隔離實驗發現：**FFmpeg 的 HTTP-FLV `-reconnect` 在串流被取消發布再重新發布後，不會恢復**
——consumer 會卡在失敗的重連迴圈（`Will reconnect … error=Input/output error`），即使直連
SRS:8080 也一樣。因此「靠 relay 的 HTTP-FLV 重連撐住 program」無法撐過恢復階段，fanout 也
不會自動讀到換 publisher 後的新內容（曾造成恢復後永久凍結）。

修正：
- `runHold`：來源恢復（sourceActive false→true）時**重啟 relay**，讓 program 乾淨地重新發布。
- `doForward`：偵測到 **program 換 publisher**（`programSwitches` 變動）時**重啟 fanout**，
  以全新連線讀到恢復後的 program。

代價：恢復時下游會**重連一次**；中斷期間下游仍**不斷線**（凍結最後一幀）。
整合測試 `TestScenario_WithStream_PublishRtmpForwardKeepalive` 已加入「輸出必須恢復」斷言。
