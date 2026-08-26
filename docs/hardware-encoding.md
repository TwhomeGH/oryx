# 硬體編碼（GPU 轉碼）配置指南

> 目標：讓 AI 字幕疊加、攝影機推流等轉碼任務改用 GPU，大幅降低 CPU 佔用。
> 本倉庫的映像已內建全功能 ffmpeg（BtbN GPL 版，含 nvenc/qsv/vaapi/amf 編碼器），
> 剩下的工作只有一件事：**讓容器看得見你的 GPU**。

---

## 1. 你的環境能不能用？

| 部署形態 | N 卡 (NVENC) | A 卡 (VAAPI) | Intel 核顯 (QSV/VAAPI) |
|---|---|---|---|
| 原生 Linux + Docker | ✅ | ✅ | ✅ |
| Windows Docker Desktop (WSL2) | ✅ | ❌ WSL2 不暴露 /dev/dri | ❌ |

\* WSL2 NVENC 需要：(1) Windows 安裝 NVIDIA Game Ready 驅動 (2) Docker Desktop 內建 WSL2 GPU 支援 (3) 映像內建 `libnvidia-encode`（已在 Dockerfile 中安裝）。

## 2. 一次性配置：docker-compose.yml

在部署機的 `docker-compose.yml` 的 oryx 服務下加上對應段落：

### N 卡

```yaml
services:
  oryx:
    gpus: all
```

前提：宿主機已裝好 N 卡驅動；Linux 另需
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
（Docker Desktop 已內建，不用額外裝）。

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

## 6. 收益參考

同一台機器開 5 路 1080p AI 字幕疊加：

| 模式 | CPU 佔用 |
|---|---|
| libx264 medium（預設） | ~400%（吃滿 4 核） |
| NVENC / QSV / VAAPI | ~10-20% |

CPU 空出來之後，受益的是同機的其他場景（虛擬直播、轉錄 ASR 等）。
