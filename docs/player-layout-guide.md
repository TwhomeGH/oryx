# 播放器頁面排版與 CSS 修改指南

> 對象：要調整內建播放器頁面排版、顏色、間距的維護者。
> 目標：讀完這篇就能快速定位要改哪個 CSS 區塊，並在本地即時預覽。

---

## 1. 檔案總覽

```
platform/containers/www/
├── players/
│   ├── css/player.css          ← 所有 player 頁面的共享設計系統（唯一要改的 CSS）
│   ├── srs_player.html         ← 主播放器（HTTP-FLV / HLS / DASH）
│   ├── rtc_player.html         ← WebRTC 播放器
│   ├── whip.html               ← WHIP 推流
│   ├── whep.html               ← WHEP 播放器
│   ├── rtc_publisher.html      ← WebRTC 推流（SRS SDK）
│   ├── pushdiag.html           ← 推流診斷工具（FLV + WebRTC 兩種模式）
│   └── js/
│       ├── srs.sdk.js          ← WebRTC SDK（已修復安全問題）
│       ├── srs.page.js         ← 頁面初始化（query string → 預設 URL，純 vanilla JS）
│       ├── winlin.utility.js   ← 工具函數（已修復 prototype pollution）
│       └── adapter-7.4.0.min.js ← WebRTC adapter
└── tools/
    ├── player.html             ← 嵌入式播放工具（引用 ../players/css/player.css）
    └── .gitkeep
```

**重點：所有 player 頁面共用同一個 `css/player.css`，改一個檔案全部生效。**

> **歷史清理：** 舊版 jQuery/Bootstrap/swfobject/json2.js 及舊頁面（index.html、srs_chat、vlc、srs_publisher、xgplayer 等）已於 2026-08 移除。這些是上游殘留的舊式網頁，入口僅是 redirect 到現代化頁面，且引用的舊庫是 CodeQL 告警來源。若看到某個頁面報 JS 錯誤，先確認它是否引用 `js/` 下不存在的檔案。

---

## 2. 本地測試（不需要 Docker）

每個 player 頁面都有 **Preview Mode**，直接用瀏覽器開啟 `file://` 即可：

```
file:///F:/oryx/platform/containers/www/players/srs_player.html
file:///F:/oryx/platform/containers/www/players/whip.html
file:///F:/oryx/platform/containers/www/players/whep.html
file:///F:/oryx/platform/containers/www/players/rtc_player.html
file:///F:/oryx/platform/containers/www/players/rtc_publisher.html
file:///F:/oryx/platform/containers/www/tools/player.html
```

**Preview Mode 行為：**
- 偵測 `window.location.protocol === 'file:'` 自動啟動
- 顯示橙色 "Preview" 橫幅
- 載入公開 HLS 測試影片（Big Buck Bunny）
- 跳過所有後端 API 呼叫，不會有 JS 報錯

**測試流程：**
1. 用瀏覽器開啟頁面
2. 開啟 DevTools（F12）修改 CSS
3. 滿意後將改動寫入 `player.css`
4. 重新整理頁面確認效果

---

## 3. CSS 架構（player.css）

### 3.1 CSS 變數（`:root`）

全域設計 token，改這裡會影響所有元件：

```css
:root {
    --bg-primary: #0f1117;      /* 頁面背景 */
    --bg-secondary: #1a1d27;    /* 卡片/導覽列背景 */
    --bg-tertiary: #242836;     /* 輸入框/tips 背景 */
    --bg-hover: #2d3245;        /* hover 狀態背景 */
    --text-primary: #e4e7f1;    /* 主要文字 */
    --text-secondary: #9ca3b8;  /* 次要文字（label、placeholder） */
    --text-muted: #6b7394;      /* 頁尾等弱化文字 */
    --accent: #a2a4ff;          /* 主題色（按鈕、連結、focus 光暈） */
    --accent-hover: #b0b8ff;    /* 主題色 hover */
    --accent-glow: rgba(99,102,241,0.25); /* focus 光暈 */
    --border: #2d3245;          /* 邊框顏色 */
    --success: #22c55e;         /* 成功綠 */
    --warning: #f59e0b;         /* 警告橙 */
    --danger: #ef4444;          /* 錯誤紅 */
    --radius: 8px;              /* 小圓角 */
    --radius-lg: 12px;          /* 大圓角（卡片、播放器） */
    --shadow: 0 4px 24px rgba(0,0,0,0.3); /* 陰影 */
    --transition: 0.2s ease;    /* 過渡動畫 */
}
```

### 3.2 區塊對照表

| CSS 選擇器 | 控制的元件 | 常見修改 |
|---|---|---|
| `.navbar` / `.navbar-inner` | 頂部導覽列 | 高度、間距、背景模糊 |
| `.navbar-nav a` | 導覽連結 | 字大小、顏色、hover 效果 |
| `.container` | 頁面內容最大寬度 | `max-width`（影響整體佈局） |
| `.card` | 表單 + 播放器容器 | `padding`、`border-radius` |
| `.player-wrapper` | 影片播放器外框 | `max-height`（影響播放器高度） |
| `.player-wrapper video` | 影片元素 | `object-fit`（contain/cover） |
| `.form-group` / `.form-label` | 表單群組與標籤 | 間距、字大小 |
| `.form-input` | 文字輸入框 | 背景色、邊框、focus 效果 |
| `.form-row` | 輸入框 + 按鈕橫排 | 間距、響應式斷點 |
| `.btn` / `.btn-primary` | 按鈕 | padding、字大小、hover |
| `.alert` / `.alert-info` / `.alert-danger` | 資訊/錯誤橫幅 | 背景色、邊框色 |
| `.share-link` | 分享連結區塊 | 間距、字大小 |
| `.session-info` | WebRTC session 資訊 | 佈局（flex gap） |
| `.tips` / `.tips ul` | 底部提示區塊 | 格線佈局 |
| `.footer` | 頁尾 | 間距、字大小 |
| `.checkbox-label` | 核取方塊標籤 | 間距、字大小 |
| `.hidden` | 隱藏元素 | `display: none !important` |
| `.mt-16` `.mb-8` `.ml-16` | 間距工具類別 | 對應 margin 值 |
| `.preview-banner` | 本地預覽橫幅 | 顏色、文字 |

