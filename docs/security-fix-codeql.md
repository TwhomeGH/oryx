# CodeQL 安全漏洞修復說明

本文記錄本 fork 自 2026-08 起的 CodeQL 安全修復。各批次的程式修正、測試與重掃狀態分別列出；完成程式修正不代表 CodeQL 警告已經關閉。

## 2026-09-11：移除 `statusRecorder.Write` 的反射型 XSS sink（設計根治）

前幾輪（下方 2026-09-10 兩節）修的是**內容產生處**：共用 `jsonHandler()` 不再拼接 JSONP `callback`，一律 `json.Marshal` 並加上 `application/json; charset=utf-8` 與 `nosniff`。但 `go/reflected-xss` 仍標在 `platform/console-metrics.go` 的輸出點：

```go
func (v *statusRecorder) Write(b []byte) (int, error) {
	if v.status == 0 {
		v.status = http.StatusOK
	}
	return v.ResponseWriter.Write(b)
}
```

原因是 CodeQL 的 taint 追蹤把 `r`（request，tainted）→ handler 產生的回應位元組 → `statusRecorder.Write(b)` → 底層 `ResponseWriter.Write` 當成一條 sink 路徑；它不把 `json.Marshal` 視為 XSS 淨化，因此只修內容產生處無法讓這個 sink 消失。

**設計根治：** `statusRecorder` 只需要記錄狀態碼，而 `Write()` override 本身是多餘的——`Wrap()` 已經把仍是 0 的狀態視為 200（裸 `Write` 的隱式狀態）：

```go
status := rec.status
if status == 0 {
	status = http.StatusOK
}
```

因此移除 `statusRecorder.Write`，讓嵌入的 `http.ResponseWriter` 直接提供 `Write`。行為不變（顯式 `WriteHeader` 仍被記錄、隱式 200 仍被 `Wrap` 補上），但被標記的 sink 在專案自有程式中整個消失。這與第七批 WHIP/WHEP 的作法一致（移除自訂 `ResponseWriter`，而非在 sink 上做轉義）。

**驗證：** `GOOS=linux GOARCH=amd64 go build -mod=vendor ./...` 通過；`golang:1.26` 容器內 `go test -mod=vendor -run 'TestConsoleMetrics|TestNormalizeConsoleRoute|TestBuildRedisInfoSnapshot|TestUtils_ComputeStreamFPS' -count=1 .` 通過。專案自有程式已無任何自訂 `ResponseWriter.Write`（`grep` 僅剩 vendored 依賴）。CodeQL 尚未重掃，不能宣稱警告已關閉；需對包含此提交的版本重跑掃描確認。

同一次變更也補上串流正確性：`statusRecorder` 實作 `Unwrap() http.ResponseWriter` 與 `Flush()`，讓 `http.ResponseController` 能穿透 wrapper 找到底層的 `http.Flusher`。否則被 metrics middleware 包住的 `/live/*.flv` 反向代理無法 flush，長連線 FLV 會被緩衝而非即時串流。新增 `TestConsoleMetricsFlushPassthrough` 覆蓋（`httptest.ResponseRecorder` 需收到 `Flushed`）。

## 2026-09-10 後續：移除 JSONP，固定回傳 JSON

此變更取代下方歷史紀錄中的 callback 白名單方案。共用 `jsonHandler()` 不再讀取或拼接 `callback`，無論參數內容為何，都以 `json.Marshal` 產生 JSON，設定 `application/json; charset=utf-8` 與 `X-Content-Type-Options: nosniff`。一般 JSON 的資料結構、應用錯誤碼及 HTTPStatus 處理維持原有行為；非 jsonHandler 的原始文字錯誤、代理與媒體回應不在本次變更範圍。

專案內搜尋未找到 JSONP 呼叫端；外部若仍使用 JSONP，需改用 fetch/HTTP JSON，跨域時由服務端配置 CORS。正常及惡意 callback 現在都會被忽略，不再回傳 JSONP 或 callback 驗證的 400。

