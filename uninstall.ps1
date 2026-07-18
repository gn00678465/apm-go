# apm-go uninstaller (Windows / PowerShell)
#
# Removes %LOCALAPPDATA%\apm-go and drops that directory from the
# user PATH. Idempotent: safe to run when apm-go is not installed.
#
# Usage:
#   irm https://raw.githubusercontent.com/gn00678465/apm-go/main/uninstall.ps1 | iex

$ErrorActionPreference = "Stop"

$installDir = Join-Path $env:LOCALAPPDATA "apm-go"

function Write-Info { param([string]$Message) Write-Host $Message -ForegroundColor Cyan }
function Write-Success { param([string]$Message) Write-Host $Message -ForegroundColor Green }

$removedSomething = $false

if (Test-Path $installDir) {
    Remove-Item -Recurse -Force $installDir
    Write-Info "Removed $installDir"
    $removedSomething = $true
}

$currentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($currentUserPath) {
    $userEntries = $currentUserPath.Split(";", [System.StringSplitOptions]::RemoveEmptyEntries)
    if ($userEntries -contains $installDir) {
        $newUserPath = ($userEntries | Where-Object { $_ -ne $installDir }) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
        Write-Info "Removed $installDir from your user PATH."
        $removedSomething = $true
    }
}

$sessionEntries = $env:Path -split ";"
if ($sessionEntries -contains $installDir) {
    $env:Path = ($sessionEntries | Where-Object { $_ -ne $installDir }) -join ";"
}

if ($removedSomething) {
    Write-Success "apm-go uninstalled."
} else {
    Write-Info "apm-go is not installed; nothing to do."
}
