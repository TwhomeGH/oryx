# CodeQL 安全漏洞修復說明

本 fork 於 2026-08 收到 CodeQL 掃描報告，分為多個類別，已全部修復。

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

- `utils.go` Reflected XSS（#270/#269）：第四批已用 `safePort()` 整數化修復
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

### 3. CodeQL 誤報（保留，不改）

| Alert | 位置 | 原因 |
|---|---|---|
| #263-259 | 5 個 player 頁的 `if (isPreview)` | preview mode 設計：file:// 協定時載入佔位影片，非 preview 走正常流程，兩分支互斥但都需要 |
| #252/251 | `pushdiag.html` 527/741 | 合法的 fallback/分支互斥邏輯（非 high profile 時 chroma 固定 1） |
| #27 | `service.go` 518 | goroutine stack trace 只綁 `127.0.0.1:22022`（本機 debug server），不對外暴露 |

> **經驗：** CodeQL 的 `js/xss-through-dom` 對「統計數字 → innerHTML」會保守報錯。最乾淨的解法是**統一跳脫**（如 badge()），而非逐個加 sanitize。
> `go/unhandled-writable-file-close` 的修法已在第四批詳述，本批只是把相同模式套到其餘 3 個檔案。
