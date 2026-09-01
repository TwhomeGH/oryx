# B 站教學影片資訊查詢：快取與風控應對

`/terraform/v1/mgmt/bilibili` 用於查詢單一 B 站影片 metadata（`api.bilibili.com/x/web-interface/view`），
`/terraform/v1/mgmt/tutorials` 提供教程清單並在伺服器端解析 metadata。B 站對非瀏覽器請求有風控
（HTTP 412），本文說明會觸發風控的頻率問題、服務端多層應對，以及教程清單的 manifest 設計。

## 頻率問題（2026-09 已根治）

早期前端 `TutorialsButton.js` 對**每個 bvid 並行各發一個 axios POST**，頁面一載入就是
N 個不同 bvid 的並行請求——每個 bvid 快取各自獨立，singleflight 只合併「同 bvid」，對 burst 無效。

2026-09 改成 **伺服器端解析**：前端只 fetch `/terraform/v1/mgmt/tutorials` **一次**（拿到整包
manifest），B 站 metadata 由平台逐條解析（走下面同一套快取/串行化）。前端不再逐 bvid 打，
「一次 N 發」的 burst 從根上消失。

## 風控 412 的成因

B 站風控（`Precondition Failed`）主要看三個訊號：

1. **資料中心 IP**（最致命）——CI（GitHub Actions）或雲端 VPS 都是資料中心網段，風控直接標記。
2. **缺少瀏覽器指紋/cookie**——真瀏覽器執行 B 站 JS 後才有 `buvid3` 指紋；Go HTTP client 不跑 JS，
   永遠沒有指紋。
3. **請求頻率**——無指紋還頻繁/並行打，更容易被 412。

家用 IP（compose 跑在本機）時，第 3 點是主因。

## 服務端多層應對

- **每 bvid 快取**：Redis hash `SRS_CACHE_BILIBILI`，value 含 `{update, res, err}`。
  成功 24h TTL（dev 300s）、失敗（含 412/429）5 分鐘退避 TTL。refetch 判據是「無快取或已過期」，
  不是 `res==nil`——失敗結果也照樣快取，不再每次查詢都重打 B 站。
- **singleflight（同 bvid 合併）**：`bilibiliSingleFlight`，同一 bvid 的併發只打一次。
- **limiter（跨 bvid 串行化）**：`bilibiliLimiter` 是一個容量 1 的 channel semaphore，包住對外的
  `client.Do(req)`，一次只打一發。
- **瀏覽器樣 header**：`User-Agent` + `Referer` + `Accept` + `Accept-Language`。
- **錯誤帶 body**：非 2xx 時把 response body（截斷 500 字元）併入錯誤訊息，例如
  `bilibili response status 412, body=<HTML/JSON 內容>, url=...`，方便確認風控實際回了什麼。

## 教程清單 manifest

教程清單不在前端寫死，而是伺服器端的資料檔：

- **預設檔** ship 在 image 的 `platform/containers/conf/tutorials.json`（跟著 image 走）。
- **第一次啟動**時平台把它 seeding 到 `/data/tutorials.json`（使用者可編輯的副本，compose 掛載的
  持久卷）。升級 image 不會覆蓋已存在的 `/data/tutorials.json`。
- **加/改教程** = 編輯 `/data/tutorials.json`，刷新頁面即生效，**免 rebuild 免重啟**。

每個 context（`live` / `ssl` / `recordVod` / `recordCos` / `srt` / `all`）是一組條目，每個條目自描述：

```json
{"id": "BV1RS4y1G7tb", "source": "bilibili", "author": "徐光磊", "langs": ["zh"]}
{"id": "e9fe6f314ac6", "source": "medium", "author": "Winlin Yang",
 "title": "How to Setup a Video Streaming Service by 1-Click",
 "link": "https://blog.ossrs.io/how-to-setup-a-video-streaming-service-by-1-click-e9fe6f314ac6",
 "langs": ["en"]}
```

- `source`：`bilibili`（live metadata，伺服器解析）或靜態來源（`medium` / `youtube`，直接帶 title/link）。
- `langs`：可選，控制顯示的語系（`["zh"]` / `["en"]`）；缺省表示所有語系。
- 伺服器端解析 bilibili 條目（title/desc/view/like/share + 影片連結），B 站失敗時 fallback 到
  manifest 內嵌的靜態 title/link（若有的話），卡片不會因 B 站掛掉而消失。
- 前端 `useTutorials('live')` 依 context key 讀取並依語系過濾，一次 fetch 快取在模組內。

## 測試行為

- `TestApi_TutorialsQueryBilibili`：平台錯誤含 `bilibili ` 前綴（B 站側問題，含風控 412/429）時 **skip**，
  不讓 CI 因 B 站不可用而失敗；平台自身 bug（非 bilibili 錯誤）仍會 FAIL。
- `TestApi_TutorialsQuery`：驗證 `/terraform/v1/mgmt/tutorials` 一定回傳所有 context（`live`/`ssl`/
  `recordVod`/`recordCos`/`srt`/`all`）且每個條目有 id/source——manifest 不受 B 站可用性影響。
- `test/main_test.go` 的 `Request` 在非 2xx 時會把 response body 併入錯誤，skip 判斷以實際錯誤內容為準。
