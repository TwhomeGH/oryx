# 映像回退指南

> 新版映像部署後服務異常？這份文件說明怎麼快速退回上一個正常狀態。

## 回退的三種來源

| 來源 | 適用情境 | 可用範圍 |
|---|---|---|
| `:sha-xxxxxxx` 標籤 | 最常用，精準回到某次建置 | 最近 **20** 次（更舊的會被每日清理任務刪除） |
| 舊版號標籤（`:v5.16.x`）| 只有曾經改過 `platform/version.go` 出過新號才存在 | 該版號未被覆蓋就永久可用 |
| 本地匯出檔（tar） | 其他來源都失效時的保命符 | 只要 tar 還在 |

---

## 步驟一：找出要退到哪個標籤

1. 打開 GitHub 倉庫頁面 → 右側 **Packages** → 點 `oryx`
2. 版本列表中找 `sha-` 開頭的標籤，越上面越新
3. 不知道哪個 sha 對應哪次修改？到倉庫的 **Actions** 頁面，
   每筆建置紀錄都標著 commit 訊息和 short sha，對照即可

也可以用 git 在本地反查：

```bash
git log --oneline -10        # 看最近 10 次提交的 short sha
# 例如想回到 fix something 那次，它的 sha 是 a1b2c3d
```

## 步驟二：執行回退

編輯 `docker-compose.yml`，把 image 改成目標標籤：

```yaml
services:
  oryx:
    image: ghcr.io/twhomegh/oryx:sha-a1b2c3d   # ← 改這裡
```

然後套用並確認：

```bash
docker compose pull && docker compose up -d
docker compose ps                 # 容器應為 Up
curl -I http://localhost:882/mgmt/   # 或你的管理介面位址，應回 200
```

> `/data` 掛載卷不在容器裡，回退**不會**影響你的設定與錄製資料。

## 步驟三：處理根因

回退只是止血，之後要：

1. 在 repo 用 `git log` 找出肇事的 commit
2. 修復或 `git revert <SHA>` 產生反向 commit
3. push 觸發新的建置，驗證正常後再把 compose 的 tag 改回去
   （建議改回 `:v版號` 或最新的 `:sha-xxx`，不要長期停在舊存檔點）

---

## 保命符：從 tar 匯入

如果 GHCR 上的標籤都被清掉或不方便連網：

```bash
# 事前保險（在某個已知正常的狀態做一次）
docker pull ghcr.io/twhomegh/oryx:v5.15.20
docker save -o oryx-v5.15.20.tar ghcr.io/twhomegh/oryx:v5.15.20

# 緊急回退
docker load -i oryx-v5.15.20.tar
docker compose up -d --pull never   # --pull never 避免又去遠端拉新的
```

建議每次升級前，把舊版 tar 留一份在工作目錄，成本幾百 MB 換一條後路。

---

## 常見問答

**Q：`:latest` 可以拿來回退嗎？**
不可以。它永遠指向最新建置，回退就是為了離開最新。

**Q：回退後下次 `compose pull` 會不會又被拉回新版？**
只要 image 寫的是具體標籤（`sha-a1b2c3d` 或某個舊版號）就不會；
如果寫的是 `:latest` 就會。所以回退期間避免使用 latest。

**Q：怎麼確認現在跑的到底是哪個版本？**
```bash
docker inspect oryx --format "{{.Config.Image}}"
```
```
