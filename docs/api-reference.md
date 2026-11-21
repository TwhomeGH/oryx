# API 參考（自動產生）

> 本檔由 `scripts/tools/gen-api-reference.py` 掃描 `platform/*.go` 的註冊點產生，**請勿手改**。
>
> 更新：`python scripts/tools/gen-api-reference.py`；CI 以 `--check` 驗證是否過期。

共 **148** 個 `/terraform/v1` 端點。認證欄位：

| 值 | 意義 |
| --- | --- |
| `body token` | 請求 body 需帶 `token`（`middlewareAuthTokenInBody`） |
| `query token` | 以查詢參數帶 token（`middlewareAuthTokenInURL`） |
| `—` | 沒有掛中介層；可能是 SRS hook（由 SRS 直接呼叫）或刻意公開 |

動作欄位列出該處理函式 `switch action` 的分支值；`單一` 表示該端點沒有 action 分支。

說明取自處理函式的註解；沒有註解時由路徑推導中文標籤（例：`mgmt/gpu/query` → GPU · 查詢），所以不會出現空白列。目前 **129** 個是推導標籤、**19** 個有散文說明。

## ai（21）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/ai/ocr/apply` | 單一 | body token | AI · OCR · 套用 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/callback-queue` | 單一 | body token | AI · OCR · 回呼佇列 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/check` | 單一 | body token | AI · OCR · 檢查 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/cleanup-queue` | 單一 | body token | AI · OCR · 清理佇列 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/image/` | 單一 | — | AI · OCR · 影像 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/live-queue` | 單一 | body token | AI · OCR · 直播佇列 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/ocr-queue` | 單一 | body token | AI · OCR · OCR 佇列 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/query` | 單一 | body token | AI · OCR · 查詢 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/reset` | 單一 | body token | AI · OCR · 重置 | | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/transcript/apply` | 單一 | body token | AI · 字幕 · 套用 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/asr-queue` | 單一 | body token | AI · 字幕 · ASR 佇列 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/check` | 單一 | body token | AI · 字幕 · 檢查 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/clear-subtitle` | 單一 | body token | AI · 字幕 · 清除字幕 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/fix-queue` | 單一 | body token | AI · 字幕 · 修正佇列 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/hls/original/` | 單一 | — | AI · 字幕 · HLS · 原始 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/hls/overlay/` | 單一 | — | AI · 字幕 · HLS · 疊圖 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/hls/webvtt/` | 單一 | — | AI · 字幕 · HLS · WebVTT | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/live-queue` | 單一 | body token | AI · 字幕 · 直播佇列 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/overlay-queue` | 單一 | body token | AI · 字幕 · 疊圖佇列 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/query` | 單一 | body token | AI · 字幕 · 查詢 | | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/reset` | 單一 | body token | AI · 字幕 · 重置 | | `TranscriptWorker.Handle`（transcript.go） |

## ai-talk（14）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/ai-talk/badword/query` | 單一 | body token | AI 對話 · 過濾詞 · 查詢 | `redis-architecture.md` | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/badword/update` | 單一 | body token | AI 對話 · 過濾詞 · 更新 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/conversation` | 單一 | — | AI 對話 · 舞台 · 對話 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/hello-voices/` | 單一 | — | AI 對話 · 舞台 · 招呼語音清單 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/query` | 單一 | — | AI 對話 · 舞台 · 查詢 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/start` | 單一 | — | AI 對話 · 舞台 · 開始 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/upload` | 單一 | — | AI 對話 · 舞台 · 上傳 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/verify` | 單一 | — | AI 對話 · 舞台 · 驗證 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/query` | 單一 | — | AI 對話 · 訂閱 · 查詢 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/remove` | 單一 | — | AI 對話 · 訂閱 · 移除 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/start` | 單一 | — | AI 對話 · 訂閱 · 開始 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/tts` | 單一 | — | AI 對話 · 訂閱 · 語音合成 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/user/query` | 單一 | — | AI 對話 · 使用者 · 查詢 | | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/user/update` | 單一 | — | AI 對話 · 使用者 · 更新 | | `handleAITalkService`（ai-talk.go） |

