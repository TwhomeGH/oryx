<#
.SYNOPSIS
  檢查 tools 下的 PowerShell 腳本都有 comment-based help，且每個參數都有 .PARAMETER。

.DESCRIPTION
  用來避免「腳本改了、文件沒改」的不一致。只檢查最上層 param() 區塊（不含函式參數），
  CI 可直接執行；發現缺漏時以非零碼結束。
#>
$ErrorActionPreference = 'Stop'
$failed = @()
Get-ChildItem -Path $PSScriptRoot -Filter '*.ps1' | ForEach-Object {
    $path = $_.FullName
    if ($path -like '*check-ps1-docs.ps1') { return }
    $content = Get-Content -Raw $path
    if ($content -notmatch '<#\r?\n\.SYNOPSIS') {
        $failed += "$($_.Name): 缺少 comment-based help (.SYNOPSIS)"
        return
    }
    # Only the top-level param block: the first param( before any function definition.
    $paramStart = $content.IndexOf('param(')
    $funcStart = $content.IndexOf('function ')
    if ($paramStart -lt 0 -or ($funcStart -ge 0 -and $funcStart -lt $paramStart)) { return }
    $paramText = $content.Substring($paramStart, [Math]::Min(3000, $content.Length - $paramStart))
    $params = [regex]::Matches($paramText, '^\s*\[(?:string|int|switch|bool)\]\$(\w+)', 'Multiline') |
        ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique
    foreach ($p in $params) {
        if ($content -notmatch "\.PARAMETER\s+$p\b") {
            $failed += "$($_.Name): 參數 -$p 缺少 .PARAMETER 說明"
        }
    }
}
if ($failed.Count -gt 0) {
    $failed | ForEach-Object { Write-Host "FAIL: $_" -ForegroundColor Red }
    exit 1
}
Write-Host 'OK: 所有腳本都有完整的 comment-based help' -ForegroundColor Green