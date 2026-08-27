# CodeQL 安全漏洞修復說明

本 fork 於 2026-08 收到 CodeQL 掃描報告，共 31 個 High 級別警示，分為兩類：

1. **路徑穿越（Uncontrolled data used in path expression）** — 29 個
2. **反射型跨站腳本（Reflected XSS）** — 2 個

## 修復內容

### 1. 路徑穿越（Path Traversal）

**問題：** HTTP handler 從 `r.URL.Path` 提取檔名或 UUID 後，直接用於構建檔案路徑（`path.Join` + `os.Open`），未驗證是否含 `..` 等路徑穿越字元。攻擊者可透過 crafted URL 讀取任意檔案。

**修復方式：**

| 檔案 | 修復手法 |
|---|---|
| `ai-talk.go` | `http.ServeFile` 前驗證 filename 不含 `..`、`/`、`\` |
| `dvr-local-disk.go` | uuid 用 regex `^[0-9a-f-]+$` 驗證；dir 不含 `..`、`/`、`\`；m3u8 用 regex `^[0-9a-zA-Z._-]+$` 驗證 |
| `transcript.go` | 所有 handler 的 uuid 加 regex `^[0-9a-f-]+$` 驗證 |
| `ocr.go` | uuid 加 regex `^[0-9a-f-]+$` 驗證 |
| `virtual-live-stream.go` | upload handler 的 filename 驗證不含 `..`、`/`、`\` |

**影響的 API 端點：**

- `/terraform/v1/ai-talk/stage/hello-voices/` — AI 語音範例檔 serve
- `/terraform/v1/hooks/record/hls/` — DVR 錄影 HLS/MP4 serve
- `/terraform/v1/ai/transcript/hls/webvtt/` — 轉錄字幕 HLS serve
- `/terraform/v1/ai/transcript/hls/overlay/` — 轉錄疊加 HLS serve
- `/terraform/v1/ai/transcript/hls/original/` — 轉錄原始 HLS serve
- `/terraform/v1/ai/ocr/image/` — OCR 圖片 serve
- `/terraform/v1/ffmpeg/vlive/upload/` — 虛擬直播上傳

### 2. 反射型 XSS（Reflected XSS）

**問題：** `whxpResponseModifier.Write()` 中，環境變數 `RTC_LISTEN` 的值（port）透過 `fmt.Sprintf` 嵌入 SDP 回應內容，若 port 含惡意 HTML/JS 可被瀏覽器執行。

**修復方式：** 在使用 port 前用 regex `^[0-9]+$` 驗證只含數字，不合法則跳過替換直接返回原始內容。

**影響的端點：** WHIP/WHEP proxy 的 SDP 回應。

## 安全建議

- 所有從 URL 路徑提取的檔案路徑組件，使用前必須驗證格式
- UUID 欄位統一使用 `^[0-9a-f-]+$` 正規驗證
- 環境變數用於回應內容時，需驗證只含安全字元
- 建議後續新增 API 時遵循相同的輸入驗證模式
