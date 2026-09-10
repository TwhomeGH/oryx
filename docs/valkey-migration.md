# Redis 5 升級 Valkey：映像更新、資料遷移與復原

採用本次 Dockerfile 建置並發布的 Oryx 映像內建 **Valkey 8.1.10**，取代 Redis 5。使用內建資料服務的部署，更新映像後會由原本的啟動流程啟動 Valkey，讀取掛載在 `/data/redis` 的既有資料；不需要另外安裝 Valkey、OpenSSL，或逐筆轉換 key。部署時請選擇包含這項變更的已發布版本，而不是僅憑「新版」或 `latest` 名稱判斷。

原有 `REDIS_HOST/PORT/PASSWORD/DATABASE`、密碼、key 名稱及資料庫選擇沿用。若設定連到外部 Redis，更新 Oryx 映像不會替你升級外部伺服器，平台仍連接原本指定的服務。

| 使用者關心的問題 | 更新後的行為 |
|---|---|
| 換映像就會使用 Valkey 嗎？ | 內建資料服務會；外部 Redis 不會被替換 |
| 原資料需要手動轉檔嗎？ | 預設 Redis 5 RDB 不需要，Valkey 啟動時直接載入 |
| 設定與密碼要重填嗎？ | 不需要，保留原 `/data` 掛載與環境變數即可 |
| 出問題能退回嗎？ | 可使用舊映像與升級前備份復原，不能只換回映像而沿用新版資料檔 |

## 1. 適用範圍

本指南適用於 Redis 5 的既有 Oryx 單容器部署，升級到內建 Valkey 的 linux/amd64 映像。專案預設使用 RDB，檔名為 `/data/redis/dump.rdb`，未開啟 AOF。Valkey 啟動時直接載入這份舊 RDB，之後的寫入與持久化由 Valkey 接手。