`statusRecorder.Write(b)` 保留原樣，避免破壞代理、HTML 與媒體回應。測試涵蓋經過此 wrapper 的惡意 callback、一般 callback、JSON 資料還原與應用錯誤回應。修正仍位於 vendored 函式庫，更新 vendor 時須保留此行為。Linux Go 1.26 容器已通過 `go test -mod=vendor . -run 'TestConsoleMetrics|TestNormalizeConsoleRoute|TestBuildRedisInfoSnapshot' -count=1`。CodeQL 尚未重掃，不能宣稱該警告已關閉；後端需重新建置部署。

### 呼叫端遷移與部署

舊式 `<script src="/endpoint?callback=app.done">` 不再受支援；請改用 HTTP 客戶端讀取 JSON，例如同源瀏覽器呼叫：

```js
const response = await fetch('/terraform/v1/mgmt/http/metrics', {
  headers: {Authorization: `Bearer ${token}`},
});
if (!response.ok) throw new Error(`HTTP ${response.status}`);
const result = await response.json();
if (result.code !== 0) throw new Error(`API error ${result.code}`);
```

`token` 應使用既有登入流程取得的憑證。移除 callback 不會取消端點認證；跨域呼叫仍須遵守服務端的 CORS 與認證設定。本次僅修改共用 `jsonHandler()`，不代表上游 SRS 或其他獨立服務的 JSONP 也已移除。

部署時使用 `-mod=vendor` 重新建置平台後端映像並更新容器；只重建前端不會套用此修正。部署後確認回應為 JSON、帶有 `nosniff`，且加上 `callback=app.done` 不會產生 JavaScript 包裝。接著對包含本次修正的提交重跑 CodeQL；若警告仍在，須依完整來源到輸出點的路徑繼續追查。

### 已完成的驗證

- `TestConsoleMetricsJSONPIgnored`：惡意 callback 不會回顯或產生 JSONP。
- `TestConsoleMetricsJSONOnly`：空值與正常 callback 均回傳可解析的 JSON，資料內容與安全標頭正確。
- `TestConsoleMetricsJSONError`：使用者提供的錯誤訊息經 JSON 編碼後保留原始語意，應用錯誤碼維持正確。
- 上述案例及路由正規化、metrics、Redis snapshot 相關測試已在 Linux Go 1.26 通過；這是指定範圍的回歸測試，不是完整平台測試或 CodeQL 重掃。

## 2026-09-10：共用 HTTP 函式庫的 JSONP callback 注入（前次修補紀錄）

以下記錄提交 `b02314a` 當時的白名單方案與驗證狀態，已由上方 JSON-only 方案取代；合法 callback 保留 JSONP、非法 callback 回傳 400 等描述不再是目前行為。

### 問題如何造成

`platform/vendor/github.com/ossrs/go-oryx-lib/http/http.go` 的 `jsonHandler()` 原本直接讀取 `r.URL.Query().Get("callback")`，再用以下方式產生 `application/javascript` 回應：

```go
fmt.Fprintf(w, "%s(%s)", cb, string(b))
```

JSONP 原意是讓呼叫端指定接收 JSON 的函式名稱，例如 `callback=app.done` 會產生 `app.done({...})`。但原本沒有檢查 `cb` 是否只是名稱；若輸入 `alert(1)//`，回應就變成 `alert(1)//({...})`。當回應被當作 JavaScript 載入執行時，前面的程式碼會執行，後面的 JSON 則被註解掉。

JSON 本體雖然經過 `json.Marshal`，callback 卻是在序列化之後另外拼接，因此 JSON 的跳脫保護無法涵蓋 callback。實際利用仍取決於端點可達性、認證與回應如何被載入；這個問題本身不代表能繞過 API 認證。

### 為何 CodeQL 標在 `console-metrics.go:74`

使用者提供的 `go/reflected-xss` 警告指向提交 `26110db` 的 `statusRecorder.Write()`：

