# 前端組件與頁面維護指南

> 對象：未來要改 Oryx UI 排版、加欄位、加頁面的維護者。
> 目標：讀完這篇不用再反覆挖代碼，就知道每種改動要動哪幾個檔案。

---

## 1. 目錄結構

```
ui/
├── index.html              # Vite 進入模板（含 %PUBLIC_URL% 等佔位符，勿手改值）
├── vite.config.mjs         # 建置設定（佔位符替換、JSX loader、dev proxy、vitest）
├── eslint.config.mjs       # ESLint 9 平面配置
├── package.json            # 依賴與腳本（lint/test/build）
├── public/                 # favicon、manifest 等靜態檔，原樣複製到產物根目錄
└── src/
    ├── index.js            # React 入口
    ├── App.js              # 所有路由（<Route path="routers-xxx">）
    ├── Navigator.js        # 頂部選單（eventKey 清單＋主題切換）
    ├── utils.js            # Token、Locale、Tools 等共用工具
    ├── resources/          # locale_zh.json / locale_en.json（每語言一檔）與圖片、預設 md
    ├── components/         # 跨頁面共用元件（SrsQRCode、SecretInput、ThemeSwitch…）
    └── pages/              # 一個路由一個檔案
        └── SrsConsole.js   # SRS 控制台（Overview/Vhosts/Streams/Clients/Configs）
```

## 2. 頁面標準骨架（照抄即可）

```jsx
export default function Contact({onInit}) {
  return (
    <SrsErrorBoundary>          {/* 錯誤邊界，防單頁崩潰白屏 */}
      <ContactImpl onInit={onInit} />
    </SrsErrorBoundary>
  );
}

function ContactImpl({onInit}) {
  const {t} = useTranslation();                    // 多語系
  const language = useSrsLanguage();               // 目前語言 zh/en
  const handleError = useErrorHandler();           // axios 錯誤交給全局邊界
  ...
}
```

規則：對外導出的只有 Wrapper；Impl 放所有 hooks 與 JSX。

## 3. 新增一個頁面的 Checklist

| # | 檔案 | 動作 |
|---|---|---|
| 1 | `src/pages/MyPage.js` | 照第 2 節骨架建立 |
| 2 | `src/App.js` | `<Route path="routers-mypage" element={<MyPage/>}/>` |
| 3 | `src/pages/Navigator.js` | navs 陣列加 `{eventKey:'N', to:'/routers-mypage', text: t('nav.mypage')}` |
| 4 | `resources/locale.json` | `nav` 區塊加 mypage；頁面自己的文案建一個 `"mypage": {...}` 區塊 |
| 5 | 驗證 | `bash scripts/check-ui-syntax.sh` 或直接 `vite build` |

## 4. 排版系統（react-bootstrap）

Components.js 是最佳範例：

```jsx
<Container fluid className="pt-3">
  <Row className="g-3">                        {/* g-3 統一格距 */}
    <Col xs={12} md={6} xl={3}>                {/* 響應式：手機全寬→平板2列→桌面4列 */}
      <Card className="h-100">                 {/* h-100 讓同排卡片等高 */}
        <Card.Header>標題</Card.Header>
        <Card.Body className="d-flex flex-column">
          <Card.Text as="div">內容</Card.Text>
          <Button className="mt-auto">按鈕釘底</Button>
        </Card.Body>
      </Card>
    </Col>
  </Row>
</Container>
```

禁忌：不要用固定 `style={{width: '18rem'}}`（會破壞響應式）；文字過長的容器加 `wordBreak: 'break-all'`。

## 5. 表單欄位模式

### 一般欄位（非受控，頁面慣例）

```jsx
<Form.Group className="mb-3">
  <Form.Label>{t('xxx.label')}</Form.Label>
  <Form.Text> * 說明文字</Form.Text>
  <Form.Control as="input" defaultValue={value}
    onChange={(e) => updateConfigObject({...conf, field: e.target.value})} />
</Form.Group>
```

### 金鑰／密碼欄位

一律用 `<SecretInput value onChange/>`（components/SecretInput.js），自動遮蔽＋眼睛切換。

### 場景頁的多配置更新

場景頁把多組配置放在 `configs` 陣列，改動透過 `updateConfigObject({...conf, key: v})` 
以 platform 為 key 回寫陣列，「儲存」時一次 POST。

## 6. 場景頁的分區切換

