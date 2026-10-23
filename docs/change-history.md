# Oryx 變更歷史

> 本檔由 changelogkit 管理（見 [變更歷史說明](changelog.md)）。新增請用 `changelogkit add`（或 `changelogkit serve` 的網頁 GUI）。

## 2026.10.23 08:19 v5.21.6 手動結束改回正常停止流程

- end 到期後不再強踢 SRS client，交由 FFmpeg 正常退出與 SRS on_unpublish 清理。
- 僅刪除目標（目標已不存在）才主動斷開連線。

**類型**: 發佈

### 修改 - v5.21.6

- 轉推：手動結束改回正常停止（不做強殺）

---

## 2026.10.23 08:12 v5.21.5 修正轉推心跳誤殺、全域補強轉碼與 SRS 殘留清單

- 修正 fanout 被 2 分鐘無更新心跳誤殺：來源短暫中斷不再斷開下游。
- 補強轉碼改為全域（program 只轉一次），所有目標 -c copy；設定新增全域開關（預設開）。
- 刪除目標或手動結束時，主動踢掉 SRS 對應 client，避免殘留串流與客戶端。
- 控制台→診斷新增串流生命週期（publish/unpublish）事件，時間轉本地。

**類型**: 發佈

### 修改 - v5.21.5

- 轉推：修正心跳誤殺、全域補強轉碼、SRS 連線清理

---

## 2026.10.23 07:56 v5.21.4 轉播補強轉碼改為全域、串流生命週期可在前端診斷

- 補強轉碼（normalize）移到共享 program：只轉碼一次，所有目標 -c copy；有目標才轉（A1）。
- 設定新增「對共享節目源做補強轉碼」全域開關（預設開），移除每目標的補強勾選。
- 修正 fanout 被 2 分鐘無更新心跳誤殺（改為不啟用），來源短暫中斷不再斷開下游。
- SRS 的 on_publish/on_unpublish/on_play 寫入有界事件清單，控制台「診斷」可查看（時間轉本地）。
- 串流查詢 API 回應新增 events。

**類型**: 發佈

### 修改 - v5.21.4

- 診斷：串流生命週期事件（publish/unpublish）可在前端查看
- 轉推：補強轉碼改為全域（program 只轉碼一次）

---

## 2026.10.23 02:41 v5.21.3 轉播手動結束、可調編碼與診斷強化

- 轉播每個目標可手動「結束直播」：顯示結束文字（含時鐘），60 秒內可取消，之後自動停止。
- 新增可調參數：輸出碼率、CQ 畫質、NVENC/x264 編碼預設（下拉）；GOP 依來源 fps 計算。
- 控制台新增「診斷」分頁（GPU＋轉播＋SRS 串流）；設定新增 GPU 面板（含程序）。
- 記憶體顯示已用/總量、負載標明 1/5/15 分、連線數標為自啟動累計。
- 前端：網路斷線改為提示卡與全域橫幅，不再整頁錯誤。
- 工具：新增畫質對比（錄 20 秒＋多幀拼貼＋PSNR/SSIM，支援 -Source）、gpu-who；quick-test -Keep 保留來源。
- 安全：遮蔽輸出 URL 與 start 事件的串流密鑰。

**類型**: 發佈

### 修改 - v5.21.3

- 工具：畫質對比、quick-test 保留來源、gpu-who
- 控制台/設定：診斷分頁、GPU 面板、結束直播按鈕
- 轉推：手動結束直播、可調碼率/畫質/編碼預設
- VSCode Go配置文件
- markdown語法問題修正改進-2
- markdown語法問題修正-1
- md40fix markdown語法問題改進
- md40 -md60 markdownlint語法改進

---

## 2026.10.21 18:53 v5.21.2 轉播診斷、GPU 面板與前端韌性

