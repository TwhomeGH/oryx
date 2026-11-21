# TODO

本專案的待辦與已完成事項。**單一入口**：計畫中的工作與已完成的工作都記在這裡，避免散落在 issue、對話或 commit 訊息裡。

- 目前版本：`v5.22.12`（`platform/version.go`）
- 發版方式：`pwsh scripts/bump-version.ps1 -Release`（未給 `-Notes` 時自動以本批提交產生條目）
- 本地建置／部署：`pwsh scripts/tools/local-deploy.ps1 -Deploy`（見 [docs/local-build.md](docs/local-build.md)）

## 狀態標記

待辦一律用 GitHub 可辨識的任務清單：`- [ ]` 未完成、`- [x]` 完成。
需要更細的分類時，在標題前加括號註記：

| 註記 | 意義 |
| --- | --- |
| （進行中） | 部分完成，仍有明確缺口 |
| （凍結） | 存在但不投入，只修「影響建置或共用模組」的問題 |
| （已知限制） | 目前不改動，原因寫在該項說明 |

---

## 待辦

### 推流診斷頁（/players/pushdiag.html）

- [x] 圖表統一時間軸 — 四張圖改用「分析開始後的秒數」為共同 x 軸、共用 60 秒滑動視窗與秒制刻度；WebRTC 分頁的三張圖（RTT／位元率／jitter）也接上同一套時間基準（各自以 session 起點計時）；GOP 以平行時間陣列保存，統計與既有測試不受影響
- [x] 頁面拆檔 — 144KB → HTML 22.4KB＋`pushdiag.css`＋`js/pushdiag.js`(120KB)
- [x] 靜態資源版本化 — HTML no-cache ＋ 資源網址帶 `?v=<建置 revision>`（CI 傳 github.sha、本機傳 git HEAD、未帶參數則 `dev-<timestamp>`）；css/js 維持 30 天快取，換 build 即失效，附守門測試
- [x] 轉播整合測試偶發失敗 — 原因是來源只推 9 秒、而冷啟動綁定可能更久，來源先消失導致 target 永遠到不了 live；改為來源存活至測試主動 stop，並先確認綁到本測試的 stream
- [x] 診斷頁深層連結 — 支援 `?app=&stream=[&autostart=1]`：預填並切到手動輸入（避免被「跟隨來源」覆蓋）；開始分析後把來源寫回網址，方便分享
- [x] DTS 倒退時間軸標記 — 影片 DTS 倒退與音訊 PTS 回退各以標記畫在封包圖的共用時間軸上（頂端三角／菱形），不再只有累計次數
- [x] 音訊時鐘漂移歷史 — 新增「音訊時鐘漂移 (ppm，近 30 秒)」圖，每 5 秒取樣、共用同一時間軸；樣本不足時不給值

### 轉播（forward）

- [ ] 交付延遲趨勢圖 — 把已量測的 `fanoutLagSec`（節目源→各目標）寫進管線歷史樣本，在「管線歷史」旁多一張趨勢圖；目前只有即時值，看不出延遲是否在惡化
- [ ] 轉播告警 — fallback 持續中、degraded、offline-stop 目前只有畫面可見，沒有任何主動通知；沿用既有 callback 基礎設施寫事件，再選擇性推瀏覽器通知
- [ ] 目標健康度細分 — 目前只有 `restartCount`／`lastError`，缺「平台端拒絕」（handshake 失敗、401、key 失效）的分類與計數
- [ ] 每個目標獨立的結束畫面 — 目前結束畫面掛在共享節目源，任何目標結束都會讓所有目標看到同一段文字；真正做到「每目標不同」需放棄共用節目源（成本 ×N），屬架構取捨
- [ ] 管線歷史延長保留 — Valkey 只保留 1 小時（720 筆 × 5s）；更長需寫 JSONL 或外部 TSDB
- [ ] （進行中）自動源選擇 — 已排除所有目標輸出；仍可能在多個來源間選錯，工具需明確指定
- [ ] （已知限制）結束直播的重建空窗 — 換字必須重建節目源，結束瞬間下游約 5 秒空窗；這是本鏡像 ffmpeg 沒有可用的執行期改字通道所致
- [ ] （已知限制）slate 與 hold 的取捨 — `hold` 零空窗但凍結畫面；`slate` 有 2–5 秒換手空窗。見 [docs/forward-program-bus.md](docs/forward-program-bus.md)
- [ ] 評估單一目標時略過節目源 — 目前每個目標都經節目源轉一手（copy 模式實測約 0.4% CPU／43MB），換取來源中斷時下游不受影響
- [x] 結束直播顯示結束文字 — 結束文字改畫在 overlay 之後的頂層，來源還活著也看得到；composite 以重建節目源換字（zmq 在本鏡像不可用），hold 無法渲染故立即停止；寬限預設 60s→30s 並一次性遷移
- [x] 節目源遮蔽下游監測的補償 — 狀態新增 `fallbackSeconds`／`offlineStopMinutes`；navbar 徽章顯示「來源中斷 Ns」與說明；事件時間線記錄 `source-outage` 與 `source-back`（實測中斷 11s）。取樣必須放在隨 run 啟動的 supervisor goroutine，因為 `doForward` 會阻塞整個 run，寫在迴圈邊界在執行中不會跑到
- [x] 漂移語意拆解 — 時間軸速率（media 秒／牆上秒，1.000 健康）與每小時偏移，另保留「領先實際時間」並註明不是延遲；需 30 秒取樣才給值
- [x] 節目源轉碼指標 — 解析 publisher 的 `-progress`：fps、實際輸出幀率、speed、碼率、首格耗時，補上先前完全看不到的合成管道成本
- [x] 節目源時間軸計畫可見 — 顯示 copy／repair／cfr／composite 與原因代碼，並揭露來源 CFR/VFR 判定
- [x] 輸出幀率可選 — 只影響會重新編碼的路徑（備援畫面、強制 CFR）；跟隨來源或固定 23.976–120／自訂，高於來源會夾回
- [x] CFR/VFR 判定改實測 — 取樣 3 秒幀間隔判定，不再信任 `r_frame_rate`／`avg_frame_rate`；介面附抖動或最大間隔作為依據
- [x] 備援模式的節奏代價提示 — 選 `slate` 時當場說明會固定幀率重新編碼、VFR 會變 CFR

