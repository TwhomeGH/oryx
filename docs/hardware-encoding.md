# 硬體編碼（GPU 轉碼）配置指南

> 目標：讓 AI 字幕疊加、攝影機推流等轉碼任務改用 GPU，大幅降低 CPU 佔用。
> 本倉庫的映像已內建全功能 ffmpeg（BtbN GPL 版，含 nvenc/qsv/vaapi/amf 編碼器），
> 剩下的工作只有一件事：**讓容器看得見你的 GPU，並讓函式庫版本與驅動對齊**。

---

## 1. 你的環境能不能用？

| 部署形態 | N 卡 (NVENC) | A 卡 (AMF/VAAPI) | Intel 核顯 (QSV/VAAPI) |
|---|---|---|---|
| 原生 Linux + Docker | ✅（runtime 自動掛載函式庫） | ✅（需透通 /dev/dri + 補裝函式庫） | ✅ |
| Windows Docker Desktop (WSL2) | ✅（`gpus: all` + `NVIDIA_DRIVER_CAPABILITIES=all`，見 2.1） | ❌ 僅特定獨立顯卡 + ROCm 用途，內顯無解（見 5.3） | ❌ WSL2 不暴露 /dev/dri |

**NVENC 函式庫（libnvidia-encode）不 bake 進映像**，原因：它的版本必須與主機驅動的 nvenc API 版本一致，bake 固定版本會在主機驅動更新後失配（例如 bake 570 而驅動升級到 610 時，報 `Required: 13.1 Found: 13.0`）。所以：

- **原生 Linux**：由 NVIDIA Container Toolkit 的 runtime 自動掛載主機驅動對應的函式庫，無需手動處理
- **WSL2**：Docker Desktop 的 nvidia runtime 也會自動掛載，但需在 compose 設 `NVIDIA_DRIVER_CAPABILITIES=all`（不設則 encode 函式庫不會掛），見第 2.1 節

## 2. 一次性配置：docker-compose.yml

在部署機的 `docker-compose.yml` 的 oryx 服務下加上對應段落：

### N 卡（原生 Linux）

```yaml
services:
  oryx:
    gpus: all
```

前提：宿主機已裝好 N 卡驅動，並安裝
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)。
runtime 會自動把與驅動同版本的 `libnvidia-*`（含 libnvidia-encode）掛進容器。

### N 卡（Windows Docker Desktop / WSL2）

WSL2 的 Docker Desktop 內建 GPU 支援，`gpus: all` + `NVIDIA_DRIVER_CAPABILITIES` 就能讓 runtime 自動掛載與主機驅動同版本的函式庫（含 libnvidia-encode）：

```yaml
services:
  oryx:
    gpus: all
    environment:
      - NVIDIA_DRIVER_CAPABILITIES=all
```

> `NVIDIA_DRIVER_CAPABILITIES` 控制 runtime 掛載哪些函式庫；不設的話 encode 函式庫不會掛，nvenc 會報 `Cannot load libnvidia-encode.so.1`。設 `all`（或 `video,compute,utility`）即可。
>
> ⚠️ 需先確認 WSL2 能看到 GPU（`wsl -e nvidia-smi -L` 有輸出）；若看不到，先 `wsl --update` + `wsl --shutdown` + 重啟 Docker Desktop。

### A 卡 / Intel 核顯（僅原生 Linux）

```yaml
services:
  oryx:
    devices:
      - /dev/dri:/dev/dri
```

### 兩張都有、全都要

```yaml
services:
  oryx:
    gpus: all
    devices:
      - /dev/dri:/dev/dri
```

改完執行 `docker compose up -d` 重建容器。

## 3. 驗證：組件頁卡片自動判讀

打開 `http://<主機>:882/routers/components`，看「FFmpeg 編碼能力」卡片：

```
版本:   ffmpeg 7.x (BtbN GPL)
裝置:   [NVIDIA:/dev/nvidia0] [DRM/VAAPI:/dev/dri/renderD128]
編碼器: libx264✓ libx265✓ h264_nvenc✓ h264_qsv✓ h264_vaapi✓ ...
```

| 你看到的組合 | 意思 | 行動 |
|---|---|---|
| 裝置徽章在＋編碼器綠燈 | 一切就緒 | 直接用 |
| 裝置徽章在＋編碼器紅燈 | 容器看到硬體但初始化失敗 | 看紅徽章下方原始報錯 |
| 沒有裝置徽章＋編碼器紅燈 | compose 沒透通成功 | 回到第 2 節檢查縮排與重建 |

