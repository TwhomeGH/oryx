# 本地整合測試教學

> 目的：在本機跑 CI 同款的整合測試（`test/oryx.test`），不用等 GitHub Actions。
> 適用場景：改了平台程式碼或 FFmpeg/Dockerfile 後，想快速驗證是否影響既有功能。

## 前置需求

- Docker Desktop（WSL2 後端）
- Go 交叉編譯器（透過 Docker 容器，**不需要本機安裝 Go**）
- 本機有 `ffmpeg` / `ffprobe`（WinGet 安裝即可：`winget install Gyan.FFmpeg`）

## 完整流程

### 1. 建置並啟動平台

在倉庫根目錄（有 `Dockerfile` 的地方）：

```powershell
cd F:\oryx

# 建置映像（首次約 30-60 分鐘，之後有快取較快）
docker build -t oryx:local .

# 停掉舊容器（如有）
docker stop oryx 2>$null; docker rm oryx 2>$null

# 啟動平台
docker run --rm -d `
  -p 2022:2022 -p 2443:2443 -p 1935:1935 `
  -p 8000:8000/udp -p 10080:10080/udp `
  --name oryx -e REACT_APP_LOCALE=zh oryx:local
```

> **重要**：每次重啟容器後，平台會自動生成新的 `SRS_PLATFORM_SECRET`。
> 測試腳本會自動偵測並初始化，不需要手動同步 secret。

### 2. 編譯測試檔

測試檔需要在 Linux 環境編譯（cross-compile），用 Docker 一行搞定：

```powershell
docker run --rm -v F:\oryx\test:/test -w /test golang:1.24 bash -c `
  "GOOS=windows GOARCH=amd64 go test -mod=vendor -c -o oryx.test.exe ."
```

產出 `test\oryx.test.exe`（Windows 可執行檔）。

### 3. 跑測試

#### 跑全部測試（比照 CI）

```powershell
cd F:\oryx\test
.\oryx.test.exe -endpoint http://localhost:2022 -test.v `
  -wait-ready=true -check-api-secret=true `
  -srs-ffmpeg-stderr -srs-dvr-stderr -srs-ffprobe-stdout
```

#### 只跑特定測試

```powershell
.\oryx.test.exe -endpoint http://localhost:2022 -test.v `
  -test.run TestScenario_WithStream_PublishCameraDuration `
  -wait-ready=true -check-api-secret=true `
  -srs-ffmpeg-stderr -srs-dvr-stderr -srs-ffprobe-stdout
```

#### 跑不含媒體的測試（較快）

```powershell
.\oryx.test.exe -endpoint http://localhost:2022 -test.v `
  -no-media-test=true -wait-ready=true -check-api-secret=true
```

#### 跑 HTTPS 測試

```powershell
.\oryx.test.exe -endpoint https://localhost:2443 -test.v `
  -no-media-test=true -wait-ready=true -check-api-secret=true `
  -force-https=true
```

## 偵錯旗標

| 旗標 | 用途 |
|---|---|
| `-srs-log=true` | 開啟平台詳細日誌 |
| `-srs-ffmpeg-stderr` | 印出 ffmpeg 的完整 stderr（看 Stream #0:0/0:1 映射） |
| `-srs-dvr-stderr` | 印出 DVR ffmpeg 的 stderr |
| `-srs-ffprobe-stdout` | 印出 ffprobe 的完整 stdout（看 ffprobe JSON） |
| `-srs-ffprobe-duration=35000` | ffprobe 分析時長（ms），預設 35 秒 |
| `-srs-ffprobe-timeout=45000` | ffprobe 超時（ms） |

### 範例：診斷 camera 測試音訊流失

```powershell
.\oryx.test.exe -endpoint http://localhost:2022 -test.v `
  -test.run TestScenario_WithStream_PublishCameraDuration `
  -wait-ready=true -check-api-secret=true `
  -srs-ffmpeg-stderr -srs-dvr-stderr -srs-ffprobe-stdout
```

然後在另一個終端看平台的 camera ffmpeg 日誌：

```powershell
docker logs oryx 2>&1 | Select-String "Camera: Start|Output #|Stream #0:" | Select-Object -Last 30
```

## 架構說明

### 測試執行流程

```
oryx.test.exe 啟動
  → prepareTest(): 解析 flag、定位 ffmpeg/ffprobe
  → waitForServiceReady(): 等平台 API 就緒
  → auto-init 偵測: 用 /terraform/v1/mgmt/init (NoAuth) 檢查 init 狀態
     → 未初始化: 自動呼叫 init + login，並更新 API secret
     → 已初始化: 跳過
  → initSystemPassword(): 設定管理密碼
  → initSelfSignedCert(): 設定 HTTPS 憑證
  → 關閉 logger（除非 -srs-log=true）
  → m.Run(): 跑所有匹配 -test.run 的測試用例
