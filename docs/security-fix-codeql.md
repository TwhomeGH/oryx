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

**問題：** `whxpResponseModifier.Write()` 中，環境變數 `RTC_LISTEN` 的值（port）透過 `fmt.Sprintf` 嵌入 SDP 回應內容。

**修復方式：** 用 regex `^[0-9]+$` 驗證 port 只含數字，不合法則跳過替換。

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

## 安全建議

- 所有從 URL 路徑提取的檔案路徑組件，使用前必須驗證格式
- UUID 欄位統一使用 `^[0-9a-f-]+$` 正規驗證
- 環境變數用於回應內容時，需驗證只含安全字元
- JavaScript 中避免 `obj[userInput] = value` 模式，需驗證 key 安全性
- URL 解析優先使用 `new URL()` API