## debug（1）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/debug/goroutines` | 單一 | — | 除錯 · 協程 | `security-fix-codeql.md` | `handleDebuggingGoroutines`（service.go） |

## dubbing（13）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/dubbing/create` | 單一 | body token | 配音 · 建立 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/export` | 單一 | body token | 配音 · 匯出 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/list` | 單一 | body token | 配音 · 列表 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/play` | 單一 | query token | 配音 · 播放 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/query` | 單一 | body token | 配音 · 查詢 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/remove` | 單一 | body token | 配音 · 移除 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/source` | 單一 | body token | 配音 · 來源 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-merge` | 單一 | body token | 配音 · 任務合併 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-query` | 單一 | body token | 配音 · 任務查詢 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-rephrase` | 單一 | body token | 配音 · task rephrase | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-start` | 單一 | body token | 配音 · 任務開始 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-tts` | 單一 | query token | 配音 · 任務語音合成 | | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/update` | 單一 | body token | 配音 · 更新 | | `handleDubbingService`（dubbing.go） |

## ffmpeg（20）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/ffmpeg/camera/secret` | update | body token | 攝影機 · 金鑰設定 | | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/camera/source` | update | body token | 攝影機 · 來源 | | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/camera/stream-url` | update | body token | 攝影機 · stream url | | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/camera/streams` | update | body token | 攝影機 · 串流 | | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/forward/enabled` | delete, metrics, sessions, timing, toggle, update | body token | The global forward switch: query or toggle whether all forwarding is paused, without changing each target's own enabled preference. | | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/forward/secret` | delete, metrics, sessions, timing, toggle, update | body token | 轉播 · 金鑰設定 | `forward-architecture.md`, `forward-program-bus.md`, `forward-quick-test.md` | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/forward/slate` | delete, metrics, sessions, timing, toggle, update | body token | The fallback slate configuration: timezone of the clock, the explanatory text, and whether to stop a forward after the source has been offline for a while. | | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/forward/streams` | delete, metrics, sessions, timing, toggle, update | body token | 轉播 · 串流 | `forward-architecture.md`, `forward-quick-test.md` | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/transcode/apply` | 單一 | body token | 轉碼 · 套用 | | `TranscodeWorker.Handle`（trancode.go） |
| `/terraform/v1/ffmpeg/transcode/query` | 單一 | body token | 轉碼 · 查詢 | | `TranscodeWorker.Handle`（trancode.go） |
| `/terraform/v1/ffmpeg/transcode/task` | 單一 | body token | 轉碼 · 任務 | | `TranscodeWorker.Handle`（trancode.go） |
| `/terraform/v1/ffmpeg/versions` | 單一 | — | 版本 | | `handleFFmpegVersions`（service.go） |
| `/terraform/v1/ffmpeg/vlive/secret` | delete, update | body token | 虛擬直播 · 金鑰設定 | | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/server` | delete, update | body token | 虛擬直播 · server | `local-test.md` | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/source` | delete, update | body token | 虛擬直播 · 來源 | | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/stream-url` | delete, update | body token | 虛擬直播 · stream url | | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/streamUrl` | delete, update | body token | 虛擬直播 · streamUrl | | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/streams` | delete, update | body token | 虛擬直播 · 串流 | | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/upload/` | delete, update | — | 虛擬直播 · 上傳 | | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/ytdl` | delete, update | body token | 虛擬直播 · ytdl | | `VLiveWorker.Handle`（virtual-live-stream.go） |

## hooks（23）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/hooks/dvr/apply` | 單一 | — | 回呼掛鉤 · dvr · 套用 | | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/dvr/files` | 單一 | — | 回呼掛鉤 · dvr · 檔案 | | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/dvr/hls/` | 單一 | — | 回呼掛鉤 · dvr · HLS | | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/dvr/query` | 單一 | — | 回呼掛鉤 · dvr · 查詢 | | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/record/apply` | 單一 | — | 回呼掛鉤 · 錄影 · 套用 | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/end` | 單一 | — | 回呼掛鉤 · 錄影 · end | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/files` | 單一 | — | 回呼掛鉤 · 錄影 · 檔案 | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/globs` | 單一 | — | 回呼掛鉤 · 錄影 · globs | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/hls/` | 單一 | — | Format is :uuid.m3u8 or :uuid/index.m3u8 | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/post-processing` | 單一 | — | 回呼掛鉤 · 錄影 · post processing | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/query` | 單一 | — | 回呼掛鉤 · 錄影 · 查詢 | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/remove` | 單一 | — | 回呼掛鉤 · 錄影 · 移除 | | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/srs/hls` | 單一 | — | TODO: FIXME: Fixed token. See `https://github.com/ossrs/srs/wiki/v4_EN_HTTPCallback` | | `handleOnHls`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret` | on_play | body token | 回呼掛鉤 · SRS · 金鑰設定 | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret/disable` | on_play | body token | 回呼掛鉤 · SRS · 金鑰設定 · 停用 | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret/query` | on_play | body token | 回呼掛鉤 · SRS · 金鑰設定 · 查詢 | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret/update` | on_play | body token | 回呼掛鉤 · SRS · 金鑰設定 · 更新 | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/verify` | on_play | — | See `https://ossrs.io/lts/en-us/docs/v5/doc/http-callback` | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/versions` | on_play | — | 回呼掛鉤 · 版本 | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/vod/apply` | 單一 | — | 回呼掛鉤 · vod · 套用 | | `VodWorker.Handle`（dvr-tencent-vod.go） |
| `/terraform/v1/hooks/vod/files` | 單一 | — | 回呼掛鉤 · vod · 檔案 | | `VodWorker.Handle`（dvr-tencent-vod.go） |
| `/terraform/v1/hooks/vod/hls/` | 單一 | — | 回呼掛鉤 · vod · HLS | | `VodWorker.Handle`（dvr-tencent-vod.go） |
| `/terraform/v1/hooks/vod/query` | 單一 | — | 回呼掛鉤 · vod · 查詢 | | `VodWorker.Handle`（dvr-tencent-vod.go） |