### 3.3 響應式斷點

```css
@media (max-width: 768px) {
    .navbar-inner { gap: 16px; }
    .navbar-nav { gap: 2px; }
    .navbar-nav a { padding: 6px 10px; font-size: 13px; }
    .form-row { flex-direction: column; }  /* 輸入框+按鈕改為直排 */
    .container { padding: 16px; }
}
```

---

## 4. 常見修改範例

### 改播放器最大高度

`player.css` 找到 `.player-wrapper`：

```css
.player-wrapper {
    max-height: 70vh;   /* 改這裡，例如 50vh 或 600px */
}
```

### 改整體配色

修改 `:root` 的 CSS 變數：

```css
:root {
    --accent: #10b981;        /* 改成綠色主題 */
    --accent-hover: #34d399;
}
```

### 改卡片內距

```css
.card {
    padding: 32px;   /* 原本 24px，加大內距 */
}
```

### 改導覽列高度

```css
.navbar-inner {
    height: 64px;   /* 原本 56px */
}
```

### 新增一個工具類別

在 `player.css` 的 `/* Utility Classes` 區塊加入：

```css
.my-custom { margin: 16px auto; max-width: 800px; }
```

然後在 HTML 中使用：`<div class="my-custom">...</div>`

---

## 5. 各頁面結構差異

| 頁面 | 特有元件 | 使用的 JS SDK |
|---|---|---|
| `srs_player.html` | 分享連結、多格式播放（FLV/HLS/DASH/MP4） | hls.js, mpegts.js, dash.js |
| `rtc_player.html` | session info（SessionID、Simulator） | srs.sdk.js（SrsRtcPlayerAsync） |
| `whip.html` | Video Only / Audio Only 核取方塊 | srs.sdk.js（SrsRtcWhipWhepAsync） |
| `whep.html` | 同 WHIP，但用於播放 | srs.sdk.js（SrsRtcWhipWhepAsync） |
| `rtc_publisher.html` | codec 資訊（Audio/Video） | srs.sdk.js（SrsRtcPublisherAsync） |
| `pushdiag.html` | 推流診斷工具（FLV + WebRTC 模式） | mpegts.js + Chart.js + srs.sdk.js |
| `tools/player.html` | 極簡版，無導覽列 | hls.js, mpegts.js |

---

## 6. 推流診斷工具（pushdiag.html）

> 獨立工具頁面，用於分析推流品質，不是播放器本身。

### 6.1 兩種模式

| 模式 | 分析對象 | 診斷項目 |
|---|---|---|
| **FLV 分析** | HTTP-FLV 串流（`/live/<stream>.flv`） | 位元率、GOP 間隔、影音時間戳偏移、掉幀、封包檢查器、SPS/AAC 解析、原始位元組檢視 |
| **WebRTC 分析** | WHEP URL（`/rtc/v1/whep/`） | ICE 型別（host/srflx/relay）、RTT、封包遺失、jitter、掉幀率、codec（`getStats()`） |

### 6.2 使用方式

1. 從任一 player 頁面的導覽列點 **Push Diag**
2. **FLV 模式**：填入 SRS HTTP 主機 + stream key → 開始分析
3. **WebRTC 模式**：填入 WHEP URL → 開始分析
4. 健康診斷會自動給出旗標（正常/警告/錯誤）

### 6.3 診斷 CSS（加到 player.css）

pushdiag 用到的一組診斷專用類別，全部在 `player.css` 的 `/* Diagnostics */` 區塊：

| 類別 | 用途 |
|---|---|
| `.diag-full` | 全寬容器（覆蓋 sidebar flex 佈局） |
| `.mode-tabs` / `.mode-tab` | 模式切換按鈕 |
| `.diag-grid` / `.cols-3` / `.cols-2` / `.cols-3-2` | 響應式格線 |
| `.stat-card` / `.stat-value` | 數據卡片 |
| `.badge` / `.badge-ok` / `.badge-warn` / `.badge-err` / `.badge-info` | 健康旗標 |
| `.diag-table-wrap` / `.diag-table` | 診斷表格 |
| `.chart-box` | Chart.js 圖表容器 |
| `.hud` / `.hud-label` / `.hud-val` | 影片疊加資訊 |
| `.modal-overlay` / `.modal` | 原始位元組檢視彈窗 |
| `.parse-box` / `.parse-row` | 封包結構解析 |
| `.text-ok` / `.text-warn` / `.text-err` / `.text-info` / `.text-dim` | 文字顏色 |
| `.collapse-trigger` | 可收合區塊標題 |

---

## 7. 注意事項

- **所有 player 頁面共用 `player.css`**，改一個檔案全部生效
- **不要用 inline style**（`style="..."`），改用 CSS class
- **工具類別**（`.hidden` `.mt-16` 等）適用於所有頁面
- **Preview Mode** 只在 `file://` 協議下啟動，正式部署時不會顯示
- 修改 CSS 後不需要重構 Docker 映像，直接改檔案重新整理即可
