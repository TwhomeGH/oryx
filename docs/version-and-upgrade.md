# 版本體系與升級指南

> 說明 Oryx fork 的兩套版本號分別在哪、怎麼升級 SRS 主伺服器核心、以及一鍵版本工具的用法。

---

## 1. 兩套版本，各管各的

| | Oryx 產品版本 | SRS 核心 |
| --- | --- | --- |
| 定義位置 | `platform/version.go` → `const version = "v5.15.20"` | **根目錄 `Dockerfile` 第 4 行** → `FROM ${ARCH}ossrs/srs:5 AS srs` |
| 變更方式 | 手動改（或用 `scripts/bump-version.ps1`） | 改基底映像標籤（或用工具 `-SrsCore` 參數） |
| 消費點 | HTTP Header `Oryx/vX.Y.Z`、mgmt API 的 Version 欄位、UI 顯示、`./releases -v` | 容器內 `/usr/local/srs/objs/srs` 二進位本身 |
| 映像標籤關係 | `ghcr.io/twhomegh/oryx:vX.Y.Z` 的版號來源 | 只影響包進映像裡的 SRS 引擎，不影響對外標籤 |

> 注意：Oryx 的 v5.x 與 SRS 的 v5.x **沒有对应關係**，純粹是兩條獨立的版本線。

### 查目前運行的 SRS 核心版本

```bash
# 方法一：看 Dockerfile（下次建置會用的版本）
grep "AS srs" Dockerfile

# 方法二：問正在跑的容器
docker exec oryx /usr/local/srs/objs/srs -v
```

---

## 2. 升級 SRS 主伺服器核心

上游官方映像目前提供三條版本線（2026-08 查詢）：

| 標籤 | 對應版本 |
| --- | --- |
| `ossrs/srs:5` | v5.0.213（上游原本預設） |
| `ossrs/srs:6` | v6.0.191 |
| `ossrs/srs:7` | v7.0.157（**本 fork 現狀預設**，2026-08 切換） |

### 升級步驟

```powershell
# 1. 一鍵改標籤（也可指定精確版號如 -SrsCore v7.0.157）
.\scripts\bump-version.ps1 -SrsCore 7

# 2. 強烈建議先本地建置驗證（見 docs/local-build.md）
docker build -t oryx:test .
#    用測試 compose 起一次，確認推拉流/HLS/WebRTC 正常

# 3. commit + push 觸發正式建置
git add Dockerfile && git commit -m "chore: upgrade srs core to 7" && git push origin main

# 4. 部署機更新
docker compose pull && docker compose up -d
```

### 升級風險與回退

- **設定檔相容性**：SRS 大版本之間可能淘汰舊指令。Oryx 生成的 conf 在
  `containers/data/config/*.conf`（include 進主設定），升級後看容器日誌有無 config error
