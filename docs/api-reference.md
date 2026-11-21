# API 參考（自動產生）

> 本檔由 `scripts/tools/gen-api-reference.py` 掃描 `platform/*.go` 的註冊點產生，**請勿手改**。
>
> 更新：`python scripts/tools/gen-api-reference.py`；CI 以 `--check` 驗證是否過期。

共 **148** 個 `/terraform/v1` 端點。認證欄位：

| 值 | 意義 |
|---|---|
| `body token` | 請求 body 需帶 `token`（`middlewareAuthTokenInBody`） |
| `query token` | 以查詢參數帶 token（`middlewareAuthTokenInURL`） |
| `—` | 沒有掛中介層；可能是 SRS hook（由 SRS 直接呼叫）或刻意公開 |

動作欄位列出該處理函式 `switch action` 的分支值；沒有分支表示單一用途端點。

說明欄為 `—` 的端點有 **129** 個（該處理函式沒有註解）—— 這是目前的可見文件債。

## ai（21）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/ai/ocr/apply` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/callback-queue` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/check` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/cleanup-queue` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/image/` | — | — | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/live-queue` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/ocr-queue` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/query` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/ocr/reset` | — | body token | — | — | `OCRWorker.Handle`（ocr.go） |
| `/terraform/v1/ai/transcript/apply` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/asr-queue` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/check` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/clear-subtitle` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/fix-queue` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/hls/original/` | — | — | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/hls/overlay/` | — | — | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/hls/webvtt/` | — | — | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/live-queue` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/overlay-queue` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/query` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |
| `/terraform/v1/ai/transcript/reset` | — | body token | — | — | `TranscriptWorker.Handle`（transcript.go） |

## ai-talk（14）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/ai-talk/badword/query` | — | body token | — | `redis-architecture.md` | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/badword/update` | — | body token | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/conversation` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/hello-voices/` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/query` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/start` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/upload` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/stage/verify` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/query` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/remove` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/start` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/subscribe/tts` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/user/query` | — | — | — | — | `handleAITalkService`（ai-talk.go） |
| `/terraform/v1/ai-talk/user/update` | — | — | — | — | `handleAITalkService`（ai-talk.go） |

## debug（1）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/debug/goroutines` | — | — | — | `security-fix-codeql.md` | `handleDebuggingGoroutines`（service.go） |

## dubbing（13）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/dubbing/create` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/export` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/list` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/play` | — | query token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/query` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/remove` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/source` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-merge` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-query` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-rephrase` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-start` | — | body token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/task-tts` | — | query token | — | — | `handleDubbingService`（dubbing.go） |
| `/terraform/v1/dubbing/update` | — | body token | — | — | `handleDubbingService`（dubbing.go） |

## ffmpeg（20）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/ffmpeg/camera/secret` | update | body token | — | — | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/camera/source` | update | body token | — | — | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/camera/stream-url` | update | body token | — | — | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/camera/streams` | update | body token | — | — | `CameraWorker.Handle`（camera-live-stream.go） |
| `/terraform/v1/ffmpeg/forward/enabled` | delete, metrics, sessions, timing, toggle, update | body token | The global forward switch: query or toggle whether all forwarding is paused, without changing each target's own enabled preference. | — | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/forward/secret` | delete, metrics, sessions, timing, toggle, update | body token | — | `forward-architecture.md`, `forward-program-bus.md`, `forward-quick-test.md` | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/forward/slate` | delete, metrics, sessions, timing, toggle, update | body token | The fallback slate configuration: timezone of the clock, the explanatory text, and whether to stop a forward after the source has been offline for a while. | — | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/forward/streams` | delete, metrics, sessions, timing, toggle, update | body token | — | `forward-architecture.md`, `forward-quick-test.md` | `ForwardWorker.Handle`（forward.go） |
| `/terraform/v1/ffmpeg/transcode/apply` | — | body token | — | — | `TranscodeWorker.Handle`（trancode.go） |
| `/terraform/v1/ffmpeg/transcode/query` | — | body token | — | — | `TranscodeWorker.Handle`（trancode.go） |
| `/terraform/v1/ffmpeg/transcode/task` | — | body token | — | — | `TranscodeWorker.Handle`（trancode.go） |
| `/terraform/v1/ffmpeg/versions` | — | — | — | — | `handleFFmpegVersions`（service.go） |
| `/terraform/v1/ffmpeg/vlive/secret` | delete, update | body token | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/server` | delete, update | body token | — | `local-test.md` | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/source` | delete, update | body token | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/stream-url` | delete, update | body token | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/streamUrl` | delete, update | body token | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/streams` | delete, update | body token | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/upload/` | delete, update | — | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |
| `/terraform/v1/ffmpeg/vlive/ytdl` | delete, update | body token | — | — | `VLiveWorker.Handle`（virtual-live-stream.go） |

