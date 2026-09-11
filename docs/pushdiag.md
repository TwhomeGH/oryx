# 推流診斷與 HLS 相容檢查

`platform/containers/www/players/pushdiag.html` 以 FLV 原始封包診斷為主，另提供 WebRTC 分析及 HLS 畫面檢查。FLV 預覽不支援或解碼失敗時，停用的只有預覽，頁面保留「僅分析 FLV 封包」，繼續透過 Fetch 讀取原始 FLV，解析時間戳、GOP 與封包資訊。只有無法讀取 FLV 串流或 RTC 失敗時才提示 HLS 替代入口，也可手動選取 HLS 頁籤；切換 HLS 會停止 FLV 分析。

預覽庫由 mpegts.js 1.7.3 升至 1.8.2，使用 `getFeatureList().mseLivePlayback` 判斷直播能力，並設定 `disableRemotePlayback` 以滿足 Safari ManagedMediaSource 的使用條件。1.8.0 起支援 iOS 17.1+ 的 ManagedMediaSource；1.8.1 還修正較新 Safari 的能力偵測。詳見 [mpegts.js 版本說明](https://github.com/xqq/mpegts.js/releases)、[WebKit 條件說明](https://webkit.org/blog/14735/webkit-features-in-safari-17-1/)。

FLV 封包解析不依賴 MSE／ManagedMediaSource 或影片解碼器；但仍需要 Fetch 串流讀取能力、可存取的 HTTP-FLV 端點及正確的 CORS。缺少這些條件時，換成 flv.js 也不能保證解決，HLS 更不能提供原始 FLV 封包診斷。

## Safari／行動裝置

1. 選取「HLS 相容檢查」，或在錯誤提示中按「改用 HLS 檢查」。切換時會停止原本的分析及播放器。
2. 確認 HLS 網址。標準 `.flv` 路徑會換成 `.m3u8`；標準 WHEP 網址以 app／stream 推導路徑，保留其他查詢參數。不同的 HLS 主機、埠或自訂路由必須手動修正。
3. 按「開始 HLS 檢查」。優先使用瀏覽器原生 HLS，其次使用 HLS.js。預設靜音與行內播放；若自動播放被阻擋，依提示點影片的播放按鈕，可在播放器開啟聲音。

HLS 可檢查畫面、解析度及緩衝秒數，延遲通常較高，不能提供 FLV 封包／GOP 或 WebRTC RTT／丟包統計。伺服器必須啟用 HLS 且串流在線；fallback 不會自動替伺服器轉碼或開啟 HLS。

若載入失敗，確認 `.m3u8` 與分片皆可存取、認證、CORS、TLS 憑證及影音編碼。HTTPS 頁面不能使用 HTTP 串流。無原生 HLS 的環境還需要成功載入 CDN 的 HLS.js。等待超過 20 秒會停止並提示重試。

## 分層診斷（2026-09 新增）

FLV 分析頁新增「分層診斷」卡片，與「即時預覽」並排（`.diag-grid.cols-3-2`，桌機 3:2；左欄＝即時預覽＋連線統計，右欄＝分層診斷＋健康診斷），方便一邊看畫面一邊看診斷；窄版（≤1000px）自動堆疊。卡片把整條 RTMP/FLV 資料路徑拆成數層，每層以「預期 → 實際」呈現，狀態徽章（正常／注意／異常／待測）一眼看出哪一層異常：

| 層 | 預期 | 實際來源 |
|---|---|---|
| 來源端 (SRS API) | `publish active`，FLV 實測 ≈ `recv_30s` | SRS `/api/v1/streams/`：publish/clients/recv_30s/send_30s/video/audio |
| HTTP-FLV 連線 | HTTP 200 + `video/x-flv` | Fetch 狀態、Content-Type、接收緩衝 |
| FLV 容器 | 簽章 FLV、version 1、hasVideo=1 | 解析 FLV header |
| AVC 序列標頭 (SPS/PPS) | 首個 video tag 為 `avcPacketType=0`，含有效 SPS+PPS | 解析 AVCDecoderConfigurationRecord |
| 視訊時間戳 (DTS) | 單調遞增，fps ≈ VUI | 近 3 秒幀率／jitter／間距 |
| AAC 序列標頭 (ASC) | 首個 audio tag 為 `aacPacketType=0` | 解析 AudioSpecificConfig |
| 影音同步 | \|offset\| < 1000ms，無持續漂移 | `syncSeries` 樣本 |

來源端層把 SRS 的 `recv_30s`（推流端進來的碼率）與頁面 FLV 實測碼率並列；若 FLV 實測 < `recv × 0.8`，標「注意」並提示交付層可能掉包。這可用來分辨問題在 encoder／來源，還是在交付（Go proxy、consumer queue、瀏覽器）。HTTP-FLV 連線層也會顯示未消化的接收緩衝；緩衝持續變大代表解析落後，可能造成伺服器 consumer queue 溢出丟 GOP。

### AVC sequence header 異常醒目提示

當 AVC sequence header 缺失、結構錯誤或 SPS 解析失敗時，頁面頂端會出現紅色 banner，逐欄列出「預期 → 實際」：`configurationVersion`、`lengthSizeMinusOne`、`numOfSequenceParameterSets`、SPS/PPS 長度與截斷、首個 video tag 型別、IDR 是否早於 sequence header。若異常已恢復（例如後續收到有效 header），banner 改為藍色資訊提示並保留異常次數。

### fps 量測

- 只有實際影格（`avcPacketType=1`）計入 `videoTsHistory`，AVC sequence header 不再被當成一幀（先前會灌水）。
- 「分層診斷」與健康診斷使用**近 3 秒**視窗（`recentFpsStats`，回傳 fps／jitter／間距）；另有 `measureFps` 為整個 600 幀窗的平均，兩者並列可看出來源是否在變動。判斷「波動」時以 SPS 的 VUI fps 為預期值對比。

## 串流來源選單（2026-09 新增）

「連線設定」最上方有兩個下拉：

- **分析來源**：`跟隨來源（釘選目前這一路）`／每個活躍流 `app/stream`／`手動輸入`。
- **預覽來源**：`跟隨分析串流`／另一路活躍流；分析進行中切換會即時重啟預覽。

資料來自 SRS `/api/v1/streams/`（與 console 相同來源），每 5 秒、且只在頁面可見時更新。認證沿用同源 `localStorage.SRS_TERRAFORM_TOKEN` 的 bearer；未登入或 API 失敗時退回手動輸入，並在提示列說明。「跟隨」採**釘選**語意：選定後固定跟著該 `app/stream`，即使它暫時離線也不自動跳走。

FLV URL 由 `host + '/' + app + '/' + stream + '.flv'` 組成（不再硬編 `live`，新增 `app` 輸入）。頁面預設主機為**目前開啟頁面的 origin**（`window.location.origin`），避免遠端開啟時誤連 `localhost` 而誤判「不支援 HTTP-FLV」；只有以 `file://` 直接開啟時才回退 `http://localhost:882`。

## WebRTC 掉幀量測（2026-09 修正）

WebRTC 分析的「掉幀率」來自 `inbound-rtp` 的 `framesDecoded` / `framesDropped`。`framesDropped` 是**解碼前**被丟棄的幀（不完整／太晚，通常源於封包遺失或延遲），不是 CPU 解不動。

先前的算法用**每次 getStats 間隔（250ms）的差值**當「近期掉幀」，樣本太小：30fps 下一個窗只有約 7 幀，掉 1 幀就是 14%，容易誤報（曾出現 60%）。現改為：

- **近期**：以 3 秒滾動窗計算（`computeFrameDropRates`，取「至少 3 秒前的最新樣本」為基準），窗未滿 1.5 秒前不顯示，避免早期小樣本。
- **累積**：自開台至今的 `framesDropped / (framesDecoded + framesDropped)`，作為對照。

表格欄位改為「掉幀率(近3s/累積)」，健康旗標也同時顯示近期與累積。另修掉 `v` 缺失時把基準歸零、導致拿累積值當近期值的 bug（改為只在有 `v` 時取樣）。

## 修正與驗證

移除原本以原生 HLS 能力判斷後直接把 `.flv` 交給 video 的錯誤分支。FLV／RTC 播放 Promise 錯誤會被處理，RTC 失敗會關閉連線並恢復操作按鈕。HLS 停止、切換與失敗會清除播放器及逾時計時器。

`ui/src/pages/PushDiag.test.js` 的 10 項測試涵蓋網址轉換、原生 HLS 優先、自動播放限制、HLS.js 致命錯誤清理、RTC 替代入口、FLV 播放失敗不停止分析、無播放能力時仍能解析跨網路區塊的原始 FLV 音訊 tag 與時間戳、來源選單會從 SRS stream API 填入並同步 app/stream，以及 WebRTC 掉幀率以滾動窗平滑（`computeFrameDropRates`）。在 ui 目錄執行 `npm test -- src/pages/PushDiag.test.js`。

本機瀏覽器已檢查頁籤與輸入錯誤提示；自動測試模擬原生 HLS 能力。尚未在實體 iPhone／Safari 與真實直播串流上驗證播放。