- 新增控制台「診斷」分頁：GPU（型號/使用率/顯存/程序，10 秒更新）＋轉播目標＋SRS 串流。
- 新增 GPU 查詢 API 與 scripts/tools/gpu-who.ps1（查顯存占用者）。
- 轉推：遮蔽輸出 URL 路徑中的串流密鑰；program-not-ready 不再計為失敗；source-back 事件。
- 前端：網路斷線改為提示卡／全域橫幅，不再整頁錯誤；記憶體顯示已用/總量、負載標明 1/5/15 分、連線數標為累計。
- CI：安靜閘門 10 分鐘、concurrency、Go 測試失敗標註。

**類型**: 發佈

### 修改 - v5.21.2

- 控制台：分開 SRS 程序已用記憶體與主機總量語意
- 控制台：修正記憶體已用/總量計算（mem_ram_kbyte 是總量）
- 控制台：記憶體顯示已用/總量，負載標明 1/5/15 分鐘
- 控制台：連線數標籤改為「自啟動累計」，避免誤讀為目前連線
- 前端：全域離線橫幅（瀏覽器斷線時顯示，恢復自動消失）
- 前端：網路斷線只顯示提示卡，不再整頁錯誤
- 診斷/設定 GPU：每 10 秒自動更新
- 控制台：新增「診斷」分頁（GPU＋轉播＋SRS 串流集中一頁）
- 診斷：GPU 程序清單與 gpu-who 查詢腳本
- 診斷：新增 GPU 使用率（設定→磁碟頁的 GPU 面板）
- 轉推：遮蔽輸出 URL 的路徑密鑰、修正 program-not-ready 包裝
- CI：安靜閘門 30 分鐘調整為 10 分鐘
- CI：main/release push 加入 30 分鐘安靜閘門
- 轉推：來源尺寸先上限檢查再轉 int32（修正 CodeQL 警告）
- UI 測試：SrsConsole 補 mock axios.post
- CI：test.yml 與 pullrequest.yml 加上 concurrency（新 push 取消舊 run）
- 修正單元測試並讓 CI 標註失敗測試
- CI：移除 test.yml 的 check-pr-test（main/tag 必定逾時）

---

## 2026.10.20 19:16 v5.21.1 轉推事件與等待修正

- 失敗後恢復事件改名 source-back，避免與 offline-stop 同時出現時誤判。
- program 尚未發布視為短暫等待（500ms 重試），不計失敗/重啟/退避；維持「有流才送」。

**類型**: 發佈

### 修改 - v5.21.1

- 轉推：program 尚未發布屬於短暫等待，不計失敗
- 轉推事件釐清：source-back 與 wait program 訊息

---

## 2026.10.18 10:05 v5.21.0 轉播控制台、延遲量測、快速測試與多項修正

- 控制台新增「轉播」分頁：逐目標啟停、狀態/編碼器/FPS、一鍵看畫面（本機目標）與詳情（指標/事件/日誌/FFmpeg 參數）。
- 全域總開關：設定頁入口 + 導覽列關閉時紅色「轉播已暫停」提示。
- 本機轉推目標改用乾淨 stream 名（fanout 自動補 ?secret），延遲量測約 0.36s。
- 新增 forward-latency.ps1 轉推延遲量測工具（中位數，排除平台端）。
- 快速測試工具：-Compare 對照 copy/normalize、狀態顏色、TEST 標記、預設 ffplay 預覽、-NoPlay、中文 UTF-8 BOM。
- 修正控制台「看畫面」播放器參數、詳情深色模式可讀與空狀態、備援畫面改用 CJK 字型。
- local-deploy 改用 --force-recreate，確保同 tag 重建映像會換容器。

**類型**: 發佈

### 修改 - v5.21.0

