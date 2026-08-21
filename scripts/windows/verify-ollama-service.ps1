# verify-ollama-service.ps1
# Run on YUKI (192.168.68.56) to confirm OllamaService is correctly configured.
# Usage: powershell -ExecutionPolicy Bypass -File verify-ollama-service.ps1

$ErrorActionPreference = "Stop"

Write-Host "=== OllamaService status ===" -ForegroundColor Cyan
$svc = Get-Service -Name "OllamaService" -ErrorAction SilentlyContinue
if ($null -eq $svc) {
    Write-Host "FAIL: OllamaService not found" -ForegroundColor Red
    exit 1
}
Write-Host "Status   : $($svc.Status)"
Write-Host "StartType: $($svc.StartType)"

if ($svc.Status -ne "Running") {
    Write-Host "WARN: service is not running — starting..." -ForegroundColor Yellow
    Start-Service OllamaService
    Start-Sleep 3
}
if ($svc.StartType -ne "Automatic") {
    Write-Host "WARN: StartType is $($svc.StartType) — setting to Automatic" -ForegroundColor Yellow
    Set-Service OllamaService -StartupType Automatic
}

Write-Host ""
Write-Host "=== Ollama API health ===" -ForegroundColor Cyan
try {
    $resp = Invoke-RestMethod -Uri "http://localhost:11434/api/tags" -TimeoutSec 5
    $models = $resp.models
    Write-Host "Models ($($models.Count)):"
    foreach ($m in $models) {
        $sizeMB = [math]::Round($m.size / 1MB)
        Write-Host "  $($m.name)  $sizeMB MB"
    }
    Write-Host "OK: Ollama API reachable" -ForegroundColor Green
} catch {
    Write-Host "FAIL: Ollama API unreachable — $_" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "=== Firewall (port 11434) ===" -ForegroundColor Cyan
$rule = Get-NetFirewallRule -DisplayName "*Ollama*" -ErrorAction SilentlyContinue
if ($rule) {
    Write-Host "Firewall rule: $($rule.DisplayName) [$($rule.Action)]"
} else {
    Write-Host "WARN: no Ollama firewall rule found — add one if remote access is needed:" -ForegroundColor Yellow
    Write-Host "  New-NetFirewallRule -DisplayName 'Ollama API' -Direction Inbound -Protocol TCP -LocalPort 11434 -Action Allow"
}

Write-Host ""
Write-Host "All checks passed." -ForegroundColor Green
