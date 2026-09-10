# Redis 5 直接遷移 Valkey

2026-09-10 決策：平台資料服務採 **Redis 5.0.14 → Valkey**，不先部署 Redis 7.2 或 Redis 8。根目錄 Dockerfile 與發布 workflow 已接入 Valkey；本機完整映像驗證通過，尚未執行遠端發布或切換線上容器與資料。

## 1. 選型與相容性邊界

本輪選用官方仍支援的 **Valkey 8.1.10**，固定映像 digest。Valkey 是 Redis OSS 7.2.4 的延續，採 BSD；官方提供 Redis OSS 7.2 及更早版本的相容遷移路徑。專案使用的 String、Hash、計數與 TTL 不依賴 Redis 8 專屬功能。[官方下載](https://valkey.io/download/)、[遷移指南](https://valkey.io/topics/migration/)。

這是候選版本，不代表它是最新功能分支；發布前仍須檢查安全修補與支援狀態。Valkey 與 Redis 的版本號獨立，不以數字相同推論功能相同。

## 2. 單容器設計

| 項目 | 方案 |
|---|---|
| 伺服器 | 正式根目錄 Dockerfile 封裝 Valkey 8.1.10；`valkey-candidate.Dockerfile` 保留作為既有映像的隔離實驗入口 |
| 既有設定 | 保留 `REDIS_HOST/PORT/PASSWORD/DATABASE`，不要求使用者重填配置 |
| 資料與 key | 保留 `/data/redis`、現有 key 名稱、JSON 字串及 DB 選擇 |
| 持久化 | 本輪沿用現有 RDB 設定；不在遷移時同時切換 AOF 策略 |
| 啟停 | `redis-server`、`redis-cli` 相容入口轉向 Valkey；同時提供原生 `valkey-server`、`valkey-cli` |
| 程序名稱 | 舊入口仍可被 `pidof redis-server` 找到，保留原 `auto/start_redis`、`auto/stop_redis` 流程 |
| Go 客戶端 | 保留 vendored go-redis/v8；先驗證既有協定與指令，不同步更換客戶端 |
| runtime | 將 Valkey 所需 OpenSSL、glibc、systemd/cap/zlib/zstd 等依賴放在 `/opt/oryx/valkey`，wrapper 使用專用載入器；不覆蓋主系統 glibc |
| 授權 | 攜帶 Valkey COPYING 與相應依賴 copyright；發布前仍需完成依賴來源／版本及散布義務整理 |

既有命名是相容介面，並不表示候選內仍運行 Redis 5。目前只替換 server/cli；沒有引入 Sentinel、Cluster 或 modules。

## 3. 監控 API 與畫面

Valkey 的 INFO 可能同時回傳 `redis_version:7.2.4` 與 `valkey_version:8.1.10`；前者是相容欄位，不可當成實際伺服器版本。

`redisInfoSnapshot` 新增：

- `server_name`：有 `valkey_version` 時為 `valkey`，Redis 回應為 `redis`。
- `server_version`：Valkey 使用 `valkey_version`，Redis 使用 `redis_version`。
- 原有 `redis_version` 保留原值，避免破壞既有 API 呼叫端。

前端頁籤及資料卡使用 Redis / Valkey 名稱，版本列顯示實際 Valkey 版本；連到尚未提供新欄位的舊後端時，仍回退至 `redis_version`。API 路徑、認證與其他 INFO 指標維持原介面。

## 4. 可重現的候選驗證

來源映像由本機已安裝的 Oryx 提供，不會掛載正在執行的正式資料。候選是既有映像的資料服務替換實驗，不包含此次 checkout 的 Go/UI 監控修改；監控修改另跑單元測試，正式映像需從整合後原始碼重建。

```powershell
docker pull valkey/valkey:8.1.10
docker pull golang:1.26
docker build --network none -f scripts/tools/valkey-candidate.Dockerfile -t oryx-valkey:candidate .
pwsh -NoProfile -File scripts/tools/test-redis-upgrade.ps1
docker run --rm --network none --entrypoint bash --mount "type=bind,source=$PWD/scripts/tools/test-kv-runtime.sh,target=/test.sh,readonly" oryx-valkey:candidate /test.sh
```

`test-redis-upgrade.ps1` 預設目標是 `oryx-valkey:candidate`。建立隔離的 Redis 5 合成資料，SAVE 後複製 RDB 至另一個 volume，由 Valkey 讀取、寫入、SAVE/重啟；最後使用未改動的舊快照回復 Redis 5。Redis/Valkey 無對外網路或發布 port，Go probe 僅加入測試容器的網路 namespace。清理時只刪除本次建立且標籤吻合的容器與 volume。

資料測試涵蓋 Hash/JSON、String、計數、HSCAN、TTL 原到期時間、DB 1、redis.Nil、INFO。原 runtime 測試覆蓋啟停、pidof、localhost、SAVE/重啟與 FFmpeg/ffprobe 啟動。它們不等於完整平台流程、真實資料、TLS、ARM64、HA、AOF 或長時間效能驗證。

官方映像 digest：`sha256:3fbd2e3e4b6e85e046c1e7c215e8f79087bc0357789184305806664e320996f3`。

本機候選 digest：`sha256:eb918cda54cd0ac1579a2a303260f9208e997ba4c40c7eb1710d33f5b6296a94`。

2026-09-10 驗證結果：

| 檢查 | 結果 |
|---|---|
| Redis 5.0.14 → Valkey 8.1.10 合成 RDB 遷移、指令、TTL、DB 1、INFO | 通過 |
| Valkey 寫入後 SAVE/重啟 | 通過 |
| Redis 5 + 未改動舊快照回復 | 通過 |
| 既有啟停、pidof、localhost、系統 glibc 2.31、FFmpeg/ffprobe 啟動 | 通過 |
| Go 指定回歸範圍（TestConsoleMetrics、TestNormalizeConsoleRoute、TestBuildRedisInfoSnapshot，含 Valkey 真實版本案例） | 通過 |
| SrsConsole 既有 5 項測試及該頁 ESLint | 通過 |

實測 INFO 確實同時回報 redis_version 7.2.4、server_name valkey、valkey_version 8.1.10，版本辨識的修改有實際資料依據。

## 5. 正式切換及回復

根目錄 Dockerfile 與候選 Dockerfile 共用 `prepare-valkey-runtime.sh`，從固定 digest 的官方映像提取執行檔、依賴與授權資料。正式 dist 階段繼承已安裝 Valkey 的 base，並包含本次原始碼建置的 Go/UI。

`docker-publish.yml` 先建置並載入 linux/amd64 映像，再對該 image ID 執行 `test-kv-runtime.sh` 及 Redis 5 遷移／回復測試；全部通過後，才推送同一個已測映像的 tags。測試失敗不會執行推送步驟。一般測試與 PR workflow 也加入 Valkey runtime 檢查。

2026-09-10 本機已從根目錄 Dockerfile 完整建置，並通過正式發布流程使用的 runtime、`redis:5.0.14` 合成 RDB 遷移及舊快照回復測試。可重現指令：

```powershell
docker build -f Dockerfile -t oryx-valkey:release-local .
docker run --rm --network none --entrypoint bash --mount "type=bind,source=$PWD/scripts/tools/test-kv-runtime.sh,target=/test.sh,readonly" oryx-valkey:release-local /test.sh
pwsh -NoProfile -File scripts/tools/test-redis-upgrade.ps1 -SourceImage redis:5.0.14 -TargetImage oryx-valkey:release-local
```

以下為線上資料切換流程，尚未在正式服務執行：

1. 使用正式建置的新映像，驗證監控、登入、推流、錄製、任務恢復及重啟；合成資料與 runtime 測試不代表這些平台流程已完整驗證。
2. 在受控環境使用正式資料副本驗證 key 數、DB、TTL、設定與任務；不得在公開報告輸出密碼或業務 value。
3. 維護窗口停止所有寫入端，完成最終一致快照、正常停止 Redis，保存舊映像 digest、設定及升級前完整資料備份。實際若啟用 AOF，需另外驗證完整 AOF 備份，不套用 RDB-only 流程。
4. Valkey 只開啟另一份候選資料目錄；不能讓新舊程序同時寫相同目錄。驗證資料與平台行為後再恢復服務。
5. 若失敗，先停止 Valkey 與写入端，另存新資料；使用 **Redis 5 舊映像 + 升級前備份**回復。切回映像不代表能讀新版寫出的檔案，升級後的新寫入也不會自動出現在舊備份。

發布前還需完成私有 runtime 的依賴維護與散布資料、ARM64 及 TLS 等適用情境驗證。本轮不改寫正式 volume、不發布映像、不切換正式服務。

## 6. 舊方案的地位

[Redis 7.2 / 8 演練紀錄](redis-upgrade.md) 只保留為歷史比較。`redis72-*` 檔案是當時的本機實驗工具，不是正式建置入口，也不是 Valkey 遷移的前置步驟。不要先升級 Redis 7.4/8 再假設可直接把 RDB 搬到 Valkey。