- 備援畫面：優先使用 CJK 字型，修正中文顯示成方框
- 快速測試：UTF-8 BOM 與 console 編碼，修正中文亂碼
- 文件：釐清「快樂路徑」= 正常流程（非全部情境）
- 文件：更新快速測試的 -Play/-NoPlay 說明（預設開啟預覽）
- 快速測試：-Play 改回 switch
- 控制台「看畫面」修正播放器參數
- 控制台詳情：深色模式可讀、空狀態有提示
- 控制台詳情：缺少日誌時不再顯示空的「—」列，並補上內距樣式
- 控制台轉播頁：可直接看畫面與展開診斷
- 快速測試：預設開啟 ffplay 預覽（-NoPlay 可關）
- 快速測試：修正誤導的 UI 提示，明確 -Play 與 -Keep
- 快速測試工具：狀態顏色提示、-Compare 對照（copy vs normalize）、TEST 標記
- 轉推狀態顯示來源 fps，可對照 normalize 的輸出
- 本機轉推目標改用乾淨 stream 名，延遲量測可用
- 測試用轉播目標：標記 TEST 並清理殘留
- 控制台轉播頁：「啟用」欄改為顏色徽章（已啟用=綠、已停用=灰）
- 控制台轉播頁修正與部署工具修正
- 轉播控制：總開關入口/警告、控制台轉播頁（逐目標快速啟停）
- 轉推延遲工具：固定來源、檢查總開關、修正取樣
- 新增轉推延遲量測工具 forward-latency.ps1
- 「來源離線自動停止」預設改為開啟

---

## 2026.10.18 02:43 v5.20.0 轉推編碼與 A/V 同步修正：nvenc 探測修正、composite 基底改用 avg fps、GOP 依輸出 fps（可設定關鍵幀間隔）、顯示 FFmpeg 參數；備援畫面時區/說明文字/離線停止

**類型**: 發佈

### 修改 - v5.20.0

- 新增「關鍵幀間隔（秒）」設定（預設 2 秒）
- 轉推 GOP 改為「2 秒 × 輸出 fps」，不再固定 -g 50
- 補強轉碼旁顯示「將使用的參數」靜態預覽
- 修正 FFmpeg 編碼能力探測：nvenc 用 256x256（64x64 低於 NVENC 最小尺寸會誤判）
- 轉推 composite 的 A/V 漂移：基底幀率改用來源 avg_frame_rate
- bump日誌腳本更新markdownlint問題修正 日誌歷史修正
- 轉推診斷顯示實際 FFmpeg 參數（secret 遮蔽）
- 轉播備援畫面：時區、可自訂說明文字、離線自動停止（預設關閉）
- 修正轉播總開關「暫停」不會立即停止正在跑的轉播

---

## 2026.10.17 08:45 v5.19.0 導覽列常駐轉播狀態＋推流診斷入口；轉播總開關（暫停/恢復，不改各目標啟用）；多平台轉播頁體驗改善（不自動展開、批次列）；轉播狀態顯示編碼器；修正 slate 整合測試與轉播頁 TDZ

**類型**: 發佈

### 修改

- 總開關文案改清楚：恢復/暫停（不會自動開啟其他目標）＋提示
- 修正多平台轉播頁的 TDZ：forward/enabled 查詢 effect 移到 handleError 之後
- 修正轉推 slate 整合測試：composite 模式的斷言
- 導覽列常駐「轉播狀態」＋推流診斷入口
- 轉推總開關（後端全域旗標）：暫停/恢復全部轉播，不改各目標的啟用偏好
- 多平台轉播頁：不再自動展開第一組；新增批次列（全部展開/收合、全部開始/停止）
- local-deploy -Check：與源碼修訂比對，明確指出 build target 是否落後源碼
- 轉推狀態顯示編碼器（GPU/軟體），並修正 zh 的 forward 文案誤用繁中

---

## 2026.10.17 07:29 v5.18.0 轉推備用畫面改單一 composite（來源中斷端無縫）＋ GPU 編碼器；繁體中文語系；勾選框修正與限流文案澄清

**類型**: 發佈

### 修改 - 轉推 slate 改用單一 composite（來源中斷端無縫）