ScenarioTranscript.js 等頁用 `configItem` state 控制 Card.Body 分區顯示：
provider / asr / overlay / webvtt。新增分區＝加一個 `{configItem === 'xxx' && <Card.Body>}` 
＋導覽連結。

## 7. 多語系（拆分成多檔，自動掃描）

結構：每個語言一個檔 `resources/locale_<code>.json`（2026-08 起，取代舊的單一 `locale.json`）：

```json
// ui/src/resources/locale_zh.json
{
  "meta": { "code": "zh", "name": "简体中文" },
  "translation": { ...功能區塊... }
}
```

- **載入方式**：`src/localeLoader.js` 用 `import.meta.glob("./resources/locale_*.json", {eager: true})` 自動掃描資料夾 → 組出 i18next 的 `resources` 與語言列表 → `i18n.js` / `App.js` / `setupTests.js` / `LanguageSwitch.js` / `Popouts.js` 都從它取用。
- **目前語言**：`locale_zh.json`（簡中）、`locale_en.json`（英文）、`locale_ja.json`（日文，2026-08 新增，698 key 全翻譯）
- **新增語言 = 放一個檔**：在 `resources/` 加 `locale_ja.json`（含 `meta.code/name`），下拉選單自動出現，**不用改任何程式碼**。`meta.code` 決定 URL 的語言前綴（如 `/mgmt/ja/`）。
- 取值：`t('transcript.trans0')`；巢狀可用 `t('transcript.codec.balanced')`
- **加 key 必須每種語言都加**，否則某語言顯示 key 名（i18next fallback）
- **更新翻譯檔後跑測試**：`npm test` 會驗證 localeLoader 載入數量正確
- 語言切換器在 `src/components/LanguageSwitch.js`，從 `localeLoader` 的 `locales` 動態渲染（純文字，無國旗 icon）

### 7.1 翻譯維護規範

> 維護者母語是中文，對其他語言可能不熟。若你發現翻譯錯誤、用詞不當，或想新增語言支持，遵循以下流程。

**翻譯品質原則：**
- **每種語言都對照 `locale_zh.json` 當基準** — zh 是最準的（維護者母語），改 key 先改 zh，再同步其他語言
- **一致性優先**：技術名詞（如「推流」「轉碼」「碼率」）全站統一，不要同義詞混用
- **不要逐字直譯**：意譯優先，符合該語言的習慣用語（例如日文「推流」常用「配信」）
- **URL / 技術格式不要改**：`/terraform/...`、`https://...`、`gpt-4o`、`rtmp://` 等保持原樣，只翻譯顯示文字

**發現翻譯錯誤時的處理：**
1. 打開對應 `resources/locale_<lang>.json`，找到錯誤 key
2. 對照 `locale_zh.json` 同名 key 確認原意
3. 直接修正該 key 的翻譯（不需要動 zh）
4. 若有疑問（原文本身含糊），開一個 issue 或直接問維護者

**新增語言支持（三步）：**
1. 複製 `locale_zh.json` 成 `locale_<code>.json`
2. 改 `meta.code`（如 `ja`）與 `meta.name`（如 `日本語`）
3. 逐 key 翻譯 `translation` 區塊；翻譯不全的 key 會顯示 key 名（fallback），可先翻常用區塊再補

**已知翻譯現況（2026-08）：**
- `en`：與 zh 同時維護，結構同步
- `ja`：698 key 全量翻譯（2026-08 加入）。若發現用詞不自然，歡迎指正修正
- **注意**：`zh` 是維護者母語、最可信的基準。`ja` 由 AI 初譯 + 人工審查，仍可能有中文語感的殘留（例如誤把「内網/公網」直接搬進日文，正確是「プライベートIP/パブリックIP」）。發現這類問題時直接修該 key 即可，不需動 zh。

> **歷史：** 舊版是單一 `locale.json`（zh/en 塞一起）+ LanguageSwitch 寫死兩組。2026-08 重構成多檔動態載入，並移除 `country-flag-icons` 依賴（語言切換不應與國家旗幟綁定）。

## 8. 資料流：query / apply / check 三兄弟

每個場景的後端都是同一套路：

| 端點 | 用途 | 前端呼叫時機 |
|---|---|---|
| `/terraform/v1/xxx/query` | 讀目前配置 | 進頁面載入 |
| `/terraform/v1/xxx/apply` | 存配置並重啟 worker | 按「套用」 |
| `/terraform/v1/xxx/check` | 測試外部服務連通性 | 按「測試連接」 |

前端統一帶 `headers: Token.loadBearerHeader()`。後端 handler 範本見 service.go 
`handleMgmtStatus`（middlewareAuthTokenInBody + ParseBody + WriteData）。

