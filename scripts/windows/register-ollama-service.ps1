# register-ollama-service.ps1
# Registers Ollama as a Windows service on YUKI if not already present.
# Run as Administrator:
#   powershell -ExecutionPolicy Bypass -File register-ollama-service.ps1
#
# NOTE: Ollama 0.1.x+ installer on Windows already registers OllamaService.
# Run verify-ollama-service.ps1 first — this script is only needed on fresh
# installs where the service was not created by the installer.

$ErrorActionPreference = "Stop"

$serviceName = "OllamaService"
$ollamaExe   = "$env:LOCALAPPDATA\Programs\Ollama\ollama.exe"

if (-not (Test-Path $ollamaExe)) {
    Write-Host "ERROR: ollama.exe not found at $ollamaExe" -ForegroundColor Red
    Write-Host "Download from https://ollama.com/download/windows and install first."
    exit 1
}

$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "OllamaService already exists (Status: $($existing.Status), StartType: $($existing.StartType))" -ForegroundColor Yellow
    Write-Host "Run verify-ollama-service.ps1 to check configuration."
    exit 0
}

Write-Host "Registering $serviceName ..." -ForegroundColor Cyan

# Use sc.exe to create the service pointing to ollama.exe serve
# OLLAMA_HOST env var controls the listen address (default: 127.0.0.1:11434)
# Set OLLAMA_HOST=0.0.0.0:11434 for LAN access
$env:OLLAMA_HOST = "0.0.0.0:11434"

New-Service `
    -Name $serviceName `
    -DisplayName $serviceName `
    -BinaryPathName "`"$ollamaExe`" serve" `
    -StartupType Automatic `
    -Description "Ollama local LLM inference server"

Start-Service $serviceName
$svc = Get-Service $serviceName
Write-Host "Service status: $($svc.Status)" -ForegroundColor Green

# Open firewall for LAN access
$fwRule = Get-NetFirewallRule -DisplayName "Ollama API" -ErrorAction SilentlyContinue
if (-not $fwRule) {
    New-NetFirewallRule `
        -DisplayName "Ollama API" `
        -Direction Inbound `
        -Protocol TCP `
        -LocalPort 11434 `
        -Action Allow | Out-Null
    Write-Host "Firewall rule created for port 11434" -ForegroundColor Green
}

Write-Host "Done. Run verify-ollama-service.ps1 to confirm." -ForegroundColor Green