## hooks（23）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/hooks/dvr/apply` | — | — | — | — | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/dvr/files` | — | — | — | — | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/dvr/hls/` | — | — | — | — | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/dvr/query` | — | — | — | — | `DvrWorker.Handle`（dvr-tencent-cos.go） |
| `/terraform/v1/hooks/record/apply` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/end` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/files` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/globs` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/hls/` | — | — | Format is :uuid.m3u8 or :uuid/index.m3u8 | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/post-processing` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/query` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/record/remove` | — | — | — | — | `RecordWorker.Handle`（dvr-local-disk.go） |
| `/terraform/v1/hooks/srs/hls` | — | — | TODO: FIXME: Fixed token. See https://github.com/ossrs/srs/wiki/v4_EN_HTTPCallback | — | `handleOnHls`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret` | on_play | body token | — | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret/disable` | on_play | body token | — | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret/query` | on_play | body token | — | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/secret/update` | on_play | body token | — | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/srs/verify` | on_play | — | See https://ossrs.io/lts/en-us/docs/v5/doc/http-callback | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/versions` | on_play | — | — | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/hooks/vod/apply` | — | — | — | — | `VodWorker.Handle`（dvr-tencent-vod.go） |
| `/terraform/v1/hooks/vod/files` | — | — | — | — | `VodWorker.Handle`（dvr-tencent-vod.go） |
| `/terraform/v1/hooks/vod/hls/` | — | — | — | — | `VodWorker.Handle`（dvr-tencent-vod.go） |
| `/terraform/v1/hooks/vod/query` | — | — | — | — | `VodWorker.Handle`（dvr-tencent-vod.go） |

## host（1）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/host/versions` | — | — | — | — | `handleHostVersions`（service.go） |

## live（5）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/live/room/create` | — | body token | — | — | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/list` | — | body token | — | — | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/query` | — | body token | — | — | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/remove` | — | body token | — | — | `handleLiveRoomService`（live-room.go） |
| `/terraform/v1/live/room/update` | — | body token | — | — | `handleLiveRoomService`（live-room.go） |

