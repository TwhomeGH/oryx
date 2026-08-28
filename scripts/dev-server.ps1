# Oryx UI Dev Server 啟動工具
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1               # 平台自動偵測，開場景頁
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Platform http://127.0.0.1:882  # Docker 映射端口
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Port 4000    # 自訂 dev server 端口
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page console # 開啟控制台頁（等同舊 dev-console.ps1）
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page settings # 開啟系統配置頁
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Page "scenario?tab=vlive" # 開啟虛擬直播頁
#   powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -NoBrowser    # 不自動開瀏覽器
#
# 這會啟動 Vite dev server（含 /api /terraform /players /tools 等 proxy），並自動開啟
# 瀏覽器到指定頁面。需要先有運作中的平台（原生 2022 或 Docker 映射端口）。
#
# 可用 -Page 值：scenario（預設，應用場景）、settings（系統配置）、console（控制台）、
#   components（組件管理）、contact（關於）、players（播放器目錄）。
#
param(
    [string]$Platform = "",        # 平台地址，預設自動偵測
    [int]$Port = 3000,             # Vite dev server 端口
    [switch]$NoBrowser,            # 不自動開瀏覽器
    [string]$Page = "scenario"     # 要開啟的頁面
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
                Write-Host "[dev-server] 偵測到平台: $Platform" -ForegroundColor Green
                break
            }
        } catch {}
    }
    if (-not $Platform) {
        Write-Host "[dev-server] 找不到平台，請用 -Platform 指定（原生 2022 或 Docker 映射端口，例如 882）" -ForegroundColor Red
        Write-Host "  範例: powershell -ExecutionPolicy Bypass -File scripts\dev-server.ps1 -Platform http://127.0.0.1:882" -ForegroundColor Yellow
        exit 1
    }
}

# 檢查 node_modules 是否完整（esbuild/rollup native module）。
if (-not (Test-Path (Join-Path $uiDir "node_modules\vite"))) {
    Write-Host "[dev-server] 安裝 UI 依賴..." -ForegroundColor Cyan
    Push-Location $uiDir
    npm install
    Pop-Location
}

# 頁面路由對照表。新增頁面時記得在這裡加一行。
$pageMap = @{
    "scenario"   = "/mgmt/zh/routers-scenario"
    "settings"   = "/mgmt/zh/routers-settings"
    "console"    = "/mgmt/zh/routers-console"
    "components" = "/mgmt/zh/routers-components"
    "contact"    = "/mgmt/zh/routers-contact"
    "players"    = "/players/"
}
$path = $pageMap[$Page]
if (-not $path) {
    Write-Host "[dev-server] 未知頁面 '$Page'，可用: $($pageMap.Keys -join ', ')" -ForegroundColor Red
    exit 1
}

# 啟動 Vite dev server。
$env:PUBLIC_URL = "/mgmt"
$env:REACT_APP_LOCALE = "zh"
$env:SRS_PLATFORM = $Platform

Write-Host "[dev-server] 啟動 Vite dev server (port $Port, proxy → $Platform)" -ForegroundColor Cyan
Write-Host "[dev-server] 網址: http://localhost:$Port$path" -ForegroundColor Green

if (-not $NoBrowser) {
    Start-Process "http://localhost:$Port$path"
}

Push-Location $uiDir
npm start
Pop-Location
