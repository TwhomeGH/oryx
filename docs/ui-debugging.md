# 前端本地調試與測試指南

> 對象：要本地開發 / 調試 / 測試 Oryx UI（Vite + React）的開發者。
> 目標：從「啟動」到「瀏覽器裡看實際效果」到「自動化測試」，一條龍說明。

---

## 1. 概覽

Oryx UI 是 Vite + React 專案（`ui/`），建置時由 Go 平台內嵌。本地開發有兩條路：

| 方式 | 用於 | 特色 |
|---|---|---|
| **Vite dev server** | 日常改 UI、調排版、看效果 | HMR 即時更新，連真實後端 |
| **Playwright 自動化** | 端到端測試、重複操作、截圖 | 可跑在 dev server 或產物上 |

> 不管哪種，前端都要連到「平台後端」才有資料（登入、env、streams 等）。後端可以是 Docker 容器或原生。

---

## 2. 啟動 Vite dev server

### 2.1 一鍵腳本（推薦）

```powershell
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1                 # 預設開場景頁
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page console   # 控制台
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page "scenario?tab=vlive"
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page settings
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page players   # 播放器目錄
```

腳本會：
1. 自動偵測平台端口（先試 `127.0.0.1:2022` 原生，再試 `882` Docker 映射）
2. 檢查 node_modules，缺依賴自動 `npm install`
3. 啟動 Vite（`PUBLIC_URL=/mgmt` + `REACT_APP_LOCALE=zh` + `SRS_PLATFORM=<偵測到>`）
4. 自動開瀏覽器到指定頁面

常用參數：
- `-Platform http://127.0.0.1:882`：手動指定平台端口
- `-Port 4000`：改 dev server 端口（預設 3000）
- `-NoBrowser`：不自動開瀏覽器
- `-Page players`：開播放器目錄

> 新增頁面時，記得在 `scripts\dev-server.ps1` 的 `$pageMap` 對照表加一行。

### 2.2 手動啟動

```powershell
cd ui
$env:PUBLIC_URL="/mgmt"; $env:REACT_APP_LOCALE="zh"
$env:SRS_PLATFORM="http://127.0.0.1:882"   # 改為你的平台端口
npm start
```

然後開 `http://localhost:3000/mgmt/zh/routers-console`，用平台密碼登入。

### 2.3 `SRS_PLATFORM` 是什麼

vite.config.mjs 的 dev proxy 把 `/api` `/terraform` `/players` 等轉發到後端，預設指向 `127.0.0.1:2022`（原生端口）。

**若平台跑在 Docker**（host `882→容器 2022`），`2022` 在 host 不可達，必須用 `SRS_PLATFORM` 指到映射端口，否則請求 404：

```
SRS_PLATFORM=http://127.0.0.1:882 npm start
```

---

## 3. 連線真實後端

後端可以是：

1. **Docker 容器**（最常見）：compose 部署的平台，host 通常映射 `882`。用 `-Platform http://127.0.0.1:882` 或讓腳本自動偵測。
2. **原生運行**：`make -j -C platform` 後平台監聽 `2022`。腳本自動偵測會先試到它。

> 偵測方式是打 `GET /api/v1/versions`，有回 JSON 代表後端活著。

**登入：** dev server 起來後，用平台的管理密碼登入（密碼在 `/data/config/.env`，或容器 `docker exec oryx redis-cli hget SRS_PLATFORM_SECRET token`）。

---

## 4. Playwright 端到端調試

### 4.1 啟動環境

先確保：
1. 平台後端在跑（Docker 或原生）
2. Vite dev server 在跑（見第 2 節）

### 4.2 方式 A：Playwright 獨立工具（推薦）

本專案開發環境有 Playwright MCP 工具，可直接對 dev server 做瀏覽器操作：

```
# 在 opencode / 支援 MCP 的環境中
playwright_browser_navigate  http://localhost:3000/mgmt/zh/routers-scenario
playwright_browser_snapshot  # 看目前頁面的可訪問性快照
playwright_browser_click     # 點擊元素
playwright_browser_evaluate  # 執行 JS（檢查 DOM / 拿 console）
playwright_browser_console_messages  # 看 console 錯誤
```

**調試流程範例：**
1. 開啟頁面 → 確認 **0 console errors**（`console_messages`）
2. 操作要測的功能 → 每次操作後看 console 有沒有噴錯
3. 用 `evaluate` 檢查關鍵 DOM 狀態（例如登入後 nav 是否有正確文字）
4. 有問題時，`evaluate` 撈 `document.querySelector(...)` 的實際值對照預期

### 4.3 方式 B：程式化 Playwright（寫成測試）

若要寫可重跑的 E2E 測試，在 `ui/` 安裝 Playwright：

```bash
cd ui
npm install -D @playwright/test
npx playwright install chromium
```

寫 `ui/e2e/<名>.spec.js`：

