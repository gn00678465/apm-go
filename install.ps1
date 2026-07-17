# apm-go installer (Windows / PowerShell) - release-download mode
#
# Downloads the apm-go binary for this platform from GitHub Releases,
# verifies its SHA256 checksum, installs it to %LOCALAPPDATA%\apm-go,
# and adds that directory to the user PATH.
#
# Usage:
#   irm https://raw.githubusercontent.com/gn00678465/apm-go/main/install.ps1 | iex
#
# Install a specific version:
#   $env:APM_GO_VERSION = "0.2.1"; irm https://raw.githubusercontent.com/gn00678465/apm-go/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "gn00678465/apm-go"
$installDir = Join-Path $env:LOCALAPPDATA "apm-go"
$binaryName = "apm-go.exe"

function Write-Info { param([string]$Message) Write-Host $Message -ForegroundColor Cyan }
function Write-Success { param([string]$Message) Write-Host $Message -ForegroundColor Green }
function Write-ErrorText { param([string]$Message) Write-Host $Message -ForegroundColor Red }

# ---------------------------------------------------------------------------
# Stage 1 - Platform detection + URL
# ---------------------------------------------------------------------------

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$asset = "apm-go-windows-$arch.exe"

if ($env:APM_GO_VERSION) {
    $baseUrl = "https://github.com/$repo/releases/download/v$($env:APM_GO_VERSION)"
} else {
    $baseUrl = "https://github.com/$repo/releases/latest/download"
}

# ---------------------------------------------------------------------------
# Stage 2 - Download binary + checksums to a temp directory
# ---------------------------------------------------------------------------

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("apm-go-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

try {
    $downloadedBinary = Join-Path $tempDir $binaryName
    $checksumFile = Join-Path $tempDir "SHA256SUMS"

    Write-Info "Downloading $asset from $baseUrl ..."
    try {
        Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $downloadedBinary -UseBasicParsing
        Invoke-WebRequest -Uri "$baseUrl/SHA256SUMS" -OutFile $checksumFile -UseBasicParsing
    } catch {
        Write-ErrorText "Download failed: $($_.Exception.Message)"
        Write-ErrorText "URL: $baseUrl/$asset"
        exit 1
    }

    # ------------------------------------------------------------------
    # Stage 3 - Verify checksum
    # ------------------------------------------------------------------

    Write-Info "Verifying checksum..."
    $expectedLine = Get-Content $checksumFile | Where-Object { $_ -match [regex]::Escape($asset) + '$' }
    if (-not $expectedLine) {
        Write-ErrorText "No checksum entry for $asset in SHA256SUMS."
        exit 1
    }
    $expectedHash = ($expectedLine -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -Algorithm SHA256 -Path $downloadedBinary).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        Write-ErrorText "SHA256 checksum mismatch for $asset."
        Write-ErrorText "  expected: $expectedHash"
        Write-ErrorText "  actual:   $actualHash"
        exit 1
    }
    Write-Success "Checksum OK."

    # ------------------------------------------------------------------
    # Stage 4 - Test the binary before installing
    # ------------------------------------------------------------------

    Write-Info "Testing binary..."
    $testOutput = & $downloadedBinary --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-ErrorText "Downloaded binary failed to run (exit code $LASTEXITCODE): $testOutput"
        exit 1
    }
    Write-Success "Binary test successful: $testOutput"

    # ------------------------------------------------------------------
    # Stage 5 - Install
    # ------------------------------------------------------------------

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item -Path $downloadedBinary -Destination (Join-Path $installDir $binaryName) -Force

    # ------------------------------------------------------------------
    # Stage 6 - Add to user PATH
    # ------------------------------------------------------------------

    $currentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $userEntries = @()
    if ($currentUserPath) {
        $userEntries = $currentUserPath.Split(";", [System.StringSplitOptions]::RemoveEmptyEntries)
    }
    if ($userEntries -notcontains $installDir) {
        $newUserPath = if ($currentUserPath) { "$installDir;$currentUserPath" } else { $installDir }
        [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
        Write-Info "Added $installDir to your user PATH."
    }
    if (($env:Path -split ";") -notcontains $installDir) {
        $env:Path = "$installDir;$env:Path"
    }

    Write-Host ""
    Write-Success "apm-go installed successfully!"
    Write-Info "Location: $(Join-Path $installDir $binaryName)"
    Write-Info "Run 'apm-go --version' in a new terminal to verify the installation."
} finally {
    if (Test-Path $tempDir) {
        Remove-Item -Recurse -Force $tempDir
    }
}