## 9. 能力探測卡片模式（硬體/功能自檢）

「組件頁」的卡片 = 後端探測端點 + 前端徽章渲染。現有兩例：

| 卡片 | 後端 | 探測方式 |
|---|---|---|
| SRS 核心能力 | srs-capability.go | 逐項 `srs -t -c` 解析測試 |
| FFmpeg 編碼能力 | ffmpeg-capability.go | `-encoders` 掃描＋硬編碼實編 0.05 秒驗證，快取 10 分鐘 |

新增探測卡：複製 Components.js 的 FFmpeg 卡結構＋後端照 ffmpeg-capability.go 
樣板（probe 函式回傳 `{name, ok, detail}` 陣列）。**重探測的按鈕要有**，
且避免自動輪詢（每次探測都會 spawn 進程）。

### 9.1 能力探測應用於表單（ScenarioTranscode 範例）

不只是組件頁，**表單下拉也可以用能力探測啟用/禁用選項**。轉碼頁（ScenarioTranscode.js）的「视频编码器」下拉即是一例：

- `CODEC_PRESETS` 列出編碼器（libx264 / nvenc / qsv / vaapi / amf），每個含 `need`（FFmpeg encoder 名）
- 前端 `POST /terraform/v1/mgmt/ffmpeg/capabilities` 取得 `ffCaps.encoders`，找不到或 `ok=false` 的選項 `disabled`
- 選了硬體 preset（如 nvenc）時：**顯示實際 codec 參數**（唯讀 `-c:v h264_nvenc ...`），並隱藏不適用的 profile/preset 下拉
- 「自定义（专家）」選項顯示可編輯輸入框，直接填 FFmpeg 參數

**後端對應：** trancode.go 的 `TranscodeConfig.CodecCustom` — 非空時用 `strings.Fields(codecCustom)` 取代固定 `-vcodec/-profile/-preset/-tune/-bf` 參數，讓硬體編碼器傳自己的旗標（如 `-preset:p p4`）。bitrate 不屬於 codecCustom，統一由 UI 的 `-b:v <bitrate>k` 控制。

## 10. Vite 特殊事項

| 主題 | 說明 |
|---|---|
| `.md` 當字串引入 | `import x from './a.md?raw'`（無 ?raw 會 build 失敗） |
| 環境變數 | 只認 `REACT_APP_*` 前綴（envPrefix 已設）；原始碼僅 `process.env.REACT_APP_LOCALE` 被 define 替換 |
| index.html 佔位符 | `%PUBLIC_URL%`/`%REACT_APP_LOCALE%` 由 vite.config.mjs 的 oryx-html-env 在解析前替換，**勿在 html 裡直接寫死** |
| JSX 在 .js 檔 | esbuild 已配 jsx loader，正常寫即可；但 JSX 文字裡不能出現裸 `=>`，請寫 `=&gt;` |

## 11. 除錯工具與常見炸點

```bash
bash scripts/check-ui-syntax.sh     # 快速語法驗證（不需完整建置）
npx vite build                      # 完整產物
npm run lint                        # ESLint（eslint9 平面配置 eslint.config.mjs）
npm run test                        # vitest
```

歷史炸點備忘：

| 症狀 | 原因 |
|---|---|
| 整站空白，Console 報 `require is not defined` | 原始碼用了 CJS require（Vite 不轉換），改 ESM import |
| build 報 `Duplicate key 'xxx'` | 物件字面量重複 key，ESLint no-dupe-keys |
| build 報 `URI malformed` | index.html 出現裸 `%VAR%` 佔位符未被替換 |
| `The character ">" is not valid inside a JSX element` | JSX 文字裡的裸 `=>`，改 `=&gt;` |
| `Cannot call a namespace ("moment")` | `import * as moment` 後直接調用，改 default import |

## 12. 深色主題

- Bootstrap 5.3 原生支援：`<html data-bs-theme="dark">` 即全域換膚
- 切換元件：components/ThemeSwitch.js（存在 localStorage `oryx-theme`）
- 防閃爍：index.html head 內聯腳本在首繪前套用
- 自訂樣式請盡量用 bootstrap 變數/class，避免寫死白色背景

## 13. SRS 控制台（SrsConsole.js）

取代舊版 AngularJS console（`/console/`，已刪除）。Route: `routers-console`。

**資料來源：** 直接呼叫 SRS HTTP API，經平台 `/api/` proxy（帶 Bearer token）：

