# 歷史紀錄：Redis 5 → 7.2 / 8 升級演練

> 本文的 Redis 7.2 / 8 選型與候選方案已停止推進，保留當時結果供比較。**現行方向是 Redis 5 直接遷移 Valkey**，以 [Valkey 遷移設計](valkey-migration.md) 為準。下方「目前」「下一步」均屬歷史紀錄，不代表現行發布計畫。測試工具現已預設 Valkey 候選；重跑歷史官方 binary 診斷時需明確指定 `-TargetImage` 與 `-SkipBareBinaryCheck:$false`。

2026-09-10 狀態：完成來源盤點與合成資料演練，**尚未更換正式映像或正式 Redis**。資料與常用指令測試通過，但官方 Redis 8 二進位檔不能直接複製進目前的 Oryx 執行環境。正式資料副本、完整平台流程、效能及多架構驗證仍待完成。

## 目前優先候選：Redis 7.2.16

依目前平台以 String/Hash 為主的使用方式，先評估 7.2，暫緩 Redis 8。7.2 保留 BSD-3-Clause 授權；官方維護表列至 2029-12-01。2026-09-10 查核官方發行列表後，選定 7.2.16 安全修補版，而不是固定在最早的 7.2.0。[7.2.16 發行說明](https://github.com/redis/redis/releases/tag/7.2.16)、[授權對照](https://redis.io/legal/licenses/)。

本輪使用 `redis:7.2.16`，拉取 digest 為：

```text
sha256:74566c6910d13ae61e7ce73ebd3127438a1fe805b309b097c323142719ec8a5b
```

| Redis 5.0.14 → 7.2.16 驗證 | 結果 |
|---|---|
| 原 redis.conf 啟動、讀取合成 Redis 5 RDB | 通過 |
| vendored go-redis/v8：Hash/String、計數、TTL、DB 1、INFO | 通過 |
| 寫入、SAVE、Redis 7.2 重啟 | 通過 |
| 用未改動的舊快照回復 Redis 5 | 通過 |
| 官方 7.2 binary 直接放入目前 Oryx 映像 | **失敗：同樣缺少 libssl.so.3** |

因此 7.2 在授權與功能範圍上較符合本輪目標，但不能假設官方映像中的執行檔依賴也更舊。下一步仍須在候選映像補齊相容依賴，或在現有 runtime 相容的環境編譯 Redis 7.2；尚未證明只補 libssl 就能解決所有 ABI 相容性。正式 Dockerfile、執行中的服務與資料未變更。

演練腳本預設目標已改為 7.2.16；可重跑：

```powershell
docker pull redis:7.2.16
pwsh -NoProfile -File scripts/tools/test-redis-upgrade.ps1 -TargetImage redis:7.2.16
```

測試的資料範圍、正式切換條件與回復限制同本文後續說明；正式資料副本及完整平台流程尚未驗證。下方保留先前 Redis 8.2.8 演練紀錄供比較，並非目前選定的發布目標。

## Redis 7.2 候選映像：補齊依賴後的結果

已建立本機 `oryx-redis72:candidate`，未發布或部署。上述缺少 libssl.so.3 是「只複製 binary」的歷史結果；補上 libssl.so.3 與 libcrypto.so.3 後，實測又發現 GLIBC_2.32/2.33/2.34 符號缺失。因此候選映像將官方 Redis 的 OpenSSL、libc、libm 與載入器放進 `/opt/oryx/redis72`，僅由 Redis wrapper 使用，不替換全域系統函式庫。

載入器保留 redis-server/redis-cli 程序名稱，讓既有 `pidof redis-server` 停機檢查繼續有效。包含 Redis BSD 與 OpenSSL/glibc 授權聲明；此為本機測試封裝，正式散布時仍須整理依賴版本、來源及相應散布要求。官方 Redis 映像未提供 openssl.cnf 時，使用空設定以維持預設 provider 行為。

```powershell
docker build --network none -f scripts/tools/redis72-candidate.Dockerfile --target candidate -t oryx-redis72:candidate .
pwsh -NoProfile -File scripts/tools/test-redis-upgrade.ps1 -TargetImage oryx-redis72:candidate -SkipBareBinaryCheck
docker run --rm --network none --entrypoint bash --mount "type=bind,source=$PWD/scripts/tools/test-redis72-runtime.sh,target=/test.sh,readonly" oryx-redis72:candidate /test.sh
```

`-SkipBareBinaryCheck` 只跳過「把單一 binary 放回未補依賴舊映像」的診斷；資料遷移、寫入、重啟及舊快照回復仍全部執行。候選映像繼承本機既有 Oryx 映像，不是從目前 checkout 重建整個平台。

最終候選 digest：`sha256:d738fb7e73b02cdf2117e1c7da2b2f6afe04b2fbcc37cf688f5fcc687b5ab779`。

- Redis 5 → 候選 7.2.16 合成資料遷移、Go 客戶端指令、TTL、DB 1、SAVE/重啟與舊快照回復：全部通過。
- 原 `auto/start_redis` / `auto/stop_redis`、pidof、localhost 解析、儲存後啟停：通過。
- 系統 glibc 仍為 2.31，FFmpeg/ffprobe 版本啟動檢查：通過；未進行完整影音功能測試。
- TLS 握手、完整平台、真實資料副本、ARM64 與長時間負載仍未驗證。獨立 runtime 需要隨 Redis 映像一起維護安全更新。

## Valkey 比較候選

Valkey 採 BSD，官方說明與 Redis OSS 7.2 及更早版本相容；可列為長期替代候選。尚未實測，不宣稱目前平台已相容。後續應重用隔離資料測試，另外驗證 valkey-server/valkey-cli 程序與原腳本的整合、動態函式庫、持久化、認證及監控。

監控不能只讀 `redis_version`：Valkey 可能為相容性回報 Redis 7.2.4，應讀 `server_name`、`valkey_version` 顯示實際服務。不要先升級到 Redis 7.4/8 再假設可直接搬同一份 RDB 到 Valkey。[官方遷移與相容性說明](https://valkey.io/topics/migration/)。

## 1. 現況與 Redis 8 歷史候選

| 項目 | 已確認結果 |
|---|---|
| 本機 `oryx` 容器 | Redis **5.0.14**，glibc **2.31**；唯讀執行版本查詢，沒有存取業務資料 |
| 正式映像來源 | 根目錄 `Dockerfile` 的 dist 繼承 `ossrs/oryx:focal-1` |
| Redis 建置來源 | `focal/Dockerfile` 使用 `redis:5.0`，複製 redis-server/redis-cli，經 UPX 壓縮後放進 Ubuntu focal |
| 其他路徑 | `Dockerfile.origin_cluster` 也繼承 focal-1；`scripts/setup-ubuntu/Dockerfile.script` 的 Redis 5 stage 只提供 redis-cli；停用的 mirrors workflow 另有 Redis 5 標籤 |
| 啟動 | `platform/auto/start_redis` 使用專案 redis.conf，加上 daemonize、資料目錄與可選的密碼／port |
| 資料 | `/data/redis`；平台設定、任務、直播間及錄製資訊等持久化狀態，不只是快取 |
| 儲存設定 | 版本控制內的設定是 RDB 快照（900 秒/1 次、300 秒/10 次、60 秒/10000 次變更），`appendonly no`；尚未查詢正式服務的 CONFIG GET，不能視為正式執行設定的證明 |
| Go 客戶端 | `github.com/go-redis/redis/v8 v8.11.5`；使用 host、port、password、database 連線，這個 v8 不是伺服器版本 |
| 主要操作 | GET/SET/DEL、HGET/HSET/HGETALL/HSCAN/HLEN/HDEL/HINCRBY、INFO；部分資料帶 TTL |
| 本輪候選 | Redis **8.2.8** 官方映像，作為可重現的測試基準；部署前仍需確認此分支最新安全修補版本 |

選擇 8.2 的理由是官方列為 Extended 分支，維護期限列至 2030-09-01。這不是聲稱 8.2 是最新功能版本。[官方版本維護表](https://redis.io/docs/latest/operate/oss_and_stack/install/version-mgmt/)、[8.2.8 發行說明](https://github.com/redis/redis/releases/tag/8.2.8)。

## 2. 可重現的隔離測試

工具：

- [PowerShell 演練腳本](../scripts/tools/test-redis-upgrade.ps1)
- [Go 指令與資料驗證程式](../scripts/tools/redis-upgrade-probe.go)

在有 Docker Linux engine 與 PowerShell 7 的環境執行：

```powershell
docker pull redis:8.2.8
docker pull golang:1.26
# SourceImage 指定本機已有、包含 Redis 5 的 Oryx 映像。
pwsh -NoProfile -File scripts/tools/test-redis-upgrade.ps1 `
  -SourceImage ghcr.io/twhomegh/oryx:latest `
  -TargetImage redis:8.2.8
```

腳本將映像標籤解析為本機 image ID，並輸出來源及目標 ID。建立獨立、帶本次執行識別標籤的 volume/container；不發布 port，Redis 使用 `--network none`，Go probe 只加入該測試容器的網路 namespace。原始碼唯讀掛載，Go 依賴使用 `-mod=vendor`，沒有讀取正式 `/data` 或正式 Redis 密碼。結束時檢查資源擁有標籤，僅清理本次建立的資源。

流程：

1. 用專案原有 redis.conf 啟動獨立 Redis 5，產生合成 Hash/JSON、String、counter、TTL 與 DB 1 資料，執行 SAVE 並停止。
2. 把 RDB 複製到另一個 volume，原始 Redis 5 快照保持獨立。
3. Redis 8 讀取副本，驗證資料、到期時間與 `redis.Nil`，執行常用讀寫及 INFO 查詢。
4. Redis 8 SAVE、停止、重新啟動，確認新增計數值及其他資料仍正確。
5. 用未經 Redis 8 寫入的舊快照重新啟動 Redis 5，驗證回復。
6. 額外把 Redis 8 官方 redis-server 放入獨立 volume，在舊 Oryx 映像執行 `--version`，測試二進位檔最低啟動相容性。

## 3. 2026-09-10 演練結果

測試使用本機 Docker Linux engine，來源 image ID：

```text
sha256:e411c51e4ca86a216484499e405f3b5162f67cac879210caaef1fa7959418238
```

Redis 8.2.8 映像拉取時的 digest：

```text
redis@sha256:2f7462b9e93e0a7ae2edf3a0a0babc8a4d29f8bfc50849b906b7caaef925edc1
```

| 驗證 | 結果 |
|---|---|
| Redis 5.0.14 產生合成快照 | 通過 |
| Redis 8.2.8 使用既有 redis.conf 啟動並讀取 Redis 5 RDB | 通過 |
| vendored go-redis/v8 常用指令、JSON 原文、TTL、DB 1、INFO sections | 通過 |
| Redis 8 寫入、SAVE、重啟與重新讀取 | 通過 |
| Redis 5 + 獨立保留的舊快照回復 | 通過 |
| Redis 8 官方 binary 直接放入舊 Oryx 映像 | **失敗：缺少 libssl.so.3** |

這證明本次合成資料的直接 RDB 遷移可行，不等於所有 Redis 5 資料都已驗證。官方 Redis 8 升級指南主要涵蓋 7.x 來源；不能把此結果當成官方支持所有 5 → 8 路徑的承諾。[官方單機升級指南](https://redis.io/docs/latest/operate/oss_and_stack/install/upgrade/standalone/)。

測試未涵蓋正式資料完整性、真正的登入／推流／錄製／任務恢復、長時間負載、ARM64、AOF 遷移、Redis modules 或 HA。合成回復只驗證升級前狀態；Redis 8 上線後新增的寫入不會自動出現在 Redis 5 備份裡。

## 4. 封裝決策

不能只將 `focal/Dockerfile` 的 `redis:5.0` 改成 `redis:8.2.8`：現有做法只複製 binary，不會帶入新版需要的動態函式庫；測試已在 libssl.so.3 失敗，其他 ABI 相容性也尚未驗證。只修改 focal/Dockerfile 也不會自動更新主 Dockerfile 所引用的已發布 focal-1 映像。

下一個實作階段須選擇並驗證以下其中一種封裝：

| 方式 | 需要的工作 |
|---|---|
| 保留單容器，在相容環境編譯 Redis 8 核心 | 固定來源版本與 checksum、確認編譯器與依賴、保留授權及來源資訊、在實際 focal runtime 測試 redis-server/redis-cli、更新主映像引用；不要再套用舊 UPX 流程 |
| 更新整個 runtime 基底 | 同步檢查 Redis、SRS、FFmpeg、lego 與啟動腳本的相容性，範圍較大 |
| Redis 改獨立官方容器 | 設計持久化 volume、健康檢查、網路、認證、備份及啟動順序；平台也須支援停用內建 Redis 啟動，不能只改 REDIS_HOST |

若優先保留現有單容器部署體驗，可先做第一種候選映像，使用本工具的 `-TargetImage` 重跑相同資料驗證。通過後仍需完整平台流程測試，才可替換發布建置。這輪尚未改動正式 Dockerfile 或啟動流程。

Redis 8 提供 AGPLv3、RSALv2、SSPLv1 選項，與 Redis 5 的授權不同；發布包含 Redis 的映像前，需選定適用條款並落實相應的授權／來源提供方式。[官方授權說明](https://redis.io/legal/licenses/)。

## 5. 正式切換與回復條件

正式切換前先用受控的真實資料副本演練，不直接讓新版開啟唯一的資料目錄。

1. 記錄舊映像 digest、實際 Redis 版本與持久化設定、資料目錄、各 DB 的 key 數、資料量與 TTL 分布。不要把密碼或業務 value 寫進公開測試報告。
2. 排定維護窗口，停止平台及其他寫入端，確認背景儲存沒有錯誤；完成最終快照並正常停止 Redis。若正式環境啟用了 AOF，要另外設計一致的完整 AOF 備份，不能沿用本輪 RDB-only 步驟。
3. 保留升級前完整資料目錄、設定與旧映像，記錄 checksum；使用另一份資料副本啟動候選新版。Redis 新旧版本不能同時寫同一個目錄。
4. 驗證登入、設定儲存、直播間、推拉流、錄製、任務恢復、重啟及 Redis/API 監控，再恢復流量。
5. 若需回復，先停止新版及所有寫入端，另存新版資料供後續分析，使用**舊映像 + 升級前備份**還原。不能僅切回映像並期待 Redis 5 讀懂 Redis 8 寫出的 RDB/AOF。恢復服務後的新增資料需另行處理，回復備份會退回備份時間點。

持久化強化（例如 AOF everysec）應另作變更：先評估 RPO、磁碟空間與 I/O，再測試故障還原，避免和版本遷移同時改動而難以定位問題。