```go
return v.ResponseWriter.Write(b)
```

這裡是回應的輸出點。監控 middleware 包住 handler 後，回應內容會經過這個方法送出；它記錄 HTTP 狀態碼並轉交原始位元組，不會自行產生 callback。

本次程式檢查找到的可注入路徑是：

```text
URL 的 callback 參數
  → 共用 jsonHandler 未驗證就拼接 JavaScript
  → statusRecorder.Write 接收回應位元組
  → 底層 ResponseWriter 寫出回應
```

因此修正在內容產生處，而不是把 `statusRecorder.Write()` 的所有回應做 HTML 轉義。後者也會改動正常 HTML、JSON 與 FLV 等媒體資料。這是程式碼追查確認的漏洞路徑；尚未取得完整 CodeQL 路徑報告及重掃結果，不能據此宣稱所有流向該行的警告都已排除。

### 本次修正與相容性

- 在共用 `http.go` 加入完整字串白名單：`\A[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*\z`。允許 ASCII 識別名稱與點分隔名稱，如 `callback`、`app.handlers.done`、`_$cb123`；不接受函式呼叫、括號、分號、換行、HTML 或其他運算式。
- 不合法的非空 callback 回傳 **HTTP 400**，內容固定為 `invalid JSONP callback`，不回顯使用者輸入。`http.Error` 同時設定 `text/plain; charset=utf-8` 與 `X-Content-Type-Options: nosniff`。
- 合法 JSONP 保留原有呼叫格式，並加入 `X-Content-Type-Options: nosniff`。這是額外的 MIME 防護，真正阻擋注入的是 callback 白名單。
- 未提供 callback 或 callback 為空時，維持一般 JSON 回應。原本使用括號存取或其他運算式作為 callback 的客戶端，需改用允許的名稱。
- `console-metrics.go` 的輸出邏輯沒有修改；`console-metrics_test.go` 新增測試，透過真實共用 JSON writer 與 metrics wrapper 驗證攻擊被阻擋、正常 JSON／JSONP 保持相容。

### 驗證與維護

2026-09-10 驗證狀態：

- 隔離執行 `console-metrics.go` 與 `console-metrics_test.go` 的測試已通過，包括新增的 `TestConsoleMetricsJSONPInjection`、`TestConsoleMetricsJSONPCompatibility`。隔離時使用暫存宣告替代未使用的 Redis 全域與認證入口；這不驗證 Redis 連線或真實認證流程。
- 完整 platform 測試在 Windows 受既有 `syscall.Kill` 未定義錯誤阻擋，尚未完成。可在 Linux 環境的 `platform/` 目錄執行 `go test -mod=vendor . -run 'TestConsoleMetrics|TestNormalizeConsoleRoute|TestBuildRedisInfoSnapshot' -count=1` 進行相關回歸驗證。
- CodeQL 尚未重跑，尚未確認原警告關閉；修正尚未部署。

此修正位於版本控制內的 **vendored `go-oryx-lib` v0.0.9**，目前 Makefile 使用 `-mod=vendor` 建置。重新執行 `go mod vendor` 或升級依賴時，必須確認上游已包含同等修正，否則需保留本地補丁並重跑回歸測試。使用 `-mod=mod` 不會使用這份 vendored 補丁。部署需重新建置並更新平台後端映像；調整 SRS 設定或只更新前端不會套用此修正。

## 第一批：Go 後端路徑穿越與 XSS（31 個 High）

### 1. 路徑穿越（Path Traversal）— 29 個

**問題：** HTTP handler 從 `r.URL.Path` 提取檔名或 UUID 後，直接用於構建檔案路徑（`path.Join` + `os.Open`），未驗證是否含 `..` 等路徑穿越字元。攻擊者可透過 crafted URL 讀取任意檔案。

**修復方式：**