[Valkey 官方遷移指南](https://valkey.io/topics/migration/) 提供 Redis OSS 7.2 及更早版本的遷移路徑。本專案已通過 Redis 5.0.14 → Valkey 8.1.10 的隔離資料遷移測試，不需要先升級 Redis 6、7 或 8。自訂 AOF、Cluster、Sentinel、外部資料服務或其他架構，需依實際部署另行安排遷移。

## 2. 更新前準備

- 記錄目前可正常運行的舊映像 digest 或固定 tag、Compose 設定與 `/data` 的實際掛載來源。不要只記錄會變動的 `latest`。
- 先拉取指定的新映像，保留舊映像供復原使用。
- 安排短暫停機，停止平台與其他會寫入這個資料庫的服務。正常停止 Redis 並確認最後一次持久化成功；若逾時被強制終止或儲存失敗，先處理，不要把該次停止視為完整備份。
- 在服務停止後，備份完整 `/data` 與部署設定，保留檔案權限。只複製運行中的 `dump.rdb` 可能漏掉尚未持久化的變更。

## 3. 更新映像與自動載入資料

把部署的 image 改成指定新版，**保留原有環境變數與 `/data` 掛載**，再建立並啟動新容器。不要刪除 volume，也不要同時啟動新舊容器寫入同一份資料。

啟動時，既有 `redis-server` 相容入口實際執行 Valkey，並直接讀取 `/data/redis/dump.rdb`。使用者不必修改 Redis 名稱的環境變數或執行額外匯入命令。也可先把停機備份複製到另一份資料目錄，讓新容器使用該副本；這是方便保留原資料的選項，並非每次更新必須改掛載路徑。

映像已包含 Valkey 所需執行庫，使用者不需要在宿主機補裝 OpenSSL 或替換系統 glibc。

## 4. 確認更新成功

1. 管理介面可登入，原本設定、串流配置及任務仍存在。
2. 監控的 Redis / Valkey 頁面顯示 **Valkey 8.1.10**，資料庫可正常讀寫。INFO 裡的 `redis_version:7.2.4` 是相容欄位，實際版本以 `valkey_version`／畫面的伺服器版本為準。
3. 確認實際使用的推流、錄製及背景任務可以工作；再正常重啟一次，確認設定和任務資料仍存在。

如果看見初始化畫面、原設定消失或資料載入失敗，先停止新容器，核對 `/data` 掛載、檔案權限及啟動日誌。不要直接重新初始化來覆蓋原本資料。

## 5. 正式切換及回復

更新成功後可繼續使用 Valkey，並保留升級前備份到確認服務穩定為止。

若需要退回 Redis 5：

1. 停止新容器與其他寫入端，另存升級後的資料，供後續排查或救回新寫入使用。
2. 將 image 改回升級前記錄的舊映像。
3. 把升級前完整 `/data` 備份還原到一個獨立的復原目錄或 volume，並讓舊容器掛載這份資料；保留原有密碼與部署設定。
4. 啟動舊容器，確認登入、設定、任務及資料讀寫恢復正常。映像 tag／tar 的操作見[映像回退指南](rollback.md)。

**復原必須搭配「舊映像 + 升級前備份」**。不要讓 Redis 5 直接開啟 Valkey 已重新儲存的資料檔。回到備份也代表回到備份當時的狀態，升級後新增的設定、任務與錄製檔不會自動合併；因此要先另存新資料。

## 6. 維護者：建置與驗證

根目錄 Dockerfile 固定使用 Valkey 8.1.10 官方映像 digest：`sha256:3fbd2e3e4b6e85e046c1e7c215e8f79087bc0357789184305806664e320996f3`。`prepare-valkey-runtime.sh` 將執行檔與依賴放到 `/opt/oryx/valkey`，wrapper 使用專用載入器，保留主系統 glibc 與既有啟停流程。`redis-server`、`redis-cli` 及原生 `valkey-server`、`valkey-cli` 入口皆可用。

`docker-publish.yml` 建置 linux/amd64 映像後，對同一個 image ID 執行啟停與 Redis 5 遷移／復原測試，全部通過才推送 tags。一般測試與 PR workflow 也檢查 Valkey runtime。監控 API 保留 `redis_version`，新增 `server_name`／`server_version` 供前端顯示真正的伺服器與版本。

2026-09-10 本機驗證結果：

| 檢查 | 結果 |
|---|---|
| 根目錄 Dockerfile 完整建置 | 通過 |
| Redis 5.0.14 RDB 載入、Hash/JSON、String、計數、HSCAN、TTL 到期時間、DB 1、redis.Nil、INFO | 通過 |
| Valkey 寫入後 SAVE／重啟 | 通過 |
| Redis 5 使用未改動舊快照復原 | 通過 |
| 原啟停腳本、pidof、localhost、系統 glibc、FFmpeg／ffprobe 啟動 | 通過 |
| Go 監控回歸與 SrsConsole 測試 | 通過 |

上述資料測試使用隔離的合成資料，不代表已驗證每個使用者的任務或部署。完整業務流程與真實資料副本仍需在部署驗收時確認；ARM64、自訂 AOF、TLS 連線及 HA 不在本次驗證範圍。

可重現指令：

```powershell
docker build -f Dockerfile -t oryx-valkey:release-local .
docker pull redis:5.0.14
docker pull golang:1.26
docker run --rm --network none --entrypoint bash --mount "type=bind,source=$PWD/scripts/tools/test-kv-runtime.sh,target=/test.sh,readonly" oryx-valkey:release-local /test.sh
pwsh -NoProfile -File scripts/tools/test-redis-upgrade.ps1 -SourceImage redis:5.0.14 -TargetImage oryx-valkey:release-local
```

測試工具只建立並清理自身標記的容器與 volume，不使用正式資料。`valkey-candidate.Dockerfile` 保留供既有映像的隔離比較，不是使用者更新的必要步驟。

Valkey 採 BSD 授權；映像封裝保留 Valkey COPYING、依賴 copyright 及套件版本清單。依賴更新與發行資料由映像維護者管理，無需使用者為日常升級另外處理。

[Redis 7.2／8 演練紀錄](redis-upgrade.md) 與 `redis72-*` 工具僅保留為歷史比較，不是 Valkey 升級前置步驟。
