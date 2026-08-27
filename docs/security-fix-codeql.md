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
| `srs.sdk.js` | `document.createElement("a").href = url` 後讀取屬性，被 CodeQL 標為 DOM text reinterpreted as HTML | 改用 `new URL()` 解析，fallback 到 `createElement("a")` |
| `winlin.utility.js` | `obj[query[0]] = query[1]` 未驗證 key，可能污染 `__proto__`/`constructor` | 加入 `__proto__`/`constructor`/`prototype` key 過濾 |

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