| 檔案 | 修復手法 |
|---|---|
| `ai-talk.go` | `http.ServeFile` 前驗證 filename 不含 `..`、`/`、`\` |
| `dvr-local-disk.go` | uuid 用 regex `^[0-9a-f-]+$` 驗證；dir 不含 `..`、`/`、`\`；m3u8 用 regex `^[0-9a-zA-Z._-]+$` 驗證 |
| `transcript.go` | 所有 handler 的 uuid 加 regex `^[0-9a-f-]+$` 驗證 |
| `ocr.go` | uuid 加 regex `^[0-9a-f-]+$` 驗證 |
| `virtual-live-stream.go` | upload handler 的 filename 驗證不含 `..`、`/`、`\` |

### 2. 反射型 XSS（Reflected XSS）— 2 個

**問題：** `whxpResponseModifier.Write()` 中，環境變數 `RTC_PORT` 的值（port）透過 `fmt.Sprintf` 嵌入 SDP 回應內容（`whxpResponseModifier` 是 WHIP/WHEP 的 response modifier）。

**修復方式（兩階段）：**

第一版：用 regex `^[0-9]+$` 驗證 port 只含數字，不合法則跳過替換。

**強化（2026-08）：** CodeQL 仍追蹤「env 變數 → 回應內容」的資料流，單靠 regex 驗證後反射仍被視為風險。改為**嚴格轉換**：

1. `safePort()` 用 `strconv.Atoi` 把 `RTC_PORT` 轉成整數，並檢查範圍 `1–65535`
2. 不合法輸入**回退到預設 8000**，絕不反射原始字串
3. SDP 中只輸出**已驗證的整數**（`fmt.Sprintf(" %v ", port)`，`port` 是 int）— 整數型別天生不可能含 HTML/XSS 字元

```go
port := safePort()          // strconv.Atoi + 1-65535 範圍檢查 + fallback 8000
if port == 8000 { return w.w.Write(b) }  // 預設 port，SDP 無需改
// 替換 candidate 行：只輸出整數 port
line = strings.ReplaceAll(line, " 8000 ", fmt.Sprintf(" %v ", port))
```

`envRtcListen()` 現在只有 `safePort()` 呼叫它，不再直接被反射。

## 第二批：前端播放器安全修復與現代化

### 3. DOM XSS 與 Remote Property Injection

| 檔案 | 問題 | 修復 |
|---|---|---|
| `srs.sdk.js` | `document.createElement("a").href = url` 後讀取屬性，被 CodeQL 標為 DOM text reinterpreted as HTML | 改用 `new URL()` 解析；**完全移除 createElement fallback**，解析失敗回傳空欄位（不影響呼叫端） |
| `winlin.utility.js` | `obj[query[0]] = query[1]` 未驗證 key，可能污染 `__proto__`/`constructor` | **key 白名單**：只允許 `[a-zA-Z0-9._-]`，不符即跳過（比黑名單更徹底） |
| `srs_player.html` | `buildShareUrl()` 把用戶輸入直接拼進 share href（DOM XSS） | query 值全部 `encodeURIComponent`（見 4.1） |
| `srs_player.html` / `tools/player.html` / `pushdiag.html` | `video.src` / `audio.src` 直接賦值用戶輸入 URL（DOM XSS / client XSS / URL redirect） | 統一 `sanitizeUrl()` 只允許 http/https，其餘回 `about:blank` |
| `winlin.utility.js` | `parse_rtmp_url` 用 `createElement("a").href` 解析 | 改用 `new URL(url, base)` + try/catch 安全 fallback |

### 4. 播放器現代化

**改動：**

- 新增 `css/player.css` — 深色主題設計系統（CSS 變數、響應式佈局）
- `srs_player.html` — 移除 jQuery，改用 vanilla JS；hls.js/mpegts.js/dash.js 改用 CDN 最新版本
- `tools/player.html` — 同上，極簡嵌入式播放器
- `rtc_player.html` — 移除 jQuery，使用新 CSS
- `whep.html` — 移除 jQuery，使用新 CSS

**移除的依賴：** jQuery 1.12.2、Bootstrap 2.x CSS/JS、json2.js

**升級的庫（CDN）：**
- hls.js: 1.4.14 → 1.5.17
- mpegts.js: 1.7.3（保持）
- dash.js: 4.5.1 → 4.7.4

### 4.1 `srs_player.html` share URL 的 DOM XSS

**問題：** `buildShareUrl()` 把用戶輸入的 stream URL 參數（`r.app`、`r.stream`、`r.server`、`r.port`）直接拼進 URL 後賦值給 `linkUrl.href`（`<a>` 元素）。CodeQL 標為 **DOM text reinterpreted as HTML**（`js/xss-through-dom`）— 若值含 `"`、`onmouseover=` 等字元，可能突破 URL 邊界成為 HTML 屬性。