- **行為差異**：WebRTC/SRT/HLS 參數語義可能微調，重點回歸你實際用到的協定
- **回退**：把 Dockerfile 標籤改回 `5` 再走一遍流程即可；或用映像回退（docs/rollback.md）
- 建議跳大版本前先看 [SRS releases](https://github.com/ossrs/srs/releases) 的 breaking changes

---

## 2-1. SRS7 主設定性能基線

Oryx 的 SRS 主設定位於 `platform/containers/conf/srs.release.conf`，本地開發與 origin-cluster
還有對應的 `srs.release-local.conf`、`srs.release-mac.conf`、`srs.origin.conf`。這些檔案會在
`vhost __defaultVhost__` 中顯式設定 RTMP/HTTP-FLV 的性能基線：

```conf
min_latency off;
tcp_nodelay off;
publish {
    mr off;
    mr_latency 350;
    firstpkt_timeout 20000;
    normal_timeout 7000;
    parse_sps on;
    try_annexb_first on;
    kickoff_for_idle 0;
}
play {
    gop_cache on;
    gop_cache_max_frames 2500;
    queue_length 10;
    time_jitter full;
    mw_latency 350;
    mw_msgs 8;
}
```

這組不是極限低延遲配置，而是「穩定吞吐＋快速起播」基線：

- `min_latency off`、`tcp_nodelay off`：保留 RTMP/HTTP-FLV 的吞吐取向，避免大量小包增加系統壓力。
- `gop_cache on`：讓播放器或轉播拉流可以快速拿到上一個 GOP，不必等下一個 IDR。
- `queue_length 10`：比 SRS 預設 30 秒更收斂，避免慢客戶端累積過長播放隊列。
- `mw_latency 350`、`mw_msgs 8`：保留 merged-write 對 RTMP/HTTP-FLV 的批量寫入收益。
- `kickoff_for_idle 0`：保持不因沒有播放器而踢掉推流，避免和 forward/錄製等後台工作衝突。

若後續要做低延遲模式，應另做成可配置選項，再評估是否切到 `min_latency on`、
`tcp_nodelay on`、`gop_cache off`、更短的 `queue_length` 或 `mw_latency 0`。這會犧牲吞吐與起播體驗，
不適合作為 Oryx 混合場景的全域預設。

---

## 2-2. 自訂 SRS 版本的替換與能力驗證（本 fork 新增功能）

若你想用自己的特製版 SRS（自行編譯、加私修改、或換供應商映像），替換點就是
Dockerfile 的基底映像標籤；但換完怎麼確認「Oryx 依賴的功能都還在」？

### 能力自動驗證 API

新增端點：`POST /terraform/v1/mgmt/srs/capabilities`（需登入 token）

原理：對每個 Oryx 依賴的功能產生一份最小設定檔，呼叫 `srs -t -c <檔案>` 做解析級驗證
（不會綁定任何埠、不影響運行中的服務）。特製版如果缺某功能（如編譯時未開 SRT），
該項 probe 會直接報錯並附上原始錯誤輸出。

回應範例：

```json
{
  "version": "7.0.157",
  "bin": "/usr/local/srs/objs/srs",
  "features": [
    { "name": "rtmp",     "ok": true },
    { "name": "http_api", "ok": true },
    { "name": "hls",      "ok": true },
    { "name": "webrtc",   "ok": true },
    { "name": "srt",      "ok": false, "detail": "conf error: srt_server not supported" },
    { "name": "forward",  "ok": true },
    { "name": "hooks",    "ok": true }
  ]
}
```

探測的 feature 清單：`rtmp` / `http_api` / `hls` / `webrtc` / `srt` / `forward`(原生轉發指令) / `hooks`。

### 替換自訂 SRS 的標準流程

1. 改 Dockerfile 基底標籤指向你的映像（或用 `SRS_BIN` 環境變數指向容器內其他二進位路徑）
2. 本地建置映像（docs/local-build.md）
3. 啟動後呼叫 capabilities API，確認全部 `"ok": true` 再正式部署
4. 有缺項 → 換回官方標籤或補齊編譯選項

> 實作位置：`platform/srs-capability.go`。UI 已在「組件」頁（Components.js）新增
> 「SRS 核心能力」卡片：顯示核心版本＋各功能綠/紅徽章，失敗項附錯誤詳情，附手動重新檢測按鈕。

---

## 3. 發佈模型與一鍵工具 `scripts/bump-version.ps1`

### 3-1. 發佈模型：main = 開發映像，tag = 正式 release

| 觸發 | 產出映像 |
| --- | --- |
| push `main` | **開發**：`:sha-<commit>`（不可變，可回滾）、`:edge`（移動指標） |
| 打 `vX.Y.Z` tag | **正式**：`:vX.Y.Z`（不可變）、`:latest`（移動到本版） |

- 只有「打 tag」才算發佈 → `:latest` 與版號 tag 才是**不可變的正式識別**，不會被一般 push 覆蓋。
- CI（`docker-publish.yml`）會**驗證 `platform/version.go` 等於 tag**，不一致就直接失敗（避免映像標錯版）。
- 保留策略：`package-cleanup.yml` 只保留最新 20 個 `:sha-*`（開發/回滾用），**release tag 與 `:latest` 永不清除**。

### 3-2. 用法

```powershell
# 一鍵發佈（推薦）：patch+1 -> commit -> tag -> push（CI 隨即出 :vX.Y.Z 與 :latest）
powershell -File scripts\bump-version.ps1 -Release

# 發佈並附一筆變更歷史（changelogkit，寫入 docs/change-history.md）
powershell -File scripts\bump-version.ps1 -Release -Notes "本次重點一句話"
powershell -File scripts\bump-version.ps1 -Release -Minor        # minor+1
powershell -File scripts\bump-version.ps1 -Release -New v6.0.0   # 指定版號
powershell -File scripts\bump-version.ps1 -Release -DryRun       # 先預覽不執行

# 只升 SRS 核心一併發佈
powershell -File scripts\bump-version.ps1 -Release -Minor -SrsCore 7

# 只改版號檔（之後自己 commit/tag）
powershell -File scripts\bump-version.ps1 -Minor

# 只升 SRS 核心，不動產品版本號
powershell -File scripts\bump-version.ps1 -SrsCore 7
```

`-Release` 的前置檢查（任一不通過即中止）：必須在 `main`、工作區乾淨、目標 tag 不存在。它會依序
`改版號 → git commit -m "Release vX.Y.Z" → git tag vX.Y.Z → git push origin main → git push origin vX.Y.Z`。

### 3-3. 版本節奏建議

| 場景 | 動作 |
| --- | --- |
| 日常修復／cherry-pick | 只 push main（`:sha-*`／`:edge` 可回退），**不打版號** |
| 累積一批修復想出正式點 | `-Release`（patch +1） |
| 新增功能（如 forward 改造落地） | `-Release -Minor` |
| SRS 核心／重大架構變更 | `-Release -Minor -SrsCore 7` |

---

## 4. 相關文件

- [Docker 使用說明](docker-usage.md) — 映像標籤策略與部署更新
- [本地打包](local-build.md) — 升核心前的快速驗證手段
- [映像回退指南](rollback.md) — 升壞了怎麼退