## host（1）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/host/versions` | 單一 | — | 主機 · 版本 | | `handleHostVersions`（service.go） |

## live（5）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/live/room/create` | 單一 | body token | 直播 · 直播間 · 建立 | | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/list` | 單一 | body token | 直播 · 直播間 · 列表 | | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/query` | 單一 | body token | 直播 · 直播間 · 查詢 | | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/remove` | 單一 | body token | 直播 · 直播間 · 移除 | | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/update` | 單一 | body token | 直播 · 直播間 · 更新 | | `handleLiveRoomService`（live-room.go） |

## mgmt（47）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/mgmt/auto-self-signed-certificate` | 單一 | body token | 自動自簽憑證 | | `handleMgmtAutoSelfSignedCertificate`（service.go） |
| `/terraform/v1/mgmt/beian/query` | 單一 | — | Note that this API is not verified by token | | `handleMgmtBeianQuery`（service.go） |
| `/terraform/v1/mgmt/beian/update` | 單一 | body token | 備案 · 更新 | | `handleMgmtBeianUpdate`（service.go） |
| `/terraform/v1/mgmt/bilibili` | 單一 | body token | B 站 | `api-auth-architecture.md`, `bilibili-tutorials-cache.md` | `handleMgmtBilibili`（service.go） |
| `/terraform/v1/mgmt/callback/query` | 單一 | body token | 回呼 · 查詢 | | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/callback/test` | 單一 | body token | Sends one synthetic event through the real path, so "is the target reachable and does it accept our payload" is answerable without publishing a stream. | | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/cert/query` | 單一 | body token | 憑證 · 查詢 | | `handleMgmtCertQuery`（service.go） |
| `/terraform/v1/mgmt/check` | 單一 | — | 檢查 | `api-auth-architecture.md` | `handleMgmtCheck`（service.go） |
| `/terraform/v1/mgmt/contact/query` | 單一 | body token | 聯絡 · 查詢 | | `handleMgmtContactQuery`（service.go） |
| `/terraform/v1/mgmt/disk/query` | 單一 | body token | reports capacity for the filesystems Oryx writes to | | `handleMgmtDiskQuery`（disk.go） |
| `/terraform/v1/mgmt/envs` | 單一 | — | 環境變數 | `api-auth-architecture.md` | `handleMgmtEnvs`（service.go） |
| `/terraform/v1/mgmt/ffmpeg/capabilities` | 單一 | body token | capabilities | `frontend-guide.md` | `handleMgmtFFmpegCapabilities`（ffmpeg-capability.go） |
| `/terraform/v1/mgmt/gpu/query` | 單一 | body token | reports GPU utilization (empty when no GPU is exposed to the container) | | `handleMgmtGPUQuery`（gpu.go） |
| `/terraform/v1/mgmt/hlsll/query` | 單一 | body token | 低延遲 HLS · 查詢 | | `handleMgmtHlsLowLatencyQuery`（service.go） |
| `/terraform/v1/mgmt/hlsll/update` | 單一 | body token | 低延遲 HLS · 更新 | | `handleMgmtHlsLowLatencyUpdate`（service.go） |
| `/terraform/v1/mgmt/hooks/apply` | 單一 | body token | 回呼掛鉤 · 套用 | | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/hooks/example` | 單一 | — | 回呼掛鉤 · example | | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/hooks/query` | 單一 | body token | 回呼掛鉤 · 查詢 | | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/hphls/query` | 單一 | body token | 高效能 HLS · 查詢 | | `handleMgmtNginxHlsQuery`（service.go） |
| `/terraform/v1/mgmt/hphls/update` | 單一 | body token | 高效能 HLS · 更新 | | `handleMgmtNginxHlsUpdate`（service.go） |
| `/terraform/v1/mgmt/init` | 單一 | — | 初始化 | `api-auth-architecture.md`, `local-test.md` | `handleMgmtInit`（service.go） |
| `/terraform/v1/mgmt/letsencrypt` | 單一 | body token | Let's Encrypt | | `handleMgmtLetsEncrypt`（service.go） |
| `/terraform/v1/mgmt/limits/query` | 單一 | body token | 限制 · 查詢 | | `handleMgmtLimitsQuery`（service.go） |
| `/terraform/v1/mgmt/limits/update` | 單一 | body token | 限制 · 更新 | | `handleMgmtLimitsUpdate`（service.go） |
| `/terraform/v1/mgmt/login` | 單一 | — | 登入 | `api-auth-architecture.md`, `local-test.md`, `streams-query-api.md` | `handleMgmtLogin`（service.go） |
| `/terraform/v1/mgmt/openai/query` | 單一 | body token | OpenAI · 查詢 | | `handleMgmtOpenAIQuery`（service.go） |
| `/terraform/v1/mgmt/openai/update` | 單一 | body token | OpenAI · 更新 | | `handleMgmtOpenAIUpdate`（service.go） |
| `/terraform/v1/mgmt/record/query` | 單一 | body token | reports the DVR state: what is recording now, what finished, and why a recording looks unhealthy | | `handleMgmtRecordQuery`（record-diag.go） |
| `/terraform/v1/mgmt/redis/info` | 單一 | body token | Redis/Valkey · info | `console-monitoring.md`, `frontend-guide.md` | `handleMgmtRedisInfo`（console-metrics.go） |
| `/terraform/v1/mgmt/redis/query` | calls, usec | body token | reports Redis/Valkey health: INFO summary, per-command stats and the slow log, so operators can see it without shelling into the container | | `handleMgmtRedisQuery`（service.go） |
| `/terraform/v1/mgmt/rtc/query` | 單一 | body token | reports the WebRTC configuration and live peers | | `handleMgmtRtcQuery`（rtc-diag.go） |
| `/terraform/v1/mgmt/secret/query` | 單一 | body token | 金鑰設定 · 查詢 | | `handleMgmtSecretQuery`（service.go） |
| `/terraform/v1/mgmt/srs/capabilities` | 單一 | body token | exposes an API to verify whether the current SRS binary supports the features Oryx depends on | `version-and-upgrade.md` | `handleSrsCapabilities`（srs-capability.go） |
| `/terraform/v1/mgmt/srs/health/query` | 單一 | ? | serves the 診斷 card | | `handleMgmtSrsHealthQuery`（srs-health.go） |
| `/terraform/v1/mgmt/srs/hls/query` | 單一 | body token | SRS · HLS · 查詢 | | `handleMgmtSrsHlsQuery`（service.go） |
| `/terraform/v1/mgmt/srs/hls/update` | 單一 | body token | toggles the vhost HLS master switch | | `handleMgmtSrsHlsUpdate`（service.go） |
| `/terraform/v1/mgmt/srs/latency/query` | 單一 | body token | SRS · 延遲 · 查詢 | | `handleMgmtSrsLatencyQuery`（service.go） |
| `/terraform/v1/mgmt/srs/latency/update` | 單一 | body token | sets the vhost latency baseline: "compat" (default, stable throughput) or "low" (min_latency + tcp_nodelay on) | `change-history.md` | `handleMgmtSrsLatencyUpdate`（service.go） |
| `/terraform/v1/mgmt/ssl` | 單一 | body token | TLS | | `handleMgmtSsl`（service.go） |
| `/terraform/v1/mgmt/status` | 單一 | body token | 狀態 | `api-auth-architecture.md` | `handleMgmtStatus`（service.go） |
| `/terraform/v1/mgmt/streams/events/clear` | 單一 | body token | clears the bounded publish/unpublish event log | | `handleMgmtStreamsEventsClear`（service.go） |
| `/terraform/v1/mgmt/streams/fps` | 單一 | body token | 串流 · fps | `frontend-guide.md`, `streams-query-api.md` | `handleMgmtStreamsFPS`（service.go） |
| `/terraform/v1/mgmt/streams/kickoff` | 單一 | body token | 串流 · kickoff | `streams-query-api.md` | `handleMgmtStreamsKickoff`（service.go） |
| `/terraform/v1/mgmt/streams/query` | 單一 | body token | 串流 · 查詢 | `streams-query-api.md` | `handleMgmtStreamsQuery`（service.go） |
| `/terraform/v1/mgmt/token` | 單一 | body token | 權杖 | `api-auth-architecture.md` | `handleMgmtToken`（service.go） |
| `/terraform/v1/mgmt/tutorials` | 單一 | body token | 教學 | `api-auth-architecture.md`, `bilibili-tutorials-cache.md` | `handleMgmtTutorials`（service.go） |
| `/terraform/v1/mgmt/versions` | 單一 | — | 版本 | `local-build.md` | `handleMgmtVersions`（service.go） |

## releases（1）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/releases` | 單一 | ? | For version management. | | `handleHTTPService`（service.go） |

## tencent（2）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
| --- | --- | --- | --- | --- | --- |
| `/terraform/v1/tencent/cam/secret` | on_play | body token | See `https://console.cloud.tencent.com/cam` | | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/tencent/versions` | on_play | — | 騰訊雲 · 版本 | | `handleHooksService`（srs-hooks.go） |

---

## 還缺什麼

- 這份索引只回答「有哪些端點、怎麼認證、有哪些動作」。
- 完整 schema（請求／回應欄位）尚未產生；核心端點見各 `docs/*.md` 與程式內 `ParseBody` 結構。
- 端點若在程式裡改名或新增而沒有重新產生本檔，`--check` 會讓 CI 失敗。
