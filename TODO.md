# TODO

本專案的待辦與已完成事項。**單一入口**：計畫中的工作與已完成的工作都記在這裡，避免散落在 issue、對話或 commit 訊息裡。

- 目前版本：`v5.22.2`（`platform/version.go`）
- 發版方式：`pwsh scripts/bump-version.ps1 -Release -Patch -Notes "..."`
- 本地建置／部署：`pwsh scripts/tools/local-deploy.ps1 -Deploy`（見 [docs/local-build.md](docs/local-build.md)）

## 圖例

| 標記 | 意義 |
|---|---|
| `[ ]` | 未開始 |
| `[~]` | 進行中／部分完成 |
| `[x]` | 已完成 |
| `[!]` | 已知限制，暫不改動 |

---

## 待辦

### 推流診斷頁（/players/pushdiag.html）

| 狀態 | 項目 | 說明 |
|---|---|---|
| `[ ]` | 圖表統一時間軸 | 碼率、封包、GOP、影音同步目前各有獨立 x 軸，難以對齊同一時間點的事件 |
| `[ ]` | 頁面拆檔 | 邏輯內嵌在 121KB HTML；拆成 `js/*.js` 後可用長快取，改版只更新資源 |
| `[ ]` | 靜態資源版本化 | `player.css`／`js/*` 仍是 30 天長快取，改動不會立即反映（目前關鍵樣式改為內嵌） |
| `[ ]` | DTS 倒退時間軸標記 | 目前只計數回退次數，未在時間軸上標出發生位置 |
| `[ ]` | 音訊時鐘漂移歷史 | 目前只顯示即時 ppm，沒有趨勢 |

### 轉播（forward）

| 狀態 | 項目 | 說明 |
|---|---|---|
| `[ ]` | 每個目標獨立的結束畫面 | 目前結束畫面掛在共享節目源，任何目標結束都會讓所有目標看到同一段文字 |
| `[ ]` | 管線歷史延長保留 | Valkey 只保留 1 小時（720 筆 × 5s）；更長需寫 JSONL 或外部 TSDB |
| `[~]` | 自動源選擇 | 已排除所有目標輸出；仍可能在多個來源間選錯，工具需明確指定 |
| `[!]` | slate 與 hold 的取捨 | `hold` 零空窗但凍結畫面；`slate` 有 2–5 秒換手空窗。見 [docs/forward-program-bus.md](docs/forward-program-bus.md) |

### 診斷與監控

| 狀態 | 項目 | 說明 |
|---|---|---|
| `[ ]` | 磁碟 I/O 壓力 | cgroup `io.stat` 可取得，屬第二線指標 |
| `[ ]` | Redis 慢查詢面板 | 已有 SLOWLOG 摘要，尚未逐筆展開 |
| `[x]` | 轉播執行紀錄持久化 | 最近 10 場寫入 `SRS_FORWARD_SESSIONS:<platform>` |
| `[!]` | WSL GPU 編碼器利用率 | WSL 不回報 `utilization.encoder`，介面已標註 0% 代表未知 |

### 設定頁

| 狀態 | 項目 | 說明 |
|---|---|---|
| `[~]` | SRS 選項重新分組 | 已把 GPU 從磁碟獨立；其餘（HLS／延遲／流管理）仍待整理 |
| `[x]` | GPU 獨立分頁 | 設定 → GPU |

### 工具與 CI

| 狀態 | 項目 | 說明 |
|---|---|---|
| `[ ]` | 品質比對自動對齊 | 目前仍需手動掃描偏移；循環來源會讓自動搜尋失準 |
| `[x]` | PowerShell 說明文件檢查 | `scripts/tools/check-ps1-docs.ps1`，PR 時執行 |
| `[x]` | CI 失敗分類標註 | Go／整合測試失敗會輸出 `::error` 註解與 Job Summary |

---

## 已完成

### 最近（v5.22.x）

| 版本 | 內容 |
|---|---|
| unreleased | `/players`、`/tools` 的 HTML 改為 `no-cache`；導覽列推流診斷連結加快取破壞參數（`ed0ec7b`） |
| unreleased | 推流診斷：GOP 分布統計 p50/p90/p99 + 分桶（`10e7393`） |
| unreleased | 推流診斷：連線摘要列、畫面落後、時鐘漂移 ppm（`c4da62f`） |
| v5.22.2 | 轉播執行紀錄持久化、診斷日誌擴充（PTS/DTS 樣式）、管線歷史只反映本次執行 |
| v5.22.1 | GPU 診斷擴充（編碼器／解碼器／溫度／功耗）、WSL 提示、GPU 獨立分頁、啟動清空幽靈流 |
| v5.22.0 | 管線歷史圖表、結束直播立即停止、來源時間戳跳躍自動重啟、測試工具安全防護 |
| v5.21.x | 控制台轉播頁、GPU／磁碟／Redis／生命週期診斷、zh-TW 語系、前端離線提示 |

### 更早

| 主題 | 內容 |
|---|---|
| 轉播架構 | 節目源（program bus）+ slate/hold 備援；下游只讀節目源，來源中斷不影響已建立連線 |
| 轉碼 | GPU 編碼器選擇（NVENC → AMF → QSV → libx264）、CFR 正規化、GOP 依 keyframeSeconds × fps、音訊沿用來源碼率／取樣率 |
| 測試工具 | `forward-quick-test.ps1`、`forward-latency.ps1`、`forward-quality-compare.ps1`、`forward-drift.ps1`、`gpu-who.ps1` |
| SRS 設定 | 延遲模式（compat／low）、HLS 總開關、磁碟容量診斷 |
| CI | `paths-ignore`、concurrency、quiet gate、Go 測試註解、`actions/cache@v5` |

---

## 已知限制（暫不改動）

| 限制 | 原因 |
|---|---|
| SRS 原生 forward 無法改寫 app／stream key | 因此必須保留 Go + FFmpeg 轉推 |
| 同一個 stream 只能有一個發布者 | 換手必然短暫中斷，無法零空窗切換到 slate |
| 孤兒流清理器停用 | SRS `/api/v1/streams` 回報不完整，掃描會誤刪活躍流；改為啟動時清空 + 刪除目標時踢除 |
| 轉播指標解析自 FFmpeg progress 行 | 非直接探測輸出，屬近似值 |
