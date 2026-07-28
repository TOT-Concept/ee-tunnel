#Requires -Version 5.0
<#
ee-tunnel installer for Windows.

Source:   https://github.com/TOT-Concept/ee-tunnel
License:  MIT
Releases: https://github.com/TOT-Concept/ee-tunnel/releases

Usage:
    iwr -useb https://entityenricher.ai/install.ps1 | iex

Pin a version:
    $env:EE_TUNNEL_VERSION = "v0.1.0"; iwr -useb https://entityenricher.ai/install.ps1 | iex

Skip the prompt (CI):
    $env:EE_TUNNEL_YES = "1"; iwr -useb https://entityenricher.ai/install.ps1 | iex
#>

$ErrorActionPreference = "Stop"

$repo    = "TOT-Concept/ee-tunnel"
$version = if ($env:EE_TUNNEL_VERSION) { $env:EE_TUNNEL_VERSION } else { "latest" }

# Determine architecture (Windows ARM64 not yet released; fall back to x86_64).
$arch = if ([Environment]::Is64BitOperatingSystem) { "x86_64" } else {
    Write-Error "ee-tunnel requires a 64-bit Windows OS."
}

$asset = "ee-tunnel-windows-$arch.exe"
$baseUrl = if ($version -eq "latest") {
    "https://github.com/$repo/releases/latest/download"
} else {
    "https://github.com/$repo/releases/download/$version"
}
$url    = "$baseUrl/$asset"
$sigUrl = "$baseUrl/$asset.sig"
$cosignPubkeyUrl = if ($env:EE_TUNNEL_COSIGN_PUBKEY_URL) {
    $env:EE_TUNNEL_COSIGN_PUBKEY_URL
} else {
    "https://entityenricher.ai/ee-tunnel-cosign.pub"
}

# Install location: prefer %LOCALAPPDATA%\Programs\ee-tunnel; fall back to USERPROFILE.
$destDir = Join-Path $env:LOCALAPPDATA "Programs\ee-tunnel"
if (-not (Test-Path $destDir)) {
    New-Item -ItemType Directory -Force -Path $destDir | Out-Null
}
$dest = Join-Path $destDir "ee-tunnel.exe"

Write-Host ""
Write-Host "ee-tunnel installer"
Write-Host ""
Write-Host "  Source     https://github.com/$repo  (MIT)"
Write-Host "  Download   $url"
Write-Host "  Install to $dest"
Write-Host "  Version    $version"
Write-Host ""
Write-Host "You can audit this script first:    iwr -useb https://entityenricher.ai/install.ps1"
Write-Host "You can pin a version:              `$env:EE_TUNNEL_VERSION = 'v0.1.0'"
Write-Host "You can skip the prompt in CI:      `$env:EE_TUNNEL_YES = '1'"
Write-Host "You can skip PATH setup:            `$env:EE_TUNNEL_NO_PATH = '1'"
Write-Host ""

if (-not $env:EE_TUNNEL_YES -and [Environment]::UserInteractive) {
    for ($i = 5; $i -ge 1; $i--) {
        Write-Host -NoNewline "Continuing in $i seconds (Ctrl+C to abort)... `r"
        Start-Sleep -Seconds 1
    }
    Write-Host ""
    Write-Host ""
}

Write-Host "Downloading..."
$tmpBin = New-TemporaryFile
$tmpSig = New-TemporaryFile
$tmpPub = New-TemporaryFile
try {
    Invoke-WebRequest -Uri $url -OutFile $tmpBin -UseBasicParsing
} catch {
    Write-Error "Download failed: $_"
}

# Signature verification (skip with EE_TUNNEL_SKIP_VERIFY=1).
if (-not $env:EE_TUNNEL_SKIP_VERIFY) {
    if (-not (Get-Command cosign -ErrorAction SilentlyContinue)) {
        Write-Error @"
cosign is required to verify the binary signature.
  Install: https://docs.sigstore.dev/cosign/installation/
  Bypass:  `$env:EE_TUNNEL_SKIP_VERIFY = '1'  (not recommended)
"@
    }
    try {
        Invoke-WebRequest -Uri $sigUrl -OutFile $tmpSig -UseBasicParsing
        Invoke-WebRequest -Uri $cosignPubkeyUrl -OutFile $tmpPub -UseBasicParsing
    } catch {
        Write-Error "Could not download signature or pubkey: $_"
    }
    & cosign verify-blob --key $tmpPub --signature $tmpSig $tmpBin | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Signature verification FAILED for $asset"
    }
    Write-Host "OK Signature verified." -ForegroundColor Green
} else {
    Write-Host "WARN EE_TUNNEL_SKIP_VERIFY=1 set - proceeding without signature check." -ForegroundColor Yellow
}

Move-Item -Force $tmpBin $dest
Remove-Item -Force $tmpSig, $tmpPub -ErrorAction SilentlyContinue

Write-Host "OK Installed ee-tunnel to $dest" -ForegroundColor Green
Write-Host ""

# === PATH setup ============================================================
# Add the install dir to the user PATH so `ee-tunnel` resolves in new shells.
# Exact-match dedup against existing user + machine PATH entries (so we never
# add a duplicate). Also updates the current session's PATH so the binary is
# usable immediately without restarting the terminal. Opt out with
# $env:EE_TUNNEL_NO_PATH = '1'.
$needsShellReload = $false
if (-not $env:EE_TUNNEL_NO_PATH) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine")
    $splitOpts = [StringSplitOptions]::RemoveEmptyEntries
    $userEntries = if ($userPath) { $userPath.Split(';', $splitOpts) } else { @() }
    $machineEntries = if ($machinePath) { $machinePath.Split(';', $splitOpts) } else { @() }
    $allEntries = @($userEntries + $machineEntries) | ForEach-Object { $_.TrimEnd('\') }
    $destDirNormalized = $destDir.TrimEnd('\')

    if ($allEntries -contains $destDirNormalized) {
        Write-Host "i  $destDir already on PATH (skipping)."
    } else {
        $newUserPath = if ($userPath) { "$userPath;$destDir" } else { $destDir }
        [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
        # Update current session so `ee-tunnel` works without a terminal restart.
        $env:Path = "$env:Path;$destDir"
        Write-Host "OK Added $destDir to user PATH" -ForegroundColor Green
        $needsShellReload = $true
    }
}

Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. Open https://entityenricher.ai → API Keys → Ollama Tunnels"
Write-Host "  2. Create a tunnel and copy the pairing command, OR run:"
Write-Host "       ee-tunnel pair --server https://entityenricher.ai"
Write-Host "  3. Then:"
Write-Host "       ee-tunnel"
if ($needsShellReload) {
    Write-Host ""
    Write-Host "WARN Open a new terminal so the updated PATH applies to other apps." -ForegroundColor Yellow
}
