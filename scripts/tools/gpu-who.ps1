#!/usr/bin/env pwsh
#
# GPU 顯存占用快速查詢 (gpu-who)
#
# 顯示：GPU 總使用量、容器內占用顯存的程序、Windows 主機上使用 GPU 的程序。
# Windows(WDDM) 下 nvidia-smi 的每程序顯存常顯示 N/A，因此主機以程序清單呈現。
#
# 用法:
#   pwsh scripts/tools/gpu-who.ps1
#   pwsh scripts/tools/gpu-who.ps1 -Container oryx
#
[CmdletBinding()]
param(
    [string]$Container = "oryx"
)

$ErrorActionPreference = "Continue"
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

function Section($msg) { Write-Host ""; Write-Host "==> $msg" -ForegroundColor Cyan }

if (-not (Get-Command nvidia-smi -ErrorAction SilentlyContinue)) {
    Write-Host "找不到 nvidia-smi（未安裝 NVIDIA 驅動或無 GPU）。" -ForegroundColor Red
    exit 1
}

Section "GPU 總覽"
nvidia-smi --query-gpu=name,memory.used,memory.total,utilization.gpu --format=csv,noheader

$running = (docker ps --filter "name=^/$Container$" --format "{{.Names}}" 2>$null)
if ($running) {
    Section "容器 $Container 內占用顯存的程序"
    $apps = docker exec $Container sh -c "nvidia-smi --query-compute-apps=pid,process_name,used_memory --format=csv,noheader 2>/dev/null"
    if ($apps) {
        $apps | ForEach-Object { Write-Host "  $_" -ForegroundColor Green }
    } else {
        Write-Host "  （無；容器內的 WSL 只看得到總量，程序多半是 Windows 端）" -ForegroundColor DarkGray
    }
} else {
    Write-Host "容器 $Container 未執行，略過容器查詢。" -ForegroundColor DarkYellow
}

Section "Windows 主機上使用 GPU 的程序（依程式名稱統計）"
nvidia-smi --query-compute-apps=pid,process_name --format=csv,noheader |
    ForEach-Object {
        $parts = $_ -split ",\s*"
        if ($parts.Length -ge 2) {
            $path = $parts[1]
            try { [IO.Path]::GetFileName($path) } catch { $path }
        }
    } |
    Group-Object |
    Sort-Object Count -Descending |
    Select-Object -First 20 |
    ForEach-Object { Write-Host ("  {0,-40} x{1}" -f $_.Name, $_.Count) }

Write-Host ""
Write-Host "提示：WDDM 下每程序顯存為 N/A；若要釋放，關閉 Wallpaper Engine、Edge、LM Studio、MuMuPlayer、Parsec 等常駐程式。" -ForegroundColor Yellow