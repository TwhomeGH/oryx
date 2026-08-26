# API 認證架構與 HTTP Status Code 說明

> 本文說明 `platform/service.go` 中的 API 認證 middleware 運作方式、
> HTTP status code 對應邏輯，以及常見的認證流程。目的是讓之後維護時
> 不用在專案裡翻來翻去找「某個 500/401 到底是誰回的」。

## 概覽

```
HTTP Request
  │
  ▼
┌─────────────────────────────────┐
│  middlewareAuthTokenInBody()    │  從 request body 讀 token
│  或 middlewareAuthTokenInURL()  │  從 query string 讀 token
│                                 │
│  Authenticate(apiSecret, token) │  比對 Bearer token
│         │                       │
│    ┌────┴────┐                  │
│  成功       失敗                │
│    │    ┌────▼──────────┐       │
│    │    │ WriteError    │       │  → 401 Unauthorized
│    │    │ (httpError)   │       │
│    │    │ return        │       │  不執行 next handler
│    │    └───────────────┘       │
│    ▼                            │
│  next.ServeHTTP()               │  繼續執行真正的 handler
└─────────────────────────────────┘
```

## 關鍵檔案

| 檔案 | 職責 |
|---|---|
| `platform/service.go:221-278` | 兩個 auth middleware 的實作 |
| `platform/service.go:218-228` | `httpError` 型別（自訂 HTTP status code） |
| `platform/service.go:555-630` | `/terraform/v1/mgmt/init` handler |
| `platform/service.go:632+` | `/terraform/v1/mgmt/check`、`login` 等 handler |
| `platform/utils.go:315` | `SRS_PLATFORM_SECRET` 常數定義 |
| `platform/utils.go:367` | `envApiSecret()` — 從環境變數讀 API secret |
| `platform/main.go:340-365` | 平台啟動時自動生成 `SRS_PLATFORM_SECRET` 並寫入 Redis |

## Auth Middleware

### middlewareAuthTokenInBody（`service.go:256`）

**使用方式**：包在需要認證的 handler 外面。

```go
handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(...) {
    // 這裡的 handler 已經通過認證
})))
```

**運作流程**：

1. 讀取 request body → 暫存到 `bodyReader`
2. 從 body 解析 `token` 欄位（JSON）
3. 呼叫 `Authenticate(apiSecret, token, r.Header)` 比對
4. **成功**：執行 `next.ServeHTTP(w, r)`
5. **失敗**：回應 `401 Unauthorized`，**不執行** next handler

**使用的端點**（全部在 `service.go` 中）：

| 端點 | 行號 | 說明 |
|---|---|---|
| `/terraform/v1/mgmt/check` | 632 | 檢查系統狀態 |
| `/terraform/v1/mgmt/login` | 797 | 登入 |
| `/terraform/v1/mgmt/status` | 855 | 系統狀態查詢 |
| `/terraform/v1/mgmt/bilibili` | 892 | Bilibili 配置 |
| `/terraform/v1/mgmt/envs` | 800 | 環境變數查詢 |
| `/terraform/v1/mgmt/token` | 785 | Token 管理 |
| `/terraform/v1/hooks/srs/*` | srs-hooks.go | SRS 回調 |
| `/terraform/v1/ffmpeg/camera/*` | camera-live-stream.go | IP 攝影機 |
| `/terraform/v1/ffmpeg/transcode/*` | transcode.go | 轉碼 |
| `/terraform/v1/ffmpeg/forward/*` | forward.go | 轉推 |
| `/terraform/v1/ai/*` | transcript.go, ocr.go, dubbing.go | AI 服務 |

### middlewareAuthTokenInURL（`service.go:230`）

**運作流程**：

1. 從 `r.URL.Query().Get("token")` 讀 token
2. 將 token 設到 `Authorization: Bearer <token>` header
3. 呼叫 `Authenticate(apiSecret, "", r.Header)` 比對
4. 成功/失敗行為同上

**使用的端點**：帶有 `?token=` 查詢參數的 URL（主要是 DJI Callback）。