CLI 快速驗證：

```bash
docker exec oryx ffmpeg -hide_banner -encoders 2>/dev/null | grep -E "nvenc|qsv|vaapi"
```

## 4. 啟用：字幕轉碼配方

系統 → AI 字幕 → 视频转码参数 → 下拉選單：

- **NVIDIA 硬體编码 (NVENC)** — 只有實測通過才會亮起
- **Intel 硬體编码 (QSV)** — 同上

灰掉的選項會標註「未检测到可用」，表示探測失敗（原因見第 3 節判讀）。
偵測結果快取 10 分鐘；改完 compose 配置後按卡片上的「重新檢測」。

## 5. 常見問題

| 症狀 | 原因與解法 |
|---|---|
| nvenc 紅燈，報 `Cannot load libnvidia-encode.so` | 映像缺少 encode 函式庫；重建映像即可（Dockerfile 已內建） |
| nvenc 紅燈，報 `Operation not permitted` | WSL2 驅動問題；確認 Windows 已裝最新 NVIDIA Game Ready 驅動，重啟 Docker Desktop |
| vaapi 紅燈，報 `/dev/dri/renderD128` 相關 | compose 沒掛 devices，或你在 WSL2（無解，改用 NVENC） |
| qsv 紅燈 | Intel 核顯需 11 代以上較穩；或缺 `/dev/dri` 透通 |
| 全部紅燈但以前是綠的 | 映像更新後重啟容器即可；或驅動被動過 |
| **全部紅燈，容器內 `ls /dev/nvidia*` 找不到，但主機 `nvidia-smi` 正常** | WSL2 裝置節點沒掛載（見下節 5.1） |

### 5.1 WSL2 容器看不到 GPU 的診斷與修復

**症狀：** 組件頁「FFmpeg 編碼能力」全部硬體編碼器紅燈，探測輸出類似：

```
h264_amf:   Invalid argument
h264_nvenc: Invalid argument
h264_qsv:   device creation failed: -542398533
h264_vaapi: found for device /dev/dri/renderD128. Device creation failed: -22
```

**診斷順序**（在 PowerShell 依序跑）：

```powershell
# 1. 主機有沒有 GPU？有 → 往下
nvidia-smi

# 2. WSL2 內能不能看到 GPU？（CUDA 層）
wsl -e nvidia-smi -L

# 3. WSL2 內裝置節點在不在？（關鍵！）
wsl -e ls /dev/dri /dev/nvidia*

# 4. 容器內裝置節點在不在？
docker exec oryx ls /dev/dri /dev/nvidia*
```

**判讀：**

| 2 的結果 | 3 的結果 | 4 的結果 | 結論 |
|---|---|---|---|
| 看得到 | 看得到 | 看得到 | ✅ 一切正常，問題在別處 |
| 看得到 | **看不到** | 看不到 | WSL2 沒把 `/dev/nvidia*` 掛進系統（最常見） |
| 看得到 | 看得到 | **看不到** | Docker Desktop 沒透通，重啟 Docker Desktop |

**WSL2 裝置節點消失的修復（步驟 3 看不到時）：**

```powershell
# 完整關閉 WSL2（所有發行版），強制重新掛載 GPU 裝置
wsl --shutdown

# 更新 WSL2 內核（WSL 2.0+ 會自動建立 /dev/dxg 並映射 nvidia 節點）
wsl --update

# 重開 WSL2 後再檢查裝置節點
wsl -e ls /dev/dri /dev/nvidia*

# 沒問題後重啟 Docker Desktop，重建容器
docker compose up -d
```

若 `wsl --update` 後 `ls /dev/nvidia*` 仍空，多半是驅動不是最新 Game Ready 版，或系統是虛擬機（VM 內再開 WSL2 時 GPU 透通常失效，Nested Virtualization）。這種情況建議直接放棄容器內硬體編碼，改用主機原生跑 ffmpeg，或改用 CPU 軟編碼。

### 5.2 nvenc 報 `Driver does not support the required nvenc API version`

**症狀：** 容器內 `nvidia-smi` 看得到 GPU，但 h264_nvenc/hevc_nvenc 探測失敗，報：