```

### ffmpeg 路徑查找（跨平台）

`test/main_test.go` 的 `tryOpenFile` 依序嘗試：

1. `os.Stat(filename)` — CWD 直接找
2. `os.Stat("../filename")` — 父目錄（GoLand blackbox 相容）
3. `os.Stat("test/filename")` — test 子目錄
4. `exec.LookPath(filename)` — 系統 PATH（跨平台：Linux/macOS/Windows）

Windows 上 `ffmpeg`/`ffprobe` 需在 PATH 中（WinGet 安裝後自動滿足）。

### 平台 vs 測試的 ffmpeg 使用

| 角色 | ffmpeg 來源 | 說明 |
|---|---|---|
| 平台 camera 重新推流 | Docker 容器內的 BtbN ffmpeg | `camera-live-stream.go` 用 `exec.CommandContext(ctx, "ffmpeg", ...)` |
| 測試 DVR ffprobe | 主機的 ffmpeg/ffprobe | 測試檔在主機跑，用主機的 ffmpeg 做 DVR 擷取 |
| CI | Ubuntu host ffmpeg + Docker BtbN ffmpeg | 同上，host 和 container 各用各的 |

### API 認證

- **初始化前**：`/terraform/v1/mgmt/init` 和 `/terraform/v1/mgmt/login` 不需要 auth
- **初始化後**：所有其他 API 需要 `Authorization: Bearer <SRS_PLATFORM_SECRET>`
- `SRS_PLATFORM_SECRET` 由平台啟動時自動生成，存於 Redis
- 測試的 auto-init 邏輯會從 init 回應中取得 `bearer` 並更新 `*apiSecret`

### CI 失敗：`Start SRS failed` / `Get SRS_PLATFORM_SECRET failed`

如果 CI 的 docker.log 顯示 SRS 啟動失敗（`Run srs-server` 之後**零輸出**就退出、
沒有 `objs/srs.pid`），但本機跑同樣映像卻正常，最可能是 **UPX 壓縮的 binary
在 runner 上自解壓崩潰**：

- 舊版 Dockerfile 會對 `srs` 和 `platform` binary 做 `upx --best --lzma` 壓縮。
- UPX-LZMA 的 binary 在**程序啟動時自解壓**，在某些 runner kernel / memory
  layout 下會瞬間 SIGSEGV，症狀就是「零輸出立刻退出」。
- 這是 **flaky**：同一個 image 在 A runner 正常、B runner 崩潰，所以 CI 會
  偶發失敗且每次掛的 job 不一定（ZH/EN 都中過）。
- 本機（WSL2 / 本機 Docker）因為 kernel 不同，通常複製不出來。

解法：Dockerfile 已移除 UPX 壓縮步驟（`upx --best --lzma` 那一段），重新 build
映像後 binary 不再自解壓，即可消除這類偶發崩潰。若又看到類似症狀，先檢查
Dockerfile 是否把 UPX 加回去了。

## 常見問題

### `file ffmpeg not found`

測試找不到 ffmpeg。確認 PATH 中有 ffmpeg：

```powershell
where.exe ffmpeg
# 應該輸出 ffmpeg.exe 的路徑
```

### `invalid status code 500` / `401`

API 認證失敗。通常是 secret 不對。解法：

1. 重新建立容器（`docker stop oryx; docker rm oryx`）
2. 重新啟動（`docker run ...`）
3. 重跑測試（auto-init 會自動處理 secret 同步）

### `already initialized`

平台已經被初始化過，init 不允許重設密碼。這是正常的——如果平台已有密碼，
測試會使用已有的 secret。如果 secret 不匹配，重建容器即可。

### Camera 測試失敗：`nb_streams != 2`

DVR 擷取的檔案只有 video 沒有 audio。診斷步驟：

1. 用 `-srs-ffmpeg-stderr` 看 camera ffmpeg 的 `Output #0` 有幾條 Stream
2. 用 `-srs-dvr-stderr` 看 DVR ffmpeg 的輸入有幾條 Stream
3. 直接 probe SRS 的 HTTP-FLV：

```powershell
ffprobe -v error -show_entries stream=codec_type "http://localhost:2022/live/publish-stream-<id>.flv"
```

如果 SRS flv 就只有 1 條流 → 問題在 camera ffmpeg 或 SRS re-publish。
如果有 2 條 → 問題在測試的 DVR 擷取步驟。
