# 轉推畫質對比（forward-quality-compare）

用同一段時間比較兩個輸出流的畫質，判斷「補強轉碼（normalize）」或不同「輸出碼率」造成多少損失。
適合在調整碼率、開啟/關閉補強轉碼後做前後對照。

## 前置條件

- 主機已安裝 `ffmpeg`（含 `ffprobe`）。
- 兩個可讀取的 HTTP-FLV 輸出網址（通常是本機目標）。
- 建議先用 `forward-quick-test.ps1 -Compare` 產生 copy 與 normalize 兩個目標，拿到兩個網址。

## 用法

```powershell
pwsh scripts/tools/forward-quality-compare.ps1 `
  -A http://localhost:882/live/quick-tgt-12345.flv `
  -B http://localhost:882/live/quick-cmp-12345.flv
```

> `quick-tgt-*` 是 copy、`quick-cmp-*` 是 normalize（`forward-quick-test.ps1 -Compare` 結束時會直接印出這兩個網址）。
> **不要**用 `__forward_*`（內部 program）或 `quick-src-*`（來源）來比對。

參數：

| 參數 | 說明 | 預設 |
| --- | --- | --- |
| `-A` | 參考流（通常是不轉碼的 copy） | 必填（除 `-Source` 外） |
| `-B` | 待比較流（通常是 normalize 或不同碼率） | 必填（除 `-Source` 外） |
| `-Source` | 由工具用這個正式來源（`app/stream`）自動建立 copy/normalize 兩個 `TEST qc` 目標，比對完自動刪除 | 空 |
| `-Seconds` | 取樣秒數（每邊錄製這麼長的影片再做逐幀比對） | `15` |
| `-Out` | 輸出目錄（`A.png`/`B.png` 拼貼圖與 `A.mp4`/`B.mp4`） | `<repo>\.tmp\forward-quality-compare` |

## 由正式來源自動建立對比目標（`-Source`）

```powershell
# 用正在直播的 live/livestream 產生 copy 與 normalize 兩個 TEST 目標，比對後自動刪除
pwsh scripts/tools/forward-quality-compare.ps1 -Source live/livestream -Seconds 20
```

這樣不需要先跑 `forward-quick-test.ps1 -Compare`，直接用你的正式來源做 A/B 對照。

> **安全檢查**：若目前有任何**非 `TEST`** 的轉播目標處於啟用狀態，工具會中止並列出它們，
> 避免測試內容被推到真實平台。請先停用這些目標再重跑。

## 輸出

- `A.png` / `B.png`：各 30 幀（6×5）拼貼圖，肉眼可直接比較
- `A.mp4` / `B.mp4`：錄下的原始片段（除錯用）
- 終端機輸出：對齊後的 PSNR / SSIM（含 Y/U/V 各平面）

## 輸出

1. **PSNR / SSIM**：直接印在終端機（B 相對 A）。
2. **截圖**：`A.png` 與 `B.png`（各取一半時間的一格），可並排肉眼比對。
3. **檔案大小**：兩張 PNG 的位元組數。

範例：

```log
==> 量測 PSNR / SSIM（取 15s）
  [Parsed_psnr_0 @ ...] PSNR y:38.42 u:41.10 v:40.77 average:39.21 min:31.05 max:45.00
  [Parsed_ssim_0 @ ...] SSIM Y:0.9821 U:0.9902 V:0.9887 All:0.9843
```

## 怎麼判讀

| 指標 | 越好代表 | 參考 |
| --- | --- | --- |
| **PSNR** | 越高越好（dB） | > 40 幾乎無感；35–40 仔細看有差；< 30 明顯糊 |
| **SSIM** | 越接近 1 越好 | > 0.98 很好；0.95–0.98 可接受；< 0.95 明顯差異 |

> 注意：兩個流的**時間軸必須大致對齊**（同來源、同時段）。若長度或起點不同，PSNR 會失真。

## 建議流程

```powershell
# 1) 產生 copy 與 normalize 兩個目標（保留設定）
pwsh scripts/tools/forward-quick-test.ps1 -Compare -Keep -NoPlay

# 2) 從輸出找到兩個目標的網址，然後比較
pwsh scripts/tools/forward-quality-compare.ps1 -A <copy-url> -B <normalize-url> -Seconds 20

# 3) 調整「輸出碼率」（設定 → 轉播備援）後重測，觀察 PSNR/SSIM 是否提升
```

## FAQ

**Q：PSNR 很高但畫面還是覺得糊？**
PSNR/SSIM 是全畫面平均，對大動態、快速移動的區塊不敏感。請一併看截圖，或提高碼率重測。

**Q：兩邊解析度不同？**
請先讓兩邊輸出一致（同來源、同解析度），否則 PSNR 沒有比較意義。

**Q：可以比較不同平台的輸出嗎？**
可以，但平台端可能再轉碼，數值會混入平台影響；建議比較**本機目標**。

**Q：需要 GPU 嗎？**
不需要，量測只在主機上用 ffmpeg 解碼比對。