```
[h264_nvenc] Driver does not support the required nvenc API version. Required: 13.1 Found: 13.0
[h264_nvenc] The minimum required Nvidia driver for nvenc is 610.00 or newer
```

**原因：** 容器內使用的 `libnvidia-encode` 版本與主機驅動的 nvenc API 版本失配。本映像內建最新 BtbN ffmpeg（2026-08 之後），需要 nvenc API 13.1（驅動 ≥610）；若容器內函式庫是舊版（如 570 → API 13.0），就會報這個錯。**舊版映像曾 bake `libnvidia-encode-570`，已移除** — 現在 NVENC 依賴 runtime 掛載（原生 Linux）或手動掛載（WSL2，見 2.1）。

**診斷確認：**

```powershell
# 容器內函式庫版本（應與驅動一致）
docker exec oryx ls -la /usr/lib/x86_64-linux-gnu/libnvidia-encode.so.1

# 容器內 ffmpeg 需要的 API 版本
docker exec oryx ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc=duration=0.05 -frames:v 3 -c:v h264_nvenc -f null -
```

**解法（擇一）：**

| 方案 | 做法 | 適用 |
|---|---|---|
| **A. 移除 bake 函式庫（已內建）** | 新映像已移除 bake 570，runtime 自動掛載與驅動同版本 | 所有環境 |
| **B. WSL2 設 NVIDIA_DRIVER_CAPABILITIES** | compose 加 `environment: NVIDIA_DRIVER_CAPABILITIES=all` | Windows + Docker Desktop |
| **C. 原生 Linux 裝 Container Toolkit** | 裝 nvidia-container-toolkit，`gpus: all` 即可 | 原生 Linux 主機 |

**判斷：** 若用舊映像（還 bake 570）會報此錯，換新映像即可；WSL2 記得加 `NVIDIA_DRIVER_CAPABILITIES`。

### 5.3 AMF（AMD）在 WSL2 下不可用

**症狀：** 組件頁 `h264_amf` 紅燈，探測報：

```
[AMF] DLL libamfrt64.so.1 failed to open
[h264_amf] Failed to create hardware device context (AMF): Unknown error occurred
```

**原因：** 與 NVENC（runtime 自動掛載函式庫）不同，AMF 有兩個硬性條件，WSL2 環境都不滿足：

| 條件 | WSL2 狀態 |
|---|---|
| **函式庫 `libamfrt64.so.1`** | 映像未安裝（Dockerfile 只處理了 VAAPI/QSV/NVENC 的函式庫，從未裝過 AMF runtime） |
| **AMD GPU 透通** | WSL2 的 GPU 透通機制（/dev/dxg + nvidia runtime）**只支援 NVIDIA**；AMD 的透通僅限特定獨立顯卡 |

**AMD 官方有提供 WSL2 驅動，但有嚴格限制：**

[AMD Software for WSL 2](https://www.amd.com/en/resources/support-articles/release-notes/RN-RAD-WIN-24-10-21-01-WSL-2.html) 僅支援：
- **用途**：ROCm（PyTorch AI 開發），**不是 AMF 硬體編碼**
- **GPU**：RX 7900 XTX/XT/GRE、PRO W7900/W7800（皆為**獨立顯卡**）
- 筆電內顯（如 Radeon Vega 8，DEV_1638）**不在支援清單**

**結論：**
- 你的 AMD **內顯**（Vega 8）在 WSL2 下 AMF 不可用，不是單純補裝函式庫能解
- 即使有支援清單內的 AMD 獨立顯卡，也需 AMD 的 container runtime 透通（本 fork 未實測）
- **這不是 env 變數能解的** — 設任何 `NVIDIA_DRIVER_CAPABILITIES` 都沒用，根本沒有 AMD 裝置可透通

**在原生 Linux + Docker 下：** AMD 透通（/dev/dri + /dev/kfd）較成熟，理論上補裝 `libamfrt64` + 透通 AMD 裝置後 AMF 可能可用，但本 fork 未實測。若你是 AMD GPU 用戶，建議用原生 Linux 主機。

## 6. 收益參考

同一台機器開 5 路 1080p AI 字幕疊加：

| 模式 | CPU 佔用 |
|---|---|
| libx264 medium（預設） | ~400%（吃滿 4 核） |
| NVENC / QSV / VAAPI | ~10-20% |

CPU 空出來之後，受益的是同機的其他場景（虛擬直播、轉錄 ASR 等）。
