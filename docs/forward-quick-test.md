# 轉推快速測試工具

> 目的：讓任何人（含第一次接觸的人）都能在幾分鐘內驗證「轉推（forward）」是否正常，
> 並在出問題時知道去哪裡看診斷。**不需要外部平台帳號**，全部在本機自測。

## 前置

- 平台容器正在執行（預設名稱 `oryx`，見 `docs/deploy-guide.md`）。
- 主機已安裝 `ffmpeg`（Windows：`winget install Gyan.FFmpeg`）。
- 倉庫內的測試輸入檔 `test/source.200kbps.768x320.flv`。

## 一鍵測試

```powershell
pwsh scripts/tools/forward-quick-test.ps1                 # 快樂路徑
pwsh scripts/tools/forward-quick-test.ps1 -Outage         # 加上「來源中斷 → 恢復」示範
pwsh scripts/tools/forward-quick-test.ps1 -Play           # 另外開 ffplay，即時看輸出（可看凍結/恢復）
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
5. `-Play` 時：用 `ffplay` 即時播放目標輸出（HTTP-FLV），可直接看到凍結與恢復。
6. 每 3 秒印出狀態與指標（`status/mode/fallback/restarts | avgFps/noOutput/drift/reconnects`）與最近事件。
7. `-Outage` 時：中斷來源、觀察（`fallback=True`、`noOutput` 上升、restart 不變），再恢復來源、觀察恢復。
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