**修復：** 對每個 query 參數值用 `encodeURIComponent` 完整編碼（host/pathname 用 `encodeURI` 保留 `/`）。用戶輸入的 `" onmouseover="alert(1)"` 被編碼成 `%2522%2520onmouseover%3D%2522...`，只能當作參數值，無法突破成 HTML 屬性或 `javascript:` scheme。

**驗證：** 正常 URL 產生的 share 連結正確；惡意輸入的 href 無裸引號、事件屬性被編碼。

## 第三批：舊 AngularJS Console 升級（消除 Library alerts）

### 5. 舊 console 的 3rdparty 函式庫

**問題：** `platform/containers/www/console/` 是舊版 AngularJS 管理介面，依賴過時的第三方函式庫，CodeQL 回報 13 個 Library alerts：

| 函式庫 | 問題 | Alerts 數 |
|---|---|---|
| `bootstrap.js` | DOM text reinterpreted as HTML | 9 |
| `angular.js` | Incomplete string escaping | 1 |
| `angular-route.js` | Incomplete string escaping | 2 |
| `adapter-7.4.0.js` | DOM text reinterpreted as HTML | 1 |

**修復方式：** 不再直接修補這些無法在 place 修復的 Library alerts，而是**完整升級 console 到 React**：

- 新增 `ui/src/pages/SrsConsole.js` — 用 React 重寫全部 console 功能（Overview/Vhosts/Streams/Clients/Configs）
- 透過平台內建的 `/api/` proxy 存取 SRS HTTP API（帶 Bearer token，取代舊 JSONP）
- 導覽列新增「控制台」入口，`UrlGenerator` 的 console 連結改指向 `/mgmt/routers-console`
- **刪除** `platform/containers/www/console/` 整個舊 AngularJS 目錄（含 `3rdparty/`）

**效果：** 13 個 Library alerts 全部消除，console 功能升級為現代 React UI。

## 安全建議

- 所有從 URL 路徑提取的檔案路徑組件，使用前必須驗證格式
- UUID 欄位統一使用 `^[0-9a-f-]+$` 正規驗證
- 環境變數用於回應內容時，需驗證只含安全字元
- JavaScript 中避免 `obj[userInput] = value` 模式，需驗證 key 安全性
- URL 解析優先使用 `new URL()` API

---

## 第四批：可寫檔案關閉時未處理錯誤（go/unhandled-writable-file-close）

### 這到底在修什麼？

**用白話講：** 程式把資料寫進檔案後，最後一步是「關閉檔案」。這個「關閉」動作也可能失敗（例如硬碟滿了、權限變了、I/O 中斷）— 失敗代表**資料可能沒有真正存進磁碟**。如果程式不管關閉的錯誤，就等於**資料可能悄悄遺失，但沒人知道**。

**為什麼重要：** 寫入（`Write`/`io.Copy`）成功不代表資料真的落盤了 — 作業系統會先放在記憶體緩衝區，直到 `Close()` 或 `Sync()` 時才真正寫入。所以 `Close()` 的錯誤是「資料是否真的保存」的最後一道防線。

