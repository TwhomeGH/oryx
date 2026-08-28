# Docker 空間清理指南（build cache / 映像）

> 對象：本地開發者。這台機器反覆 build Oryx 映像（多階段：Go compile + UI npm install + FFmpeg），
> build cache 會快速累積到數十 GB，吃光 C 槽。本文說明怎麼診斷與清理。

---

## 1. 先診斷：空間被誰吃了

```bash
docker system df          # 總覽：映像 / 容器 / build cache / volume
docker system df -v       # 明細：每個映像、每條 cache 的大小
docker buildx du          # 只看 build cache 明細
```

判讀：

| 類型 | 常見大小 | 說明 |
|---|---|---|
| Images | 10-40GB | 含測試映像、多版本映像 |
| Build Cache | **最容易爆**，可到 20-30GB | 每次 `docker build` 的層快取 |
| Containers | 小 | 容器寫入層通常不大 |

**關鍵：BuildKit 的 cache 預設不會自動清理**，除非磁碟極度吃緊。反覆 build 大映像（如 Oryx）會累積大量 500MB-1.4GB 的層快取。

## 2. 清理指令

### 清 build cache（最常需要，安全）

```bash
docker builder prune -a -f
```

- `-a` 連「未使用的」cache 一起清
- `-f` 不問確認
- **不影響**任何映像或容器，只清暫存層

> 可以加 `--filter until=24h` 只清 24 小時前的 cache，保留近期加速用：
> `docker builder prune -a -f --filter until=24h`

### 清懸掛映像（dangling）

```bash
docker image prune -f
```

### 清未使用的映像

```bash
docker image prune -a -f     # 小心：會刪除未被容器使用的映像
```

### 一次清乾淨

```bash
docker system prune -a -f    # 映像 + cache + volume（-a 含未使用映像）
```

## 3. Oryx 特別注意

反覆 build Oryx 很容易產生大量空間消耗：

1. **多階段 build 層大**：`docker build -t oryx:local .` 含 Go compile、`cd ui && npm install`、FFmpeg，每階段都是百 MB 級 cache。
2. **測試映像別堆積**：build 測試用映像（如 `oryx:test-dockerfile`、`oryx:test-full`）用完就刪：

   ```bash
   docker rmi oryx:test-dockerfile oryx:test-full
   ```

3. **確認 /data 沒異常**：Oryx 容器內 `/data` 正常應很小（設定檔 + Redis）：

   ```bash
   docker exec oryx sh -c "du -sh /data/*"
   ```

   Oryx 本身的錄製/上傳資料也在 `/data`（DVR/record/upload），若這些目錄暴增代表真有資料在寫入。

## 4. WSL2 / Docker Desktop 的 vhdx 膨脹

Docker Desktop 的資料碟是 `docker_data.vhdx`（預設在 `C:\Users\<you>\AppData\Local\Docker\wsl\disk\`），
會隨映像/cache 增長。**刪除 cache 後 vhdx 不會自動縮小**（稀疏檔），實體空間釋放需重啟 Docker Desktop：

1. 關閉 Docker Desktop（或 `wsl --shutdown`）
2. 重新啟動 Docker Desktop

> 若 vhdx 仍異常大，可用 `Optimize-VHD`（Hyper-V 工具）壓縮，或考慮把 Docker 資料移到其他碟
> （Docker Desktop → Settings → Resources → Advanced → Disk image location）。

## 5. 建議的日常習慣

- **定期清 cache**：每週或每次大量 build 後跑 `docker builder prune -a -f`
- **測試映像即用即刪**：`docker rmi <test-image>`
- **監控 C 槽**：free < 5GB 就該清一次