- 修正markdownlint提示問題 DEVELOPER文件
- 轉推 slate 改用單一 composite（來源中斷端無縫）
- 新增 zmqsend（ZMQ PUB 發送端）並編進映像，供 program 合成器執行期切畫面
- 轉推轉碼改用 GPU 編碼器（NVENC 可用時），並還原 C2
- C 實驗：換手不重啟 fanout 會卡死 → 證實必須重啟（附 handoff 記錄）
- 快速測試工具：ffplay 預覽加 reconnect（跨轉推換手空窗，才看得到備用畫面）
- 轉推備用畫面依來源解析度/幀率產生（避免切換時解析度突變）
- 快速測試工具修 ffplay 預覽：視窗標題含空格導致秒退
- 快速測試工具新增 -Slate：示範來源中斷切備用畫面（深灰底＋跳動時鐘）
- 轉推備用畫面改為「深灰底＋跳動時鐘」（讓觀眾知道還在直播）
- 繁中：修正語言判定把 zh-TW 當非中文（教程等 fallback 成英文）
- 繁中語系：補術語覆寫（碼率/碼流/鑑權/地址/配置）
- 新增繁體中文語系（OpenCC s2twp 自動產生）
- 統一修正勾選框為受控（掃過其餘頁面）
- 修正設定頁勾選框狀態誤判（非受控 defaultChecked）
- 澄清「限流」文案：實為來源碼率上限（vLive／IP 攝像頭）
- 改進發佈變更歷史：自動帶上本版提交清單 + 補齊 v5.17.0 條目

---

## 2026.10.17 04:18 v5.17.0 設定/診斷強化：延遲模式、HLS 總開關、磁碟診斷、本機部署

**類型**: 功能 · **檔案**: `platform/utils.go`、`platform/service.go`、`platform/disk.go`、`ui/src/pages/Settings.js`

### 修改 - 新增 SRS 延遲模式（`min_latency`/`tcp_nodelay`）設定

- 新增 SRS 延遲模式（`min_latency`/`tcp_nodelay`）設定。
- 設定頁：延遲模式選擇 + 澄清 HLS 選項說明。
- 新增 HLS 總開關與磁碟容量診斷。
- 設定頁：HLS 總開關 + 磁碟空間分頁。
- HLS 停用提示 + 磁碟診斷回報多檔案系統。
- 磁碟診斷：虛擬磁碟（WSL VHDX）改以實際可用呈現，避免誤導。
- 引入 changelogkit 管理改動歷史（並串到 `-Release`）。
- CI：doc/工具改動不建映像＋取消被取代的建置（省資源）。
- 新增本機部署工具 `local-deploy.ps1`（含記住 compose 目錄的 ini）。

---

## 2026.10.17 v5.16.0 發佈（版號模型重做 + 一鍵發佈）

**類型**: 功能 · **檔案**: `platform/version.go`、`scripts/bump-version.ps1`、`.github/workflows/docker-publish.yml`

### 問題 - 每次 push 都像發佈，版號卻不動

push main 會出 `:latest` 與 `:<version>`，但版號（繼承自上游的 `v5.15.20`）從未遞增，同一 tag 被覆蓋 → 等於沒有版本。

### 修改 - main=開發映像、tag=正式 release

- `docker-publish.yml`：push main 只出 `:sha-<commit>`、`:edge`；打 `v*` tag 才出 `:vX.Y.Z` 與 `:latest`；tag 建置時驗證 `platform/version.go == tag`。
- `bump-version.ps1 -Release`：一鍵 bump + commit + tag + push（前置檢查：在 main、工作區乾淨、tag 不存在）。
- 本次發佈 `v5.16.0`。

**相關文件**:

- [版本體系與升級指南](version-and-upgrade.md)
- [Docker 使用說明](docker-usage.md)

---

## 2026.10.17 設定頁分類重整 + SRS 延遲模式 + 澄清 HLS 選項