| Tab | API | 備註 |
|---|---|---|
| Overview | `/api/v1/summaries` | 回傳包在 `data` 內 |
| Vhosts | `/api/v1/vhosts/` | 列表在頂層（無 `data` 包裝） |
| Streams | `/api/v1/streams/` | 先抓 vhosts join owner 名稱 |
| Clients | `/api/v1/clients/` | Kickoff 用 `axios.delete` |
| Configs | `/api/v1/raw?rpc=raw` | `http_api` 頂層 |

**⚠️ API 回應結構不一致：** SRS HTTP API 的 summaries 包在 `data`，但 vhosts/streams/clients/raw 在頂層。`srsApi()` helper 返回完整 envelope，呼叫端自行取欄位，勿統一 unwrap。

**⚠️ SRS 欄位名稱跟直覺不同：** `/api/v1/streams/` 的 audio 物件用**單數** `channel`（如 `audio.channel: 2`），且**沒有** `stereo` 欄位。`fmtAudio()` 曾誤讀複數 `channels`、並檢查不存在的 `stereo`，導致所有流都顯示成 `mono`（d3fe529 修正）。接 SRS 原生 API 時以實測 JSON 為準，別憑欄位名猜——`/api/v1/versions` 免驗證，可用來快速撈回真實結構。

**輪詢：** Overview/Vhosts/Streams/Clients 每 3 秒更新（`setTimeout` 自重排，非 `setInterval`，避免重疊）。

**Streams FPS 標記：** Streams tab 會對推流中的 stream 每 10 秒最多探測一次
`/terraform/v1/mgmt/streams/fps`，顯示實測 FPS。判斷「波動」時不要假設固定 30fps；前端會為每一路
stream 保存自己的滾動 `baselineFps`（第一次採樣用當前 FPS，之後 `80% 舊基準 + 20% 新採樣`），
tooltip 顯示實測 FPS、基準 FPS 與 jitter。UI 即使標記「波動」也必須保留實測 FPS，例如
`59.9 fps [波動]`，方便判斷 60fps、50fps、25fps、24fps 等不同來源是否真的異常。

**修改提示：** 加 tab → 在 `SrsConsoleImpl` 的 `<Tabs>` 加 `<Tab>`，並在 `locale.json` 的 `console` 區塊補中英文。

### 13.1 本地調試預覽

前端要連到真實平台後端才有資料。兩種方式：

**方式一：一鍵腳本（推薦）**

```powershell
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1                # 預設開場景頁
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page console  # 開控制台
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page "scenario?tab=vlive"  # 開虛擬直播頁
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page settings # 系統配置
powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page players  # 播放器目錄
```

- 自動偵測平台端口（先試原生 `127.0.0.1:2022`，再試 Docker 映射 `882`）
- 自動安裝缺漏的 UI 依賴（esbuild/rollup native module）
- 啟動 Vite dev server（port 3000）並自動開瀏覽器到指定頁面
- `-Page` 可選：`scenario`（預設）/`settings`/`console`/`components`/`contact`/`players`；場景頁可帶 tab（如 `scenario?tab=vlive`）
- 其他參數：`-Platform http://127.0.0.1:882` 指定平台、`-Port 4000` 改端口、`-NoBrowser` 不開瀏覽器
- 新增頁面時記得在 `scripts\dev-server.ps1` 的 `$pageMap` 對照表加一行

**方式二：手動**

```powershell
cd ui
$env:PUBLIC_URL="/mgmt"; $env:REACT_APP_LOCALE="zh"
$env:SRS_PLATFORM="http://127.0.0.1:882"   # 改為你的平台端口
npm start
```

然後開 `http://localhost:3000/mgmt/zh/routers-console`，用平台密碼登入。

**關鍵：`SRS_PLATFORM` 環境變數** — vite.config.mjs 的 dev proxy 預設指向 `127.0.0.1:2022`（原生端口）。若平台跑在 Docker（如 host `882→容器 2022`），`2022` 在 host 不可達，必須用 `SRS_PLATFORM` 指到映射端口，否則 `/api` `/terraform` 請求會 404。

**提醒：** dev 模式 `%PUBLIC_URL%` 由 vite.config.mjs 的 `transformIndexHtml` hook 替換（build 模式走 `transform` hook），兩者都已處理，直接 `npm start` 即可。

> 完整的前端調試/測試流程（Playwright E2E、無後端調試、常見排錯）見 [前端本地調試與測試指南](ui-debugging.md)。
