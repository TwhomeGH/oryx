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

## 修正與驗證

移除原本以原生 HLS 能力判斷後直接把 `.flv` 交給 video 的錯誤分支。FLV／RTC 播放 Promise 錯誤會被處理，RTC 失敗會關閉連線並恢復操作按鈕。HLS 停止、切換與失敗會清除播放器及逾時計時器。

`ui/src/pages/PushDiag.test.js` 的 8 項測試涵蓋網址轉換、原生 HLS 優先、自動播放限制、HLS.js 致命錯誤清理、RTC 替代入口、FLV 播放失敗不停止分析，以及無播放能力時仍能解析跨網路區塊的原始 FLV 音訊 tag 與時間戳。在 ui 目錄執行 `npm test -- src/pages/PushDiag.test.js`。

本機瀏覽器已檢查頁籤與輸入錯誤提示；自動測試模擬原生 HLS 能力。尚未在實體 iPhone／Safari 與真實直播串流上驗證播放。
