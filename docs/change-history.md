# Oryx 變更歷史

> 本檔由 changelogkit 管理（見 [變更歷史說明](changelog.md)）。新增請用 `changelogkit add`（或 `changelogkit serve` 的網頁 GUI）。

## 2026.10.17 04:18 設定/診斷強化：延遲模式、HLS 總開關與停用提示、磁碟多檔案系統診斷；本機部署工具與 changelogkit

**類型**: 修復

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

### 修改

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

### 修改

- 測試輸入改用 `develop/doc/source.flv`（原路徑是 symlink）+ `curl --fail` + 大小驗證。
- CI 失敗分類：逐測試錯誤、根因提示、噪音過濾。
- Go 快取：vendored repo 改用 build cache（`cache: false` + 顯式 `~/.cache/go-build`）。
- doc/工具類改動不建映像（`paths-ignore`）+ 取消被取代的建置（`concurrency`）。

---
