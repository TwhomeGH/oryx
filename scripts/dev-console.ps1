# SRS Console 本地預覽工具
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\dev-console.ps1              # 平台在 127.0.0.1:2022
#   powershell -ExecutionPolicy Bypass -File scripts\dev-console.ps1 -Platform http://127.0.0.1:882  # Docker 映射端口
#   powershell -ExecutionPolicy Bypass -File scripts\dev-console.ps1 -Port 4000   # 自訂 dev server 端口
#
# 這會啟動 Vite dev server（含 /api /terraform 等 proxy），並自動開啟瀏覽器到
# SRS 控制台頁面。需要先有運作中的平台（原生 2022 或 Docker 映射端口）。
#
param(
    [string]$Platform = "",        # 平台地址，預設自動偵測
    [int]$Port = 3000,             # Vite dev server 端口
    [switch]$NoBrowser             # 不自動開瀏覽器
)

$ErrorActionPreference = "Stop"
$uiDir = Join-Path $PSScriptRoot "..\ui"

# 如果沒指定平台，自動偵測：先試 2022（原生），再試 882（Docker 映射）。
if (-not $Platform) {
    foreach ($cand in @("http://127.0.0.1:2022", "http://127.0.0.1:882")) {
        try {
            $r = Invoke-WebRequest -Uri "$cand/api/v1/versions" -TimeoutSec 2 -UseBasicParsing
            if ($r.StatusCode -eq 200) {
                $Platform = $cand
                Write-Host "[dev-console] 偵測到平台: $Platform" -ForegroundColor Green
                break
            }
        } catch {}
    }
    if (-not $Platform) {
        Write-Host "[dev-console] 找不到平台，請用 -Platform 指定（原生 2022 或 Docker 映射端口，例如 882）" -ForegroundColor Red
        Write-Host "  範例: powershell -ExecutionPolicy Bypass -File scripts\dev-console.ps1 -Platform http://127.0.0.1:882" -ForegroundColor Yellow
        exit 1
    }
}

# 檢查 node_modules 是否完整（esbuild/rollup native module）。
if (-not (Test-Path (Join-Path $uiDir "node_modules\vite"))) {
    Write-Host "[dev-console] 安裝 UI 依賴..." -ForegroundColor Cyan
    Push-Location $uiDir
    npm install
    Pop-Location
}

# 啟動 Vite dev server。
$env:PUBLIC_URL = "/mgmt"
$env:REACT_APP_LOCALE = "zh"
$env:SRS_PLATFORM = $Platform

Write-Host "[dev-console] 啟動 Vite dev server (port $Port, proxy → $Platform)" -ForegroundColor Cyan
Write-Host "[dev-console] 控制台網址: http://localhost:$Port/mgmt/zh/routers-console" -ForegroundColor Green

if (-not $NoBrowser) {
    Start-Process "http://localhost:$Port/mgmt/zh/routers-console"
}

Push-Location $uiDir
npm start
Pop-Location