### CodeQL 報了哪些地方？

| 位置 | 檔案 | 寫入內容 | 遺失風險 |
|---|---|---|---|
| `nginxGenerateConfig` 4 處 | `utils.go` | Nginx / SRS 設定檔 | 低（小文字檔，但設定錯誤會影響服務） |
| `reloadNginx` 1 處 | `utils.go` | Nginx reload 信號檔 | 低（但失敗 = Nginx 沒重新載入） |
| vLive 檔案複製 | `virtual-live-stream.go` | 上傳的大檔案（io.Copy） | **高**（大檔案最易受 I/O 中斷影響） |
| vLive multipart 上傳 | `virtual-live-stream.go` | 上傳的大檔案 | **高** |
| vLive 來源檔（誤報） | `virtual-live-stream.go` | 唯讀開啟（os.Open） | 無（唯讀不會遺失資料） |

### 怎麼修的？兩種模式

**1. 能回傳錯誤的地方（HTTP handler 的閉包）→ 用命名回傳值捕獲：**

```go
// 原本：Close 錯誤被忽略
defer f.Close()

// 修正後：Close 錯誤會被回傳給呼叫端
func() (r0 error) {           // ← 命名回傳值 r0
    f, _ := os.OpenFile(...)
    defer func() {
        if err := f.Close(); err != nil && r0 == nil {
            r0 = errors.Wrapf(err, "close file %v", fileName)  // ← Close 失敗→回傳
        }
    }()
    ...
}
```

關鍵設計：`err != nil && r0 == nil` — **只有當前面的錯誤還是 nil（寫入成功）時，才用 Close 錯誤取代**。如果寫入本身就失敗了（r0 已非 nil），就保留原本的錯誤，不覆蓋。

**2. 無法回傳錯誤的地方 → 記錄日誌：**

```go
defer func() {
    if err := f.Close(); err != nil {
        logger.Wf(ctx, "close file %v, err=%v", fileName, err)  // ← 至少留下記錄
    }
}()
```

（例如 `Config.String()` 這種回傳 `string` 的函數，沒地方放 error，就記 log。）

### 為什麼不直接 `defer f.Close()` 就好？

這是 Go 社群常見但**不嚴謹**的寫法。CodeQL 把它當成潛在 bug：對**小檔案**（設定檔）風險確實低，但對**大檔案傳輸**（vLive 上傳），Close 失敗可能代表整個上傳的檔案損壞。統一補上錯誤處理是更穩健的設計。

### 驗證

- `go build` 編譯通過
- `gofmt` 格式檢查乾淨
- 行為不變：正常情況下 Close 不會失敗，程式邏輯與原本完全一致；只有真的 Close 失敗時才會多回傳一個錯誤（而非默默吞掉）

---

## 第五批：雜項掃描清理（2026-08 補）

CodeQL 後續掃描又報了一批，逐一核對後分三類處理：

### 1. 已修、重掃消失（無需動作）

- `utils.go` Reflected XSS（#270/#269）：第四批用 `safePort()` 整數化；本批再將 line-by-line 重建改為 `bytes.ReplaceAll` 原地替換（SDP 是協定資料，Content-Type: application/sdp，非 HTML；` 8000 ` pattern 只出現在 candidate port 位置，行為等價）。CodeQL 的資料流追蹤因此不再認定「用戶值重建字串」。
- `srs_player.html` DOM XSS（#267-265）：`sanitizeUrl()` 已修
- `pushdiag_tmp_check.js`（#250/249/258/254/253）：臨時檔，已刪除

### 2. 本次修復

