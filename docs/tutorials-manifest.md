# 教程清單 manifest：位置與編輯指南

Dashboard / 教學卡片的教程清單**不是寫死在前端**，而是伺服器端的一份 JSON manifest。
自架者可以直接編輯它來**新增/調整教程，免 rebuild、免重啟**。

## 檔案在哪

| 角色 | 路徑 |
|---|---|
| 使用者可編輯（權威） | `/data/tutorials.json`（容器內）；在本機 compose 就是 `ORXY` 掛載目錄下的 `tutorials.json` |
| 預設檔（跟著 image） | `/usr/local/oryx/platform/containers/conf/tutorials.json` |

- **第一次啟動**時，平台把預設檔 seeding 到 `/data/tutorials.json`，之後一律以 `/data` 那份為準。
- 升級 image **不會**覆蓋你已存在的 `/data/tutorials.json`（一旦你開始編輯，你的副本就是權威）。
- 找不到 `/data/tutorials.json` 時回退到 image 預設，接口不會因此壞掉。

## 怎麼編輯

直接編輯 `/data/tutorials.json`（用你的 ORXY 目錄對應檔案），然後 **F5 刷新瀏覽器**即可生效。
JSON 格式：一個 map，key 是 context（`live` / `ssl` / `recordVod` / `recordCos` / `srt` / `all`），
value 是條目陣列。

### 加一個 B 站影片（自動抓 metadata）

```json
{"id": "BV1xxxxxxxxxx", "source": "bilibili", "author": "作者名", "langs": ["zh"]}
```

- 伺服器端會自動查 B 站 API 補上標題/簡介/觀看數（有快取，一天最多打一次）。
- 想要 B 站掛掉時卡片仍顯示標題，可以**內嵌靜態 fallback**：

```json
{"id": "BV1xxxxxxxxxx", "source": "bilibili", "author": "作者名", "langs": ["zh"],
 "title": "影片標題（fallback）", "link": "https://www.bilibili.com/video/BV1xxxxxxxxxx"}
```

### 加一個靜態教程（YouTube / Medium / 任何網站，不打 API）

```json
{"id": "自訂id", "source": "youtube", "author": "作者", "langs": ["en"],
 "title": "標題", "link": "https://youtu.be/XXXX"}
```

`source` 可寫 `medium` / `youtube` 或任何你喜歡的名稱；伺服器不打 API，直接顯示內嵌資料，
並依 link 判斷卡片上的 media 標籤（`youtu.be` → YouTube，其餘 → Medium）。

### 語系控制（`langs`）

- `"langs": ["zh"]`：只在中文介面顯示。
- `"langs": ["en"]`：只在英文（及非中文語系）顯示。
- 省略 `langs`：所有語系都顯示。

### 調整顯示順序

伺服器端會把有觀看數的條目（B 站）依觀看數由高到低排序；靜態條目（無觀看數）維持你在
JSON 裡寫的順序。

## 擴展到新的平台來源

- **靜態來源**：如上，直接加條目即可，不用寫任何程式碼。
- **新的「會查 API」的來源**（例如以後接 YouTube API）：manifest 加 `"source": "youtube"` 條目，
  並在平台 `service.go` 的 `handleMgmtTutorials` 加一個解析分支（仿照 bilibili 的
  `queryBilibili`，含自己的快取/限流 helper）。未知的 source 會自動降級為靜態卡片，不會報錯。

## 常見問題

- **JSON 格式錯誤**：接口回 500 且錯誤訊息會指出哪個檔、哪一行格式有問題。改好再刷新即可。
- **B 站風控（412）**：B 站 metadata 查不到時，若條目有內嵌 fallback 的 title/link 就照樣顯示，
  沒有就只少標題（卡片仍在）。
- **改動沒生效**：確認你改的是 `/data/tutorials.json`（不是 image 內的預設檔），並**完整刷新**
  瀏覽器（SPA 首次載入後會把 manifest 快取在記憶體，F5 才會重新抓）。