### 診斷與監控

- [ ] 磁碟 I/O 壓力 — cgroup `io.stat` 可取得，屬第二線指標
- [ ] Redis 慢查詢面板 — 已有 SLOWLOG 摘要，尚未逐筆展開
- [ ] WebRTC 連線診斷 — 目前只有播放器頁；ICE／DTLS 失敗與連線數無從觀察
- [ ] `streams/fps` 探測結果持久化 — 目前只在前端保存，重整頁面即失去上一筆成功值與失敗時間
- [ ] （進行中）平台回呼（callback）驗證診斷 — 送出端在我們手上：記錄每次回呼的目標、action、HTTP 狀態、延遲、失敗原因與最近 N 筆，並提供一鍵測試
- [ ] （已知限制）WSL GPU 編碼器利用率 — WSL 不回報 `utilization.encoder`，介面已標註 0% 代表未知
- [ ] （已知限制）錄影必須開啟 HLS — 錄影由 SRS `on_hls` 分段驅動；HLS 關閉時不會產生任何檔案，診斷卡會明示
- [ ] （已知限制）錄影收尾有延遲 — worker 在最後分段後仍保留 30s（正式 300s）才收尾，期間顯示「等待收尾」屬正常
- [x] SRS 健康計數（publish timeout／kickoff／queue overflow） — SRS 改寫檔案日誌 `/data/logs/srs.log` 並鏡射到 stdout（docker logs 不變）；診斷卡掃描最近 256KB，統計錯誤樣式與四類關鍵失敗
- [x] SRS 檔案日誌輪替 — 實測 SRS **不會**自行輪替（預塞 31MB 後重啟仍繼續 append，fd 為 O_RDWR|O_APPEND）；改由平台在查詢前與每 10 分鐘檢查，超過 16MB 保留最新 4MB 截斷，實測 31MB → 4MB
- [x] 串流生命週期「事件→原因→影響」 — 發布者 IP／client、session 時長、受影響的轉播目標、平台踢除原因；播放事件分開存放
- [x] 影片流無資料／無影格警示 — `video_frames` 與 `recv_bytes` 雙軌判定，區分「有資料沒畫面」與「完全沒資料」
- [x] 影片流每流詳情 — 發布者、累計影格／流量、來源 tcUrl（遮蔽 secret）、消費者清單
- [x] 錄影（DVR）狀態卡 — 錄影中／等待收尾／疑似中斷／已完成／檔案不存在，含分段數、大小、時長
- [x] 幀率探測失敗回報 — 改為 200 + reason（timeout/no-frames/failed），介面顯示原因並保留舊值標記
- [x] 轉播執行紀錄持久化 — 最近 10 場寫入 `SRS_FORWARD_SESSIONS:<platform>`

### 設定頁

- [ ] （進行中）SRS 選項重新分組 — 已把 GPU 從磁碟獨立；其餘（HLS／延遲／流管理）仍待整理
- [x] GPU 獨立分頁 — 設定 → GPU

### 工具與 CI

- [ ] 品質比對自動對齊 — 目前仍需手動掃描偏移；循環來源會讓自動搜尋失準
- [x] PowerShell 說明文件檢查 — `scripts/tools/check-ps1-docs.ps1`，PR 時執行
- [x] CI 失敗分類標註 — Go／整合測試失敗會輸出 `::error` 註解與 Job Summary
- [x] API 參考產生器 — `scripts/tools/gen-api-reference.py` 掃描 handler 註冊點，產生 `docs/api-reference.md`（148 端點：動作、認證、既有文件連結、實作位置）；PR CI 以 `--check` 驗證是否過期
- [x] OpenAPI 規格與瀏覽頁 — `scripts/tools/gen-openapi.py` 以真實回應推導 8 個維運端點的 schema（forward／streams／record／disk／GPU／SRS 健康／版本），輸出 `platform/containers/www/api/openapi.json`，執行中實例在 `/openapi.json`；`/players/api.html` 提供搜尋、欄位表、範例與 curl；CI 驗證規格與程式一致

