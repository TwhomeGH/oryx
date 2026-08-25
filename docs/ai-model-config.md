# AI 服務擴展性指南（自訂模型與本地 LLM）

> 對應本 fork 改造：讓 Oryx 的 AI 功能可以接 OpenAI 以外的相容服務——本地 LLM
> （Ollama、LM Studio、vLLM）或第三方平台（Groq、DeepSeek、one-api 閘道等）。

---

## 1. 先講結論

| 能力 | 支援狀態 |
|---|---|
| 自訂服務器地址（BaseURL） | ✅ 原本就有——每個 AI 功能的設定都有「接入地址」欄位 |
| 自訂聊天模型 | ✅ 原本就有——直播間助理/配音等各有模型設定 |
| **自訂 ASR 語音辨識模型** | ✅ **本次新增**——原本寫死 `whisper-1` |
| **連線預檢容錯** | ✅ **本次新增**——伺服器不實作 `/models` API 不再擋功能 |

---

## 2. 本次新增了什麼

### ASR 模型可配置

| 功能 | 配置位置 | 欄位 |
|---|---|---|
| AI 字幕（轉錄） | 場景 → AI 字幕 → 服務提供商 | `model`（預設 `whisper-1`）＋ `chatModel`（連線檢查用，預設 `gpt-3.5-turbo`） |
| 直播間語音助理 | 直播間設定的 AI 助理 | `aiAsrModel`（預設 `whisper-1`） |
| 視頻翻譯/配音 | 專案的 ASR 設定 | `aiAsrModel`（預設 `whisper-1`） |

留空一律使用原本的 `whisper-1`，行為完全向下相容。

### 連線預檢容錯

AI 設定的「測試連接」原本會強制查詢 `/v1/models/whisper-1`，
很多 OpenAI 相容伺服器（特別是本地推理框架）不實作 models API，
導致功能明明可用卻被預檢擋住。現在：

- 查不到模型只記**警告日誌**，不再中斷
- 「測試連接」仍會用你設定的聊天模型做一次真實對話測試（這步失敗才報錯）

---

## 3. 接上本地 Ollama 的完整範例

假設 Ollama 跑在 `http://192.168.0.200:11434`，且已拉取模型：

```bash
ollama pull llama3.2          # 聊天模型
# ASR 需要 OpenAI 相容的轉錄端點，例如搭配 faster-whisper-server：
# docker run -p 8000:8000 fedirz/faster-whisper-server:latest
```

在 Oryx 的 AI 功能設定中填入：

| 欄位 | 值 |
|---|---|
| 接入地址 (BaseURL) | `http://192.168.0.200:11434/v1` |
| 密鑰 | Ollama 不驗證，隨意填（如 `ollama`） |
| 聊天模型 | `llama3.2` |
| ASR 模型 | `faster-whisper-small`（依你的轉錄服務而定） |

其他常見端點：

| 服務 | BaseURL |
|---|---|
| LM Studio | `http://<host>:1234/v1` |
| vLLM | `http://<host>:8000/v1` |
| Groq | `https://api.groq.com/openai/v1` |
| DeepSeek | `https://api.deepseek.com/v1` |
| one-api / new-api 閘道 | `http://<host>:3000/v1` |

---

## 4. 邊界與限制

1. **僅支援 OpenAI 相容格式**——原生 Anthropic/Gemini 協議不在範圍內，
   請透過 one-api 這類閘道轉換
2. **語音辨識需要 `/v1/audio/transcriptions` 端點**——純聊天框架（如裸 Ollama）
   沒有此端點，字幕/語音助理功能需搭配 whisper 相容服務
3. 模型名稱由服務端決定，填錯會在實際調用時報錯（測試連接的聊天測試可提前發現）

## 5. 實作位置（後續維護參考）

| 檔案 | 變更 |
|---|---|
| `platform/transcript.go` | TranscriptConfig 加 `Model`/`ChatModel`；預檢容錯；worker 用配置模型 |
| `platform/ocr.go` | 預檢容錯改用配置的 chat model |
| `platform/dubbing.go` | 配音 ASR 使用專案設定的 `AIASRModel` |
| `platform/live-room.go` | SrsAssistantASR 加 `AIASRModel` 欄位 |
| `platform/ai-talk.go` | openaiASRService 支援 model 注入；Stage 從房間讀取 |
| `ui/src/components/OpenAISettings.js` | 可選的 ASR 模型輸入框（有傳 setModel 才顯示） |
| `ui/src/pages/ScenarioTranscript.js` | 接入 model/chatModel 狀態與保存 |