## mgmt（47）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/mgmt/auto-self-signed-certificate` | — | body token | — | — | `handleMgmtAutoSelfSignedCertificate`（service.go） |
| `/terraform/v1/mgmt/beian/query` | — | — | Note that this API is not verified by token | — | `handleMgmtBeianQuery`（service.go） |
| `/terraform/v1/mgmt/beian/update` | — | body token | — | — | `handleMgmtBeianUpdate`（service.go） |
| `/terraform/v1/mgmt/bilibili` | — | body token | — | `api-auth-architecture.md`, `bilibili-tutorials-cache.md` | `handleMgmtBilibili`（service.go） |
| `/terraform/v1/mgmt/callback/query` | — | body token | — | — | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/callback/test` | — | body token | Sends one synthetic event through the real path, so "is the target reachable and does it accept our payload" is answerable without publishing a stream. | — | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/cert/query` | — | body token | — | — | `handleMgmtCertQuery`（service.go） |
| `/terraform/v1/mgmt/check` | — | — | — | `api-auth-architecture.md` | `handleMgmtCheck`（service.go） |
| `/terraform/v1/mgmt/contact/query` | — | body token | — | — | `handleMgmtContactQuery`（service.go） |
| `/terraform/v1/mgmt/disk/query` | — | body token | reports capacity for the filesystems Oryx writes to | — | `handleMgmtDiskQuery`（disk.go） |
| `/terraform/v1/mgmt/envs` | — | — | — | `api-auth-architecture.md` | `handleMgmtEnvs`（service.go） |
| `/terraform/v1/mgmt/ffmpeg/capabilities` | — | body token | — | `frontend-guide.md` | `handleMgmtFFmpegCapabilities`（ffmpeg-capability.go） |
| `/terraform/v1/mgmt/gpu/query` | — | body token | reports GPU utilization (empty when no GPU is exposed to the container) | — | `handleMgmtGPUQuery`（gpu.go） |
| `/terraform/v1/mgmt/hlsll/query` | — | body token | — | — | `handleMgmtHlsLowLatencyQuery`（service.go） |
| `/terraform/v1/mgmt/hlsll/update` | — | body token | — | — | `handleMgmtHlsLowLatencyUpdate`（service.go） |
| `/terraform/v1/mgmt/hooks/apply` | — | body token | — | — | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/hooks/example` | — | — | — | — | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/hooks/query` | — | body token | — | — | `CallbackWorker.Handle`（callback.go） |
| `/terraform/v1/mgmt/hphls/query` | — | body token | — | — | `handleMgmtNginxHlsQuery`（service.go） |
| `/terraform/v1/mgmt/hphls/update` | — | body token | — | — | `handleMgmtNginxHlsUpdate`（service.go） |
| `/terraform/v1/mgmt/init` | — | — | — | `api-auth-architecture.md`, `local-test.md` | `handleMgmtInit`（service.go） |
| `/terraform/v1/mgmt/letsencrypt` | — | body token | — | — | `handleMgmtLetsEncrypt`（service.go） |
| `/terraform/v1/mgmt/limits/query` | — | body token | — | — | `handleMgmtLimitsQuery`（service.go） |
| `/terraform/v1/mgmt/limits/update` | — | body token | — | — | `handleMgmtLimitsUpdate`（service.go） |
| `/terraform/v1/mgmt/login` | — | — | — | `api-auth-architecture.md`, `local-test.md`, `streams-query-api.md` | `handleMgmtLogin`（service.go） |
| `/terraform/v1/mgmt/openai/query` | — | body token | — | — | `handleMgmtOpenAIQuery`（service.go） |
| `/terraform/v1/mgmt/openai/update` | — | body token | — | — | `handleMgmtOpenAIUpdate`（service.go） |
| `/terraform/v1/mgmt/record/query` | — | body token | reports the DVR state: what is recording now, what finished, and why a recording looks unhealthy | — | `handleMgmtRecordQuery`（record-diag.go） |
| `/terraform/v1/mgmt/redis/info` | — | body token | — | `console-monitoring.md`, `frontend-guide.md` | `handleMgmtRedisInfo`（console-metrics.go） |
| `/terraform/v1/mgmt/redis/query` | calls, usec | body token | reports Redis/Valkey health: INFO summary, per-command stats and the slow log, so operators can see it without shelling into the container | — | `handleMgmtRedisQuery`（service.go） |
| `/terraform/v1/mgmt/rtc/query` | — | body token | reports the WebRTC configuration and live peers | — | `handleMgmtRtcQuery`（rtc-diag.go） |
| `/terraform/v1/mgmt/secret/query` | — | body token | — | — | `handleMgmtSecretQuery`（service.go） |
| `/terraform/v1/mgmt/srs/capabilities` | — | body token | exposes an API to verify whether the current SRS binary supports the features Oryx depends on | `version-and-upgrade.md` | `handleSrsCapabilities`（srs-capability.go） |
| `/terraform/v1/mgmt/srs/health/query` | — | ? | serves the 診斷 card | — | `handleMgmtSrsHealthQuery`（srs-health.go） |
| `/terraform/v1/mgmt/srs/hls/query` | — | body token | — | — | `handleMgmtSrsHlsQuery`（service.go） |
| `/terraform/v1/mgmt/srs/hls/update` | — | body token | toggles the vhost HLS master switch | — | `handleMgmtSrsHlsUpdate`（service.go） |
| `/terraform/v1/mgmt/srs/latency/query` | — | body token | — | — | `handleMgmtSrsLatencyQuery`（service.go） |
| `/terraform/v1/mgmt/srs/latency/update` | — | body token | sets the vhost latency baseline: "compat" (default, stable throughput) or "low" (min_latency + tcp_nodelay on) | `change-history.md` | `handleMgmtSrsLatencyUpdate`（service.go） |
| `/terraform/v1/mgmt/ssl` | — | body token | — | — | `handleMgmtSsl`（service.go） |
| `/terraform/v1/mgmt/status` | — | body token | — | `api-auth-architecture.md` | `handleMgmtStatus`（service.go） |
| `/terraform/v1/mgmt/streams/events/clear` | — | body token | clears the bounded publish/unpublish event log | — | `handleMgmtStreamsEventsClear`（service.go） |
| `/terraform/v1/mgmt/streams/fps` | — | body token | — | `frontend-guide.md`, `streams-query-api.md` | `handleMgmtStreamsFPS`（service.go） |
| `/terraform/v1/mgmt/streams/kickoff` | — | body token | — | `streams-query-api.md` | `handleMgmtStreamsKickoff`（service.go） |
| `/terraform/v1/mgmt/streams/query` | — | body token | — | `streams-query-api.md` | `handleMgmtStreamsQuery`（service.go） |
| `/terraform/v1/mgmt/token` | — | body token | — | `api-auth-architecture.md` | `handleMgmtToken`（service.go） |
| `/terraform/v1/mgmt/tutorials` | — | body token | — | `api-auth-architecture.md`, `bilibili-tutorials-cache.md` | `handleMgmtTutorials`（service.go） |
| `/terraform/v1/mgmt/versions` | — | — | — | `local-build.md` | `handleMgmtVersions`（service.go） |

## releases（1）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/releases` | — | ? | For version management. | — | `handleHTTPService`（service.go） |

## tencent（2）

| 端點 | 動作 | 認證 | 說明 | 文件 | 實作 |
|---|---|---|---|---|---|
| `/terraform/v1/tencent/cam/secret` | on_play | body token | See https://console.cloud.tencent.com/cam | — | `handleHooksService`（srs-hooks.go） |
| `/terraform/v1/tencent/versions` | on_play | — | — | — | `handleHooksService`（srs-hooks.go） |

---

## 還缺什麼

- 這份索引只回答「有哪些端點、怎麼認證、有哪些動作」。
- 完整 schema（請求／回應欄位）尚未產生；核心端點見各 `docs/*.md` 與程式內 `ParseBody` 結構。
- 端點若在程式裡改名或新增而沒有重新產生本檔，`--check` 會讓 CI 失敗。
