# 串流查詢 API：單流精確查詢

> 對應上游 PR [#210](https://github.com/ossrs/oryx/pull/210)（本 fork 已套用）。

## 功能說明

`POST /terraform/v1/mgmt/streams/query` 原本只會回傳**全部**活躍串流清單。
套用 #210 後支援三個可選參數 `vhost`、`app`、`stream`——三個都帶時，
改用 Redis 精確鍵查詢（HGet），只回傳那一路串流：

| 呼叫方式 | 行為 | 底層操作 |
|---|---|---|
| 只帶 token | 回傳全部活躍串流（原有行為，不變） | `HGETALL` |
| 帶齊 vhost + app + stream | 只回傳該路串流；不存在回空陣列 | `HGET` |

## 請求格式

```json
{
  "token": "<認證令牌>",
  "vhost": "__defaultVhost__",
  "app": "live",
  "stream": "my-stream-01"
}
```

---

## 「token」是哪一組？（兩種都可以）

| 來源 | 取得方式 | 特點 |
|---|---|---|
| **① 登入 session token** | 用管理密碼呼叫登入 API 換取（見下方範例） | 就是 WebUI 登入後瀏覽器自動使用的那組 JWT；**有過期時間** |
| **② API 密鑰** | WebUI「系統設定 → OpenAPI」區塊顯示並可一鍵複製 | **永不過期**，推薦給腳本/監控長期使用 |

> 對應關係：① 放 body 的 `"token"` 欄位；② 不放 body，改放 HTTP Header
> `Authorization: Bearer <API密鑰>`。兩者擇一即可。

### 方式 ②：API 密鑰（推薦）

```bash
curl -s -X POST http://localhost:2022/terraform/v1/mgmt/streams/query \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <你的API密鑰>' \
  -d '{"vhost":"__defaultVhost__","app":"live","stream":"my-stream-01"}'
```

### 方式 ①：先登入換 token 再查詢

```bash
# 1. 用管理密碼換 session token
TOKEN=$(curl -s -X POST http://localhost:2022/terraform/v1/mgmt/login \
  -H 'Content-Type: application/json' \
  -d '{"password":"<你的管理密碼>"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# 2. 帶著 token 查詢
curl -s -X POST http://localhost:2022/terraform/v1/mgmt/streams/query \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"$TOKEN\",\"vhost\":\"__defaultVhost__\",\"app\":\"live\",\"stream\":\"my-stream-01\"}"
```

## 使用範例

### 查全部串流（向下相容）

```bash
curl -s -X POST http://localhost:2022/terraform/v1/mgmt/streams/query \
  -H 'Content-Type: application/json' \
  -d '{"token":"<token>"}'
```

### 精確查詢某一路串流

```bash
curl -s -X POST http://localhost:2022/terraform/v1/mgmt/streams/query \
  -H 'Content-Type: application/json' \
  -d '{
    "token": "<token>",
    "vhost": "__defaultVhost__",
    "app": "live",
    "stream": "my-stream-01"
  }'
```

回應：

```json
{
  "code": 0,
  "data": {
    "streams": [
      {
        "vhost": "__defaultVhost__",
        "app": "live",
        "stream": "my-stream-01",
        "url": "rtmp://127.0.0.1/live/my-stream-01",
        "client": "<client-id>",
        "update": "2026-08-25T19:30:08Z"
      }
    ]
  }
}
```

串流不存在時 `data.streams` 為空陣列 `[]`（HTTP 仍是 200）。

---

## 參數值從哪裡來？

不確定 vhost/app/stream 的值？先呼叫一次「查全部」，從回應的每筆串流物件中
直接抄這三個欄位。Oryx 預設推流的 vhost 是 `__defaultVhost__`，app 通常是
`live`（可在推流設定中自訂）。

## 典型使用場景

1. **監控輪詢**：外部監控腳本每 5 秒確認某一路關鍵串流是否在線，
   不必每次拉全表再過濾
2. **轉推前檢查**：設定 forward/camera 任務前，先精確確認來源串流存在
3. **前端單流狀態卡**：UI 只需顯示某一串流狀態時減少資料量

> 注意：三個參數是「全帶才生效」的設計——只帶其中一兩個會退回查全部的行為。

---

## 相關端點對照

| 端點 | 用途 |
|---|---|
| `/terraform/v1/mgmt/streams/query` | 查詢活躍串流（本文） |
| `/terraform/v1/mgmt/streams/fps` | 用 ffprobe 探測單路推流的實測 FPS、jitter 與波動標記；前端會基於每路 stream 自己的滾動基準顯示，並推算預期幀間隔區間作參考 |
| `/terraform/v1/mgmt/streams/kickoff` | 強制斷開某路串連（本就支援 vhost/app/stream 定位） |