## httpError 與 Status Code 對應

### httpError 型別（`service.go:218`）

```go
type httpError struct {
    err    error
    status int
}

func (e *httpError) Error() string { return e.err.Error() }
func (e *httpError) Status() int   { return e.status }
```

實作了 `ohttp.HTTPStatus` interface，讓 `ohttp.Error()` 使用自訂 status code。

### Status Code 決策邏輯

`ohttp.WriteError()` → `ohttp.Error()` 的判斷鏈（`go-oryx-lib/http/http.go:92-126`）：

```
error
  ├── SystemComplexError  → 走 FilterCplxSystemError（帶 code + message）
  ├── SystemError (int)   → 走 FilterSystemError（帶 code）
  ├── AppError            → 走 FilterAppError（帶 code + data）
  ├── HTTPStatus          → 用 error 自己的 Status()
  └── 其他                → http.StatusInternalServerError (500)
```

**重點**：如果 error 沒實作 `HTTPStatus`，一律回 500。這就是之前 auth 失敗
回 500 的原因——`errors.Wrapf(err, "authenticate")` 產生的是普通 error。

### 修改後的行為

修改後，auth 失敗使用 `&httpError{err, status: http.StatusUnauthorized}`：

```go
ohttp.WriteError(ctx, w, r, &httpError{err: err, status: http.StatusUnauthorized})
```

| 情境 | Status Code | 說明 |
|---|---|---|
| Auth 失敗（token 不對/為空） | **401** Unauthorized | `middlewareAuthTokenInBody` / `InURL` |
| API 內部錯誤 | **500** Internal Server Error | handler 自身的 `ohttp.WriteError` |
| 請求格式錯誤 | **500** → 應改為 400 | handler 自身的 error（未實作 HTTPStatus） |
| 查詢 init 狀態 | **200** | `/terraform/v1/mgmt/init`（NoAuth） |

## 認證流程

### 首次初始化

```
1. 平台啟動
   └─ main.go:340-365 — 從 Redis 讀 SRS_PLATFORM_SECRET
      ├─ 有值 → 使用既有 secret
      └─ 無值 → 隨機生成 32B hex → 寫入 Redis → os.Setenv

2. 測試呼叫 /terraform/v1/mgmt/init（NoAuth）
   └─ service.go:555-630
      ├─ password 為空 → 回傳 {init: true/false}（查詢用）
      ├─ 已初始化 → 回傳 "already initialized" 錯誤
      └─ 未初始化 → 寫 .env → 回傳 {token, bearer}
         └─ bearer = envApiSecret() = 平台的 SRS_PLATFORM_SECRET

3. 測試用 bearer 更新 *apiSecret
   └─ 後續 API 呼叫帶上正確的 Bearer token
```

### 正常運作（已初始化）

```
1. 客戶端帶 Authorization: Bearer <SRS_PLATFORM_SECRET>
2. middlewareAuthTokenInBody 讀 body 中的 token，或用 header 的 Bearer
3. Authenticate() 比對 apiSecret
4. 通過 → 執行 handler
5. 不通過 → 回 401
```

## Authenticate 函數

`Authenticate()` 定義在 `go-oryx-lib` 中，比對邏輯：

- 從 `Authorization: Bearer <token>` header 取 token
- 或從 body 的 `token` 欄位取
- 與 `apiSecret` 比對（字串相等）
- 匹配 → nil（成功）
- 不匹配 → error（被 middleware 包成 401）

## 安全設計要點

1. **init/login 端點不包 auth middleware** — 用裸 `HandleFunc` 註冊，
   首次設定密碼不需要先有 secret
2. **init 端點不可重設** — `envMgmtPassword() != ""` 時直接回錯誤，
   防止密碼被覆蓋
3. **auth 失敗不執行 handler** — middleware 在寫完 401 後 `return`，
   不會繼續執行 `next.ServeHTTP`（之前有 bug，已修復）
4. **SRS_PLATFORM_SECRET 自動生成** — 平台啟動時從 Redis 讀或隨機生成，
   不需要人工設定
