# 功能列表

Oryx 是一體化、開箱即用的開源影片解決方案，支援直播與 WebRTC，可用於雲端或自架。

> 本列表從上游 README 的 Features 拆出，持續維護。

## 已實現功能

- [x] 管理介面：支援身分認證與自動更新。
- [x] Docker 內運行 SRS，透過 Docker 與 SRS API 查詢狀態。
- [x] 推流：RTMP / WebRTC / SRT；播放：RTMP / HTTP-FLV / HLS / WebRTC / SRT。
- [x] SRS 容器使用 `json-file` 日誌並輪替。
- [x] 高解析度、低延遲（200~500ms）SRT 直播。
- [x] SRS hooks 容器內回呼（callback）。
- [x] Redis 使用隨機密碼。
- [x] 騰訊雲 VoD 整合。
- [x] 多平台轉播（restreaming）。
- [x] WordPress 外掛：SrsPlayer。
- [x] aaPanel / 寶塔面板安裝。
- [x] DVR 錄製到本機磁碟。
- [x] 手動升級到最新版。
- [x] Let's Encrypt（LEGO）自動 HTTPS。
- [x] 虛擬直播（將檔案或其他資源轉成直播流）。
- [x] 自架 HLS CDN，可服務 10k+ 觀看者。
- [x] Typecho 外掛：Typecho-Plugin-SrsPlayer。
- [x] DVR 錄製到騰訊雲儲存。
- [x] 拉取 RTSP 攝影機，轉推到 YouTube / Twitch / Facebook。
- [x] FFmpeg 直播轉碼（見 [#2869](https://github.com/ossrs/srs/issues/2869)）。
- [x] AI 字幕（語音轉文字，transcription）。
- [x] 直播間 AI 助手。
- [x] 多語言影片翻譯（dubbing）。
- [x] 影片 OCR 識別。

## 規劃中功能

- [ ] 限制串流時長以控制費用。
- [ ] SRS 5.0 容器支援 GB28181。
- [ ] WebRTC 面對面聊天（見 [#2857](https://github.com/ossrs/srs/issues/2857)）。
- [ ] WebRTC 視訊聊天室（見 [#2924](https://github.com/ossrs/srs/issues/2924)）。
- [ ] 開發者工具組（見 [#2891](https://github.com/ossrs/srs/issues/2891)）。
- [ ] 集中收集 mgmt 與容器日誌。
- [ ] 停止、重啟、升級容器。
- [ ] logrotate 管理日誌。
- [ ] Prometheus API 增加認證。
- [ ] 整合 Prometheus 與 node-exporter。