**類型**: 功能 · **檔案**: `ui/src/pages/Settings.js`、`platform/service.go`、`platform/utils.go`

### 問題 - 設定頁意圖不明、HLS 標籤誤導

設定頁把帳號／外觀／整合／SRS／運行時混在同一排；「開啟高性能分發 HLS」其實只是 SRS `hls_ctx`，且本分支已無 nginx。

### 修改 - 左側分組導覽 + 延遲模式

- 設定頁改為左側分組導覽（帳號與安全／串流引擎 SRS／集成／站點）。
- 新增 `POST /terraform/v1/mgmt/srs/latency/update`（`compat`/`low`）：把 `min_latency`/`tcp_nodelay` 產生進 `srs.vhost.conf` 並 reload SRS（免重啟）。
- 澄清 HLS 選項文案、補上說明與「SRS 會自動重新載入」提示。

---

## 2026.10.17 轉推：快速測試工具組 + 補強轉碼（normalize）

**類型**: 功能 · **檔案**: `scripts/tools/forward-quick-test.ps1`、`scripts/tools/local-deploy.ps1`、`platform/forward.go`

### 問題 - 不知從何測起、來源掉幀/VFR 無法補強

### 修改 - `forward-quick-test` 快速檢查工具

- `forward-quick-test.ps1`：一鍵起本機來源 → 轉推到本機目標 → 印品質指標與事件；`-Outage`／`-Play`／`-Normalize`。
- 每個轉推目標可選 `normalize`：以 `-fps_mode cfr` 轉碼把來源掉幀／VFR／抖動正規化為固定幀率。
- `local-deploy.ps1`：建置 + 回報「目前跑哪個 build」+ 一鍵重建。

---

## 2026.10.17 轉推：品質指標、診斷時間線與恢復修正

**類型**: 修正 · **檔案**: `platform/forward.go`、`platform/forward-program.go`、`platform/forward-metrics.go`

### 問題 - 來源中斷後下游永久凍結

FFmpeg 的 HTTP-FLV `-reconnect` 在串流被取消發布再重新發布後不會恢復（隔離實驗證實），fanout 卡在失敗的重連迴圈。

### 根因 - 「靠 reconnect 撐住」的前提不成立

### 修改 - 恢復時重啟

- 來源恢復時重啟 relay、program 換 publisher 時重啟 fanout（下游只重連一次，不再永凍）。
- 新增品質指標（輸出 FPS／速度／時鐘漂移／來源重連）與診斷時間線、近期日誌；UI 顯示趨勢。

---

## 2026.10.16 轉推：程式總線（program bus）+ 來源中斷保持

**類型**: 功能 · **檔案**: `platform/forward-program.go`

### 問題 - 來源一抖動就殺 ffmpeg → 下游斷線

### 修改 - 以 SRS 程式總線解耦下游與來源

- 每個來源一個 `__forward_<app>_<stream>` 程式流，由 relay 持續餵；fanout 只讀程式流。
- 來源中斷時下游凍結（hold）或切備用畫面（slate），不立即斷線。

**相關文件**:

- [轉推程式總線設計](forward-program-bus.md)

---

## 2026.10.16 CI：修正測試輸入下載與失敗分類；本機部署工具

**類型**: 修正 · **檔案**: `.github/workflows/pullrequest.yml`、`scripts/tools/github-test-annotations.sh`

### 修改 - CI 失敗分類：逐測試錯誤、根因提示、噪音過濾

- 測試輸入改用 `develop/doc/source.flv`（原路徑是 symlink）+ `curl --fail` + 大小驗證。
- CI 失敗分類：逐測試錯誤、根因提示、噪音過濾。
- Go 快取：vendored repo 改用 build cache（`cache: false` + 顯式 `~/.cache/go-build`）。
- doc/工具類改動不建映像（`paths-ignore`）+ 取消被取代的建置（`concurrency`）。

---