---

## 凍結（存在但不投入）

這些是上游 Oryx 的示範場景。它們不是壞掉，而是**從來沒有被我們的實際用法驅動過**：最後一次功能改動停在 2026-08-29（AI 後端）與更早的 UI，之後只有全域重構的機械替換掃到（例如 2026-11-08 的斷線提示改動）。

界線：**只修「會讓建置失敗或影響共用模組」的問題**（lint、路由、i18n、共用 hook）；不新增功能、不補診斷、不因使用者回報的單點問題擴大投入。

- [ ] （凍結）vLive（虛擬直播） — 把本地影片 loop 成 RTMP；OBS 的媒體源已直接取代，且真正的需求（多平台轉播）由 forward 承擔
- [ ] （凍結）直播間（Live Room） — 載體是「標題＋串流金鑰」，價值全在內嵌 AI 助播（Whisper＋LLM＋TTS）；AI 未設定時只剩空殼。若要重做，應是「可分享的觀看頁＋觀眾統計」，不是 AI 外殼（見下方決策）
- [ ] （凍結）轉碼（Transcode） — 把 ffmpeg 指令包成 UI；forward 的 normalize／encode 參數已覆蓋實際用途
- [ ] （凍結）AI 字幕（Transcript） — 依賴 OpenAI Whisper ASR，需 API key 且按量計費；沒有 key 就是不可用
- [ ] （凍結）配音（Dubbing） — 依賴 OpenAI TTS，同上
- [ ] （凍結）OCR — 依賴外部視覺 API，同上

### 待決：直播間是否重做

- [ ] 若要做，方向是**「分享觀看頁」**：每個串流一組可分享網址＋QR、即時觀眾數、開播狀態、錄影連結，資料來源用既有的 SRS `/api/v1/clients` 與串流生命週期事件；**不接 AI**
- [ ] 若不做，維持凍結，並在場景頁標註「示範場景」

---

## 已完成

### 最近（v5.22.x）

| 版本 | 內容 |
| --- | --- |
| unreleased | `/players`、`/tools` 的 HTML 改為 `no-cache`；導覽列推流診斷連結加快取破壞參數（`ed0ec7b`） |
| unreleased | 推流診斷：GOP 分布統計 p50/p90/p99 + 分桶（`10e7393`） |
| unreleased | 推流診斷：連線摘要列、畫面落後、時鐘漂移 ppm（`c4da62f`） |
| v5.22.8 | **修正錄影完全無法運作**（DVR 路徑相對 cwd，on_hls 全 500）；串流生命週期原因/影響、影片流無資料/無影格警示與每流詳情、錄影狀態卡、幀率探測失敗回報、位元率單位修正 |
| v5.22.7 | 轉播時間軸策略 `normalizeMode`（預設保留 VFR）、時間戳損壞改為重建時間軸、來源原始 PTS/DTS 留存 |
| v5.22.2 | 轉播執行紀錄持久化、診斷日誌擴充（PTS/DTS 樣式）、管線歷史只反映本次執行 |
| v5.22.1 | GPU 診斷擴充（編碼器／解碼器／溫度／功耗）、WSL 提示、GPU 獨立分頁、啟動清空幽靈流 |
| v5.22.0 | 管線歷史圖表、結束直播立即停止、來源時間戳跳躍自動重啟、測試工具安全防護 |
| v5.21.x | 控制台轉播頁、GPU／磁碟／Redis／生命週期診斷、zh-TW 語系、前端離線提示 |

### 更早

| 主題 | 內容 |
| --- | --- |
| 轉播架構 | 節目源（program bus）+ slate/hold 備援；下游只讀節目源，來源中斷不影響已建立連線 |
| 轉碼 | GPU 編碼器選擇（NVENC → AMF → QSV → libx264）、CFR 正規化、GOP 依 keyframeSeconds × fps、音訊沿用來源碼率／取樣率 |
| 測試工具 | `forward-quick-test.ps1`、`forward-latency.ps1`、`forward-quality-compare.ps1`、`forward-drift.ps1`、`gpu-who.ps1` |
| SRS 設定 | 延遲模式（compat／low）、HLS 總開關、磁碟容量診斷 |
| CI | `paths-ignore`、concurrency、quiet gate、Go 測試註解、`actions/cache@v5` |

---

## 已知限制（暫不改動）

| 限制 | 原因 |
| --- | --- |
| SRS 原生 forward 無法改寫 app／stream key | 因此必須保留 Go + FFmpeg 轉推 |
| 同一個 stream 只能有一個發布者 | 換手必然短暫中斷，無法零空窗切換到 slate |
| 孤兒流清理器停用 | SRS `/api/v1/streams` 回報不完整，掃描會誤刪活躍流；改為啟動時清空 + 刪除目標時踢除 |
| 轉播指標解析自 FFmpeg progress 行 | 非直接探測輸出，屬近似值 |