| Alert | 位置 | 問題 | 修復 |
|---|---|---|---|
| #248 | `pushdiag.html` 1795 | `innerHTML = flags.join()`，getStats 數值可能注入 | `badge()` 加入 HTML 跳脫（`&<>"'` → entity） |
| #257 | `pushdiag.html` 1760 | `conn === 'connected' \|\| conn === 'connected'`（identical operands，筆誤） | 改為 `connected \|\| completed` |
| #205 | `transcript.go` 1784 | `DriveAsrQueue` 寫 .srt 後 `defer f.Close()` 忽略錯誤 | 命名回傳值 `(r0 error)` + deferred Close 捕獲 |
| #204 | `dvr-local-disk.go` 1035 | `finishM3u8` 寫 m3u8 後忽略 Close 錯誤 | 命名回傳值 `(ret error)`（內部已有 r0 變數故用 ret）+ deferred Close 捕獲 |
| #203 | `ai-talk.go` 877 | `io.Copy` 後忽略 dst.Close 錯誤 | 命名回傳值 `(r0 error)` + deferred Close 捕獲；src 唯讀用 `logger.Wf` |

### 3. CodeQL 誤報或已加固（保留）

| Alert | 位置 | 處理 |
|---|---|---|
| #251 | `pushdiag.html` 527 | **已修**：SPS 的 `chroma_format_idc`（色彩格式）。原本先設預設 1 再在 High profile 分支覆蓋（CodeQL 誤判「initial value unused」）。改用三元運算子在宣告時決定（High profile 才讀 bitstream），並把結果回傳 + 在 videoInfo 面板顯示色彩格式（4:2:0/4:2:2/4:4:4） |
| #252 | `pushdiag.html` 741 | **已修**：`avcInfo = 'AVC seq header (SPS)'` 是死賦值（row.detail 已設 'SPS/PPS'）。移除死賦值，並讓 SPS 行的 detail 顯示解析度/Profile/Level（如 `SPS/PPS 1280×720 High L3.1`） |
| #27 | `service.go` 512-528 | **已加固**：`/terraform/v1/debug/goroutines` 原本只綁 `127.0.0.1:22022`（loopback），第六批又加了 `Authenticate` 驗證（需 `Authorization: Bearer <SRS_PLATFORM_SECRET>`），即使誤綁到非 loopback 也不洩漏 stack trace |

> **經驗：** CodeQL 的 `js/xss-through-dom` 對「統計數字 → innerHTML」會保守報錯。最乾淨的解法是**統一跳脫**（如 badge()），而非逐個加 sanitize。
> `go/unhandled-writable-file-close` 的修法已在第四批詳述，本批只是把相同模式套到其餘 3 個檔案。

---

## 第六批：preview mode 冗餘條件清理（#263-259）

5 個 player 頁的 `if (isPreview)` 被 CodeQL 報為「useless conditional」（This negation always evaluates to true）。

**根本原因：** 每個頁面都是這個模式：

```js
if (isPreview) {
    ...載入佔位影片...
    return;        // ← 早退
}
...
if (!isPreview) {  // ← 冗餘！早退後 isPreview 恆為 false
    ...初始化...
}
```

頂部的 `if (isPreview) { ...; return; }` 已經把 preview 情況處理完並早退，執行到後面的程式碼時 `isPreview` 必然為 false，所以 `if (!isPreview)` 是多餘包覆（CodeQL 的判斷正確）。

**修復：** 直接移除 `if (!isPreview)` 包覆，後面的初始化程式碼必然執行。檔案：`whep.html`、`whip.html`、`rtc_player.html`、`rtc_publisher.html`、`tools/player.html`。

**驗證：** 語法全 OK；HTTP 模式下非 preview 初始化正常（URL 自動填入、按鈕存在、banner 隱藏）。preview（file://）模式邏輯不變（早退仍在）。

---

## 第七批：近期 Go 品質與可維護性修正（2026-08）

本批不是 CodeQL alert，而是配合 `GOOS=linux` 的 gopls / vet / staticcheck 分析，
清理長期累積的 Go 品質問題，並修掉兩個潛在 bug。

### 1. Reflected XSS 根治（CodeQL #272/#273）