```js
const {test, expect} = require('@playwright/test');

test('虛擬直播頁載入', async ({page}) => {
  await page.goto('http://localhost:3000/mgmt/zh/routers-scenario?tab=vlive');
  await expect(page.getByText('新增虚拟直播')).toBeVisible();
  const errors = [];
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  expect(errors).toEqual([]);
});
```

執行：

```bash
cd ui
npx playwright test
```

### 4.4 注意事項

- **先登入**：多數頁面要登入才有資料。可以在測試前置步驟登入，或用「保持登入的 session」。
- **不要在 localhost 測 WebRTC**：WHEP/WHIP 不支援 localhost（後端會擋），要用內網/公網 IP。
- **HMR 會干擾**：Playwright 連到 dev server 時，改程式碼觸發 HMR 可能讓頁面重載。要穩定測試建議 build 後測產物（見第 6 節）。

---

## 5. 無後端的純前端調試

有些時候後端不可用（例如只想調排版、看元件長相）。有兩種做法：

### 5.1 播放器頁面：preview mode

播放器頁（`platform/containers/www/players/*.html`）支援 **file:// preview**：

直接雙擊開 `players/srs_player.html`（或任何一個），頁面偵測到 `file:` 協定自動進 preview mode：
- 顯示橘色「Preview」橫幅
- 載入公開測試影片（Big Buck Bunny HLS）
- 跳過所有後端 API 呼叫，**0 console error**

> 詳見 [播放器頁面排版指南](player-layout-guide.md)。

### 5.2 React 頁面：mock 或佔位

React 頁面（`/mgmt/*`）需要後端 envs 才能初始化，純前端無法直接跑。若要只看元件長相：
- 臨時在 `SrsEnvContext` 提供 mock env（改 `App.js` 的 `setEnv`）
- 或直接 mock `useTranslation` / axios（見 `src/pages/SrsConsole.test.js` 的寫法）
- 但不建議常駐 mock，正式開發還是連真實後端

---

## 6. 驗證產物（build 後測試）

dev server 有 HMR 干擾時，用產物測試更接近上線行為：

```bash
cd ui
npm run build          # 產生 dist/
npx serve dist         # 用靜態伺服器（或 vite preview）
```

`vite preview` 預設也支援 proxy（沿用 vite.config），可直接連後端：

```bash
npx vite preview --port 3000
```

---

## 7. 快速驗證命令

| 命令 | 作用 | 適合 |
|---|---|---|
| `bash scripts/check-ui-syntax.sh` | 快速語法驗證（JSON + JSX parse） | 改檔後快速確認沒爆 |
| `npm run lint` | ESLint（eslint9 平面配置） | commit 前 |
| `npm test` | vitest 單元測試 | commit 前 |
| `npm run build` | 完整建置 | 確認能出產物 |
| `npx vite preview` | 跑產物 | 接近上線驗證 |

---

## 8. 常見調試手法

### 8.1 看 Console 錯誤

任何頁面載入後，第一件事確認 **0 console errors**。常見錯誤與原因：

| Console 錯誤 | 原因 |
|---|---|
| `AxiosError: Network Error` | dev server 沒跑，或 `SRS_PLATFORM` 指錯（後端連不上） |
| `404` 在 `/terraform/...` | `SRS_PLATFORM` 沒設對（Docker 時 2022 不可達） |
| `[vite] ... URI malformed` | dev server 起不來：index.html 佔位符問題 |
| React key warning | map 沒給 key 或 key 重複 |

> **`AxiosError: Network Error` 特別說明**：這不是程式 bug，是瀏覽器發的請求沒到伺服器。最常見是 **dev server 關了**（後台 task 被殺）。重啟 dev server 即可。

### 8.2 HMR 與快取

- 改 React 程式碼 → 頁面熱更新（不用重載）
- 改 CSS → 即時套用
- 改了 `locale_*.json` → 熱更新（localeLoader 用 import.meta.glob，vite 會偵測新檔）
- 若改了看不到效果，先 `Ctrl+Shift+R` 強制刷新（避免瀏覽器快取舊 bundle）

### 8.3 檢查網路請求

DevTools → Network 可看 `/api` `/terraform` 請求是否到正確後端、回什麼。這對追「後端資料沒載入」很有用。

---

## 9. 播放器頁面（非 React）

播放器頁（`players/*.html` 與 `tools/player.html`）是純 HTML/JS，不走 Vite。調試方式：

| 方式 | 說明 |
|---|---|
| **file:// preview mode** | 直接開 html 檔，自動進 preview 模式，適合調排版 |
| **dev server proxy** | 從 `http://localhost:3000/players/srs_player.html` 存取（vite proxy 轉發），可連後端 |
| **直連後端** | 若平台在 `192.168.0.102:882`，直接開 `http://192.168.0.102:882/players/srs_player.html` |

> 詳見 [播放器頁面排版指南](player-layout-guide.md)。
