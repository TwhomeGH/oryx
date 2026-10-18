# 轉推快速測試工具

> 目的：讓任何人（含第一次接觸的人）都能在幾分鐘內驗證「轉推（forward）」是否正常，
> 並在出問題時知道去哪裡看診斷。**不需要外部平台帳號**，全部在本機自測。

## 前置

- 平台容器正在執行（預設名稱 `oryx`，見 `docs/deploy-guide.md`）。
- 主機已安裝 `ffmpeg`（Windows：`winget install Gyan.FFmpeg`）。
- 倉庫內的測試輸入檔 `test/source.200kbps.768x320.flv`。

## 一鍵測試

```powershell
pwsh scripts/tools/forward-quick-test.ps1                 # 正常流程（只測來源→轉推→指標，不含中斷/備援/對照）
pwsh scripts/tools/forward-quick-test.ps1 -Outage         # 加上「來源中斷 → 恢復」示範（斷線行為）
pwsh scripts/tools/forward-quick-test.ps1 -NoPlay         # 不開 ffplay 預覽（預設會自動開，CI/無顯示環境用）
pwsh scripts/tools/forward-quick-test.ps1 -Normalize      # 啟用「補強轉碼（正規化 CFR）」再測（畫質/時間戳）
pwsh scripts/tools/forward-quick-test.ps1 -Outage -Slate  # 來源中斷改用備用畫面（深灰底＋跳動時鐘）
pwsh scripts/tools/forward-quick-test.ps1 -Keep           # 保留設定，方便自己點 UI 看
pwsh scripts/tools/forward-quick-test.ps1 -Container oryx -Endpoint http://localhost:882
```

> 埠會**自動偵測**：工具用 `docker port <容器> 2022/1935` 找出對外埠（自訂埠如 `882`/`1936`
> 也能用）；需要時才用 `-Endpoint`/`-Rtmp` 手動指定。

它會依序：

1. 檢查前置條件（ffmpeg、來源檔、容器）。
2. 從容器讀取 `SRS_PLATFORM_SECRET`（API）與 `SRS_AUTH_SECRET/pubSecret`（推流）。
3. 用**本機 ffmpeg** 推一個來源到 `rtmp://localhost:1935/live/quick-src-<pid>`。
4. 啟用一個**本機目標**的轉推：`rtmp://localhost/live/quick-tgt-<pid>`。
5. 預設會用 `ffplay` 即時播放目標輸出（HTTP-FLV），可直接看到凍結與恢復；`-NoPlay` 可關閉。
6. 每 3 秒印出狀態與指標（`status/mode/fallback/restarts | avgFps/noOutput/drift/reconnects`）與最近事件。
7. `-Outage` 時：中斷來源、觀察（`fallback=True`、`noOutput` 上升、restart 不變），再恢復來源、觀察恢復。
   - 預設 `failover=hold`：目標畫面**凍結在最後一格**；
   - 加 `-Slate`：目標畫面切到**備用畫面（深灰底＋跳動時鐘）**，可一眼確認「還在播」。
8. 結束後清理（除非 `-Keep`）。

輸出可播放（HTTP-FLV）：`http://localhost:2022/live/quick-tgt-<pid>.flv`。

## 在 UI 哪裡看

開啟 `http://localhost:2022` → **場景** → **轉推** → 展開「**狀態**」→ 每一列最後一欄
「**本機輸出進度**」：

- 累計 FPS／速度／輸出媒體時間（解析 FFmpeg 日誌）。
- 指標：`平均輸出 FPS`、`無輸出時間`、`媒體時鐘漂移`、`來源重連次數`，以及 avgFps 走勢。
- 可展開：**原始日誌**、**診斷時間線**（start/ready/failure/recovered/program-switch/program-reconnect）、**近期日誌**。

> 這些是**本機 FFmpeg 的輸出證據**，不代表目的平台已確認接收或播放；仍請至目的平台確認。

## 怎麼判讀

| 情境 | 你應該看到 |
| --- | --- |
| 正常 | `fallback=False`、`avgFps≈來源 fps`、`noOutput` 很小、`drift≈0` |
| 來源中斷（hold） | `fallback=True`、`noOutput` 持續上升、`restarts` 不變（下游凍結、未斷） |
| 恢復 | `fallback=False`、`noOutput` 回到很小、`frame` 重新遞增（一個重連） |
| 來源端在抖 | `reconnects` 增加、事件出現 `program-reconnect` |
| 掉幀/卡頓 | `avgFps` 低於來源 fps；`drift` 持續偏離 |