`whxpResponseModifier`（自訂 `ResponseWriter`）在 `Write()` 直接寫回上游 proxy body，
被 `go/reflected-xss` 判定為「user-provided value 進 HTTP 回應」，前幾批的 regex /
整數驗證都無法消除。**根治**：改用 `httputil.ReverseProxy.ModifyResponse` 在 proxy
內部重寫 WHIP/WHEP SDP 的 RTC port（`modifySdpRtcPort`），**徹底移除自訂
`ResponseWriter`**——被 flag 的 sink 整個消失，與 proxy1985/8080（原本就沒被報）
的 pattern 一致。行為等價：只在成功且 Content-Type 含 `sdp` 時，用 `safePort()`
驗證過的整數重寫 port。檔案：`platform/utils.go`、`platform/service.go`。

### 2. 潛在 bug：`strconv.ParseFloat` bitSize 錯誤（staticcheck SA1030）

`transcript.go` 原本 `strconv.ParseFloat(format.Format.Starttime, 10)`——`bitSize`
**只能是 32 或 64**，傳 10 會直接 panic。改為 `64`（符合原意，float64）。

### 3. 潛在 bug：空的 critical section（staticcheck SA2001）

`ocr.go` 的 `OnTsSegment` 原本 `v.lock.Lock(); v.lock.Unlock()` 中間沒保護任何東西
（queue 的方法已內建 lock），是空的鎖區段。移除多餘 lock，直接 enqueue。

### 4. copylocks：value receiver 複製含 Mutex 的 struct（go vet）

`RecordM3u8Stream` / `DvrM3u8Stream` / `VodM3u8Stream` 的 `String()` 用 value receiver，
會複製含 `sync.Mutex` 的 struct。改為 pointer receiver。檔案：`dvr-local-disk.go`、
`dvr-tencent-cos.go`、`dvr-tencent-vod.go`。

### 5. context leak：`chatTaskCancel` 未在所有分支呼叫（go vet）

`ai-talk.go` 的 `context.WithCancel` 若走 merge 或 chat-disabled 分支，cancel 從不呼叫。
建立後立即 `defer chatTaskCancel()`。

### 6. gopls diagnostics 清理（GOOS=linux 下 39 條 info）

- **unused parameter（24 條）**：未用參數改 `_`（保留簽名）。涵蓋 `discoverSource` /
  `discoverRegistry` / `buildVodM3u8` 系列、各 Dvr/Vod/Record `updateArtifact`/
  `finishArtifact`/`addMessage`、`probeFFmpegDevices` / `queryLatestVersion`、
  transcript/ocr 的 `reset`/`restart`/`clearSubtitle`、`httpAllowCORS` 等。
- **redundant type from composite literal（7 條）**：`[]*T{&T{...}}` 簡化 `[]*T{{...}}`。
- **unused write to field Data（1 條）**：`dubbing.go` 建立 `audio.IntBuffer` 後只用
  `Format`，移除未讀的 `Data` 初始化。
- **impossible condition: non-nil == nil（1 條）**：`dubbing.go` 的 `nextGroup == nil`
  在前一行已判 nil，冗餘檢查移除。
- **time.Now().Sub → time.Since（5 處）**：`service.go`、`virtual-live-stream.go`。

### 7. 驗證方法（重要）

VSCode 預設 gopls 用 Windows target，很多 Linux 才有的問題看不到。本機透過
`.vscode/settings.json` 設定 `gopls.env.GOOS=linux`。CLI 驗證：

```powershell
cd platform
$env:GOOS="linux"; $env:GOARCH="amd64"
go build ./...        # 編譯
go vet ./...          # vet
# staticcheck（需 Go 1.26 容器）：
docker run --rm -v "F:\oryx:/oryx" -w /oryx/platform golang:1.26 sh -c "go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./..."
# gopls 完整診斷（含 info 層級建議）：
$files = Get-ChildItem *.go | ? { $_.Name -notmatch '_test|vendor' } | % { $_.FullName }
gopls check -severity=hint $files
```
