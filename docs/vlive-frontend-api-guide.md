# 虛擬直播（vLive）前後端接口指南

> 對象：要改 vLive 功能、新增平台、或理解前後端怎麼串的維護者。
> 背景：vLive（虛擬直播）用 FFmpeg 把影片檔/串流轉成直播流，推到 Oryx 或其他平台。2026-08 已將「預設塞滿 vLiveLimit 個空平台」的舊邏輯，改成與多平台轉播（ScenarioForward）一致的模式：**只保留 3 個內建平台（wx/bilibili/kuaishou），自訂平台由使用者按「新增/刪除」動態管理**。

---

## 1. 後端接口一覽

後端在 `platform/virtual-live-stream.go`（`VLiveWorker.Handle`），全部掛在 `/terraform/v1/ffmpeg/vlive/*` 下，均需 `Authorization: Bearer <token>`（`middlewareAuthTokenInBody`）。

| 接口 | 方法 | 用途 | 前端呼叫處 |
|---|---|---|---|
| `/secret` | POST | **查詢/新增/更新/刪除**配置（`action` 決定） | `ScenarioVLive.js` |
| `/streams` | POST | 查詢所有 vLive 串流狀態（含 FFmpeg pid/frame/時間） | `ScenarioVLive.js` |
| `/streamUrl` | POST | 把串流 URL 正規化（`RebuildStreamURL`） | `VideoSourceSelector.js` |
| `/stream-url` | POST | 同上（另一別名） | `VideoSourceSelector.js` |
| `/ytdl` | POST | 用 youtube-dl 下載影片作為影片源 | `VideoSourceSelector.js` |
| `/server` | POST | 指定伺服器端檔案作為影片源（移動檔案到 `containers/data/vlive/`） | `VideoSourceSelector.js` |
| `/source` | POST | 確認/設定影片源（檔案/串流）並回傳 FFprobe 資訊 | `VideoSourceSelector.js` |
| `/upload/<filename>` | POST | 上傳本機檔案（multipart） | `FileUploader.js` |

## 2. `/secret` 接口的 action 語義

請求 body：

```json
{
  "action": "update | delete",      // 省略或空 = 查詢
  "platform": "wx | bilibili | kuaishou | vlive-xxxx-xxxx",
  "server": "rtmp://xxx",           // update 才需要
  "secret": "...",                  // update 才需要
  "enabled": true,
  "custom": true,
  "label": "我的直播",
  "files": [{ ... }]                // update 才需要
}
```

| action | 行為 | 限制 |
|---|---|---|
| 空 | 回傳全部配置的 kv map（`SRS_VLIVE_CONFIG` hash） | 無 |
| `update` | 合併更新指定 platform 的配置，重啟對應 FFmpeg task | 需 `server`+`files`；platform 必須是內建三平台或 `vlive-` 開頭 |
| `delete` | 從 Redis 刪除配置 + `RemoveTask` 停止 FFmpeg | **只允許 `vlive-` 開頭的自訂平台**，內建平台不可刪 |

**內建平台保護：** `delete` 會檢查 `strings.Contains(platform, "vlive-")`，非自訂平台回 `invalid platform`。

## 3. 前端頁面結構（ScenarioVLive.js）

| 元件 | 作用 |
|---|---|
| `ScenarioVLive` | 掛載時查詢 `/secret`（空 action），決定預設展開哪個 accordion |
| `ScenarioVLiveImpl` | 主要 UI：把 kv 配置轉成 accordion 陣列 |
| `updateSecrets` | 送 `update`（開始/停止直播） |
| `addVLive` | 點「新增虛擬直播」→ 產生新的 `vlive-<time>-<rand>` 空配置加到陣列 |
| `removeVLive` | 送 `delete` → 成功後從陣列移除該配置 |

### 配置載入邏輯（重點）

```js
// 內建三平台（wx/bilibili/kuaishou）固定存在
const confs = [{platform:'wx'...}, {platform:'bilibili'...}, {platform:'kuaishou'...}];

// 只載入「已存在」的自訂平台，不預建空槽
const customs = Object.values(defaultSecrets)
  .filter(e => e.platform.indexOf('vlive-') === 0)
  .sort((a, b) => a.platform.localeCompare(b.platform));
customs.forEach(e => confs.push({...e, index: String(index++), allowCustom: false}));
```

**不要**再用 `while (confs.length < env.vLiveLimit)` 生成空配置 — 這會塞滿幾千個空 accordion。

## 4. 前端如何擴展

### 新增一個自訂平台（使用者操作）
1. 點「新增虛擬直播」→ `addVLive()` 產生 `vlive-<time>-<rid>` 配置
2. 填伺服器/密鑰/選影片源
3. 點「开始直播」→ `updateSecrets(e,'update',...)` 持久化並啟動

### 移除一個自訂平台
1. 該配置展開後有「删除」按鈕（只在 `platform.indexOf('vlive-') === 0` 時顯示）
2. `removeVLive()` → 送 `{action:'delete', platform}` → 成功後 `setConfigs(filter)` 從 UI 移除

### 新增一個內建平台（開發者）
1. 後端：`allowedPlatforms` 加平台名（`virtual-live-stream.go`），並補 `locale` 設定
2. 前端：`ScenarioVLiveImpl` 的 `confs` 陣列開頭加一個物件（仿 wx/bilibili/kuaishou）
3. i18n：`locale_zh.json` / `locale_en.json`（`resources/` 下每語言一檔）的 `plat.<name>.title/link/link2` 加中英文

### 新增動作（如「暫停」）
1. 後端：`allowedActions` 加 `"pause"`，在 `/secret` 的 `if action == "update"` 鏈加分支
2. 前端：`updateSecrets` 的 action 參數傳新值；按鈕文案在 `locale_zh.json`/`locale_en.json` 的 `plat.com.*` 或 `vle.*` 加

## 5. i18n 文案位置

| key | 用途 |
|---|---|
| `plat.wx.*` / `plat.bl.*` / `plat.ks.*` | 內建三平台的標題、教學連結 |
| `plat.com.*` | 共用欄位標籤（名稱/伺服器/密鑰/狀態等） |
| `vle.tip` / `vle.s3` | 頁面提示 |
| `vle.add` / `vle.remove` / `vle.removeConfirm` / `vle.defaultLabel` / `vle.limit` / `vle.deleted` | 新增/刪除自訂平台相關（2026-08 新增） |

## 6. 與多平台轉播（ScenarioForward）的對照

vLive 的 UI 模式完全是 ScenarioForward 的鏡像，兩者共用相同的「內建平台 + 動態自訂平台」架構：

| | vLive | Forward |
|---|---|---|
| 自訂平台前綴 | `vlive-` | `forwarding-` |
| 新增函數 | `addVLive` | `addForwarding` |
| 刪除函數 | `removeVLive` | `removeForwarding` |
| 後端 delete 檢查 | `strings.Contains(p, "vlive-")` | `strings.Contains(p, "forwarding-")` |
| 內建平台 | wx/bilibili/kuaishou | 同 |

改其中一個平台管理邏輯時，建議同步檢查另一個，保持一致。