## 手動逐步（不用腳本）

1. 取密鑰：`docker exec oryx redis-cli hget SRS_AUTH_SECRET pubSecret`，
   `docker exec oryx redis-cli hget SRS_PLATFORM_SECRET token`。
2. 推來源：`ffmpeg -re -stream_loop -1 -i test/source.200kbps.768x320.flv -c copy -f flv
   "rtmp://localhost:1935/live/mysrc?secret=<pubSecret>"`。
3. 啟用轉推（API，Bearer `<token>`）：
   `POST /terraform/v1/ffmpeg/forward/secret`
   `{"action":"update","platform":"forwarding-quick","server":"rtmp://localhost/live/","secret":"mytgt?secret=<pubSecret>","enabled":true,"custom":true,"label":"local"}`
   （用自訂名稱，勿覆蓋內建平台；測試完用 `{"action":"delete","platform":"forwarding-quick"}` 刪除）。
4. 查狀態：`GET /terraform/v1/ffmpeg/forward/streams`（看 `metrics`/`events`/`logs`）。
5. 到 UI 的「轉推 → 狀態」看「本機輸出進度」。

## 常見問題

- **401 / Invalid Authorization**：密鑰不對。確認容器名與 `SRS_PLATFORM_SECRET`；或用
  `bash scripts/tools/secret.sh` 同步。
- **找不到容器 / 連線被拒**：容器名稱或埠不對。工具會自動偵測容器對外埠；若平台不是用預設
  埠，確認 `docker port <容器> 2022`，或直接給 `-Endpoint`（例：`-Endpoint http://localhost:882`）。
- **健康時 `avgFps` 卻偏低**：來源本身掉幀，或主機（如 WSL2）資源不足；看 `noOutput`／`drift` 佐證。
- **中斷後一直沒有恢復**：代表「下游恢復」鏈路異常——這正是本工具要抓的；請附上
  `-Outage` 的輸出（`metrics`/`events`）。

## 量測轉推延遲（forward-latency.ps1）

`scripts/tools/forward-latency.ps1` 量「Oryx 轉推自己多出來的延遲」：在同一個瞬間比對
**program（來源端）**與**轉推輸出**的視訊 PTS，差值就是轉推這段（**不含平台端**）的延遲。

```powershell
# 用現有直播來源（建議；先確認來源正在推流）
pwsh scripts/tools/forward-latency.ps1 -Source live/livestream

# 本機自帶測試來源（無現有直播時；目前部分環境的自動就緒較不穩，建議優先用 -Source）
pwsh scripts/tools/forward-latency.ps1 -TestSource

# 多取樣
pwsh scripts/tools/forward-latency.ps1 -Source live/livestream -Samples 10 -IntervalSec 2
```

輸出範例：
```
 1. program=  123.456s  target=  121.200s  forward latency=  2.26s
 ...
forward latency: avg=2.30s  min=2.20s  max=2.40s  (samples=5)
note: this excludes the platform's own latency; add that for the viewer-side delay.
```

注意：
- 這**不含平台端**延遲（Twitch/YouTube 自己的 5–15s）；觀眾端總延遲 ≈ 轉推延遲 + 平台延遲。
- 量測會建立一個臨時的 `forwarding-latency-*` 本機目標，結束自動刪除。
- 需要容器在跑（預設名稱 `oryx`）；API/RTMP 連接埠會自動偵測。
## 對照「不轉碼」與「補強轉碼」（-Compare）

```powershell
pwsh scripts/tools/forward-quick-test.ps1 -Compare
```

同時建立兩個本機目標（都以 `TEST` 標記）：

| 目標 | 說明 |
|---|---|
| `TEST quick (copy)` | 零轉碼（`-c copy`），輸出等於來源 |
| `TEST quick (normalize)` | 開啟補強轉碼（CFR＋修正時間戳） |

之後每 3 秒會並列顯示兩邊的 `status / avgFps / noOutput / drift`，可直接比較補強轉碼的影響。

## 顏色提示

狀態列依情況上色，方便一眼判斷：

- **綠色**：`status=running`（正常）
- **黃色**：`fallback=True`（來源中斷、備援中）
- **紅色**：`retrying` / `degraded`（重試/降級）
- **灰色**：其他（等待中）

（`TEST` 目標在控制台列表會帶黃色 `TEST` 徽章，與正式目標區分。）