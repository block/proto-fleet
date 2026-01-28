#Requires -RunAsAdministrator

<#
.SYNOPSIS
    Sets up WSL2 and Docker Engine for Proto Fleet installation on Windows.

.DESCRIPTION
    This script automates the setup of WSL2 and Docker Engine inside WSL for running
    Proto Fleet on Windows systems. It handles:
    - WSL2 installation and configuration
    - Ubuntu distribution installation
    - Docker Engine installation inside WSL
    - WSL networking fixes for Docker registry connectivity
    - Docker daemon configuration and startup

.PARAMETER Force
    Skip confirmation prompts where possible

.EXAMPLE
    .\setup-wsl-docker.ps1
    Run the setup with interactive prompts

.EXAMPLE
    .\setup-wsl-docker.ps1 -Force
    Run the setup with minimal prompts

.NOTES
    - Must be run as Administrator
    - Requires Windows 10 build 19041+ or Windows 11
    - Will reboot if WSL features need to be enabled
#>

[CmdletBinding()]
param(
    [switch]$Force
)

$ErrorActionPreference = "Stop"

# Constants
$MIN_RAM_GB = 8
$MIN_DISK_GB = 20
$MIN_WIN10_BUILD = 19041

# ============================================================================
# Helper Functions
# ============================================================================

function Write-Step {
    param([string]$Message)
    Write-Host "`n$Message" -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "✓ $Message" -ForegroundColor Green
}

function Write-WarningMsg {
    param([string]$Message)
    Write-Host "⚠ $Message" -ForegroundColor Yellow
}

function Write-ErrorMsg {
    param([string]$Message)
    Write-Host "✗ $Message" -ForegroundColor Red
}

function Test-Administrator {
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    return $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Test-SystemRequirements {
    Write-Step "Checking system requirements..."

    $warnings = @()

    # Check Windows version
    $osInfo = Get-CimInstance Win32_OperatingSystem
    $buildNumber = [int]$osInfo.BuildNumber

    if ($osInfo.Caption -match "Windows 11") {
        Write-Success "Windows 11 detected (Build $buildNumber)"
    }
    elseif ($osInfo.Caption -match "Windows 10") {
        if ($buildNumber -ge $MIN_WIN10_BUILD) {
            Write-Success "Windows 10 detected (Build $buildNumber)"
        }
        else {
            $warnings += "Windows 10 build $buildNumber is below minimum required build $MIN_WIN10_BUILD"
        }
    }
    else {
        $warnings += "Unsupported Windows version: $($osInfo.Caption)"
    }

    # Check RAM
    $totalRAM_GB = [math]::Round($osInfo.TotalVisibleMemorySize / 1MB, 2)
    if ($totalRAM_GB -ge $MIN_RAM_GB) {
        Write-Success "RAM: $totalRAM_GB GB"
    }
    else {
        $warnings += "RAM: $totalRAM_GB GB (recommended: at least $MIN_RAM_GB GB)"
    }

    # Check disk space
    $systemDrive = Get-PSDrive -Name C
    $freeSpace_GB = [math]::Round($systemDrive.Free / 1GB, 2)
    if ($freeSpace_GB -ge $MIN_DISK_GB) {
        Write-Success "Available disk space: $freeSpace_GB GB"
    }
    else {
        $warnings += "Available disk space: $freeSpace_GB GB (recommended: at least $MIN_DISK_GB GB)"
    }

    # Display warnings
    if ($warnings.Count -gt 0) {
        Write-Host ""
        Write-WarningMsg "System requirement warnings:"
        foreach ($warning in $warnings) {
            Write-Host "  - $warning" -ForegroundColor Yellow
        }
        Write-Host ""

        if (-not $Force) {
            $continue = Read-Host "Continue anyway? (y/N)"
            if ($continue -notmatch "^[Yy]$") {
                Write-Host "Setup aborted by user."
                exit 1
            }
        }
    }
}

function Enable-WSLFeature {
    Write-Step "Checking WSL installation status..."

    $wslFeature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux
    $vmFeature = Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform

    $needsReboot = $false

    # Enable WSL if not enabled
    if ($wslFeature.State -ne "Enabled") {
        Write-Host "Enabling Windows Subsystem for Linux..."
        Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux -NoRestart | Out-Null
        Write-Success "WSL feature enabled"
        $needsReboot = $true
    }
    else {
        Write-Success "WSL feature already enabled"
    }

    # Enable Virtual Machine Platform if not enabled
    if ($vmFeature.State -ne "Enabled") {
        Write-Host "Enabling Virtual Machine Platform..."
        Enable-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform -NoRestart | Out-Null
        Write-Success "Virtual Machine Platform enabled"
        $needsReboot = $true
    }
    else {
        Write-Success "Virtual Machine Platform already enabled"
    }

    # Prompt for reboot if needed
    if ($needsReboot) {
        Write-Host ""
        Write-WarningMsg "A system reboot is required to complete WSL installation."
        Write-Host "After rebooting, please run this script again to continue setup."
        Write-Host ""

        if ($Force) {
            $rebootNow = "y"
        }
        else {
            $rebootNow = Read-Host "Reboot now? (Y/n)"
        }

        if ($rebootNow -match "^[Yy]$" -or [string]::IsNullOrWhiteSpace($rebootNow)) {
            Write-Host "Rebooting system..."
            Restart-Computer -Force
            exit 0
        }
        else {
            Write-Host "Please reboot your system and run this script again."
            exit 0
        }
    }

    return $needsReboot
}

function Set-WSL2AsDefault {
    Write-Step "Configuring WSL2 as default version..."

    try {
        wsl --set-default-version 2 2>&1 | Out-Null
        Write-Success "WSL2 set as default version"
    }
    catch {
        Write-WarningMsg "Could not set WSL2 as default (this may be normal if WSL is not fully initialized)"
    }
}

function Install-WSLDistribution {
    Write-Step "Checking for WSL distribution..."

    $distros = wsl --list --verbose 2>&1 | Select-Object -Skip 1

    if ($LASTEXITCODE -ne 0 -or $distros.Count -eq 0) {
        Write-Host "No WSL distribution found. Installing Ubuntu..."

        try {
            wsl --install -d Ubuntu --no-launch
            Write-Success "Ubuntu installed successfully"

            Write-Host ""
            Write-Host "Launching Ubuntu for first-time setup..."
            Write-Host "Please create a username and password when prompted."
            Write-Host ""

            Start-Process "ubuntu.exe" -Wait

            # Wait for WSL to be fully initialized
            Write-Host "Waiting for WSL to initialize..."
            Start-Sleep -Seconds 5
        }
        catch {
            Write-ErrorMsg "Failed to install Ubuntu: $_"
            Write-Host ""
            Write-Host "Please install Ubuntu manually:"
            Write-Host "1. Open Microsoft Store"
            Write-Host "2. Search for 'Ubuntu'"
            Write-Host "3. Install Ubuntu 22.04 LTS"
            Write-Host "4. Run this script again"
            exit 1
        }
    }
    else {
        Write-Success "WSL distribution already exists"

        # Check if distribution is WSL1 and upgrade if needed
        $defaultDistro = wsl --list --verbose 2>&1 | Select-String "^\*" | ForEach-Object { $_ -replace "^\*\s+", "" } | ForEach-Object { ($_ -split "\s+")[0] }

        if ($defaultDistro) {
            $version = wsl --list --verbose 2>&1 | Select-String $defaultDistro | ForEach-Object { ($_ -split "\s+")[-2] }

            if ($version -eq "1") {
                Write-Host "Upgrading $defaultDistro from WSL1 to WSL2..."
                wsl --set-version $defaultDistro 2
                Write-Success "Upgraded to WSL2"
            }
            else {
                Write-Success "Distribution is already running WSL2"
            }
        }
    }
}

function Install-DockerInWSL {
    Write-Step "Checking Docker installation in WSL..."

    $dockerInstalled = wsl bash -c "command -v docker" 2>$null

    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($dockerInstalled)) {
        Write-Host "Installing Docker Engine in WSL..."
        Write-Host "This may take several minutes..."

        # Download and run Docker installation script
        wsl bash -c "curl -fsSL https://get.docker.com | sudo sh"

        if ($LASTEXITCODE -ne 0) {
            Write-ErrorMsg "Docker installation failed"
            exit 1
        }

        Write-Success "Docker Engine installed"
    }
    else {
        Write-Success "Docker Engine already installed"
    }

    # Add current user to docker group
    Write-Host "Configuring Docker permissions..."
    wsl bash -c "sudo usermod -aG docker `$USER"

    # Enable Docker service (try systemd first, fall back to service)
    Write-Host "Enabling Docker to start on boot..."
    wsl bash -c 'sudo systemctl enable docker 2>/dev/null || true'

    # Start Docker daemon
    Write-Host "Starting Docker daemon..."
    wsl bash -c 'sudo service docker start 2>/dev/null || sudo systemctl start docker 2>/dev/null'

    # Wait for Docker to be ready
    Write-Host "Waiting for Docker daemon to be ready..."
    $retries = 10
    for ($i = 1; $i -le $retries; $i++) {
        Start-Sleep -Seconds 2
        wsl bash -c "docker info" 2>$null
        if ($LASTEXITCODE -eq 0) {
            Write-Success "Docker daemon is running"
            break
        }
        if ($i -eq $retries) {
            Write-ErrorMsg "Docker daemon failed to start"
            Write-Host "Try running: wsl --shutdown"
            Write-Host "Then run this script again"
            exit 1
        }
    }
}

function Set-WSLNetworkingFixes {
    Write-Step "Applying WSL networking fixes for Docker registry connectivity..."

    # Fix 1: Configure IPv4 preference in /etc/gai.conf
    Write-Host "Configuring IPv4 preference..."
    $gaiConfCmd = "if ! grep -qF 'precedence ::ffff:0:0/96 100' /etc/gai.conf 2>/dev/null; then echo 'precedence ::ffff:0:0/96 100' | sudo tee -a /etc/gai.conf > /dev/null; fi"
    wsl bash -c $gaiConfCmd

    # Fix 2: Disable IPv6 routing
    Write-Host "Disabling IPv6 routing..."
    wsl bash -c 'sudo sysctl -w net.ipv6.conf.all.disable_ipv6=1 > /dev/null 2>&1'
    wsl bash -c 'sudo sysctl -w net.ipv6.conf.default.disable_ipv6=1 > /dev/null 2>&1'

    # Make IPv6 settings persistent
    $ipv6PersistCmd = @'
if ! grep -q '^net.ipv6.conf.all.disable_ipv6=1' /etc/sysctl.conf 2>/dev/null; then
    echo 'net.ipv6.conf.all.disable_ipv6=1' | sudo tee -a /etc/sysctl.conf > /dev/null
fi
if ! grep -q '^net.ipv6.conf.default.disable_ipv6=1' /etc/sysctl.conf 2>/dev/null; then
    echo 'net.ipv6.conf.default.disable_ipv6=1' | sudo tee -a /etc/sysctl.conf > /dev/null
fi
'@
    wsl bash -c $ipv6PersistCmd

    # Fix 3: Add Google DNS to resolv.conf
    Write-Host "Configuring DNS..."
    $dnsConfigCmd = @'
if ! grep -q 'nameserver 8.8.8.8' /etc/resolv.conf 2>/dev/null; then
    sudo cp /etc/resolv.conf /etc/resolv.conf.backup.$(date +%s) 2>/dev/null || true
    echo 'nameserver 8.8.8.8' | sudo tee -a /etc/resolv.conf > /dev/null
fi
'@
    wsl bash -c $dnsConfigCmd

    # Fix 4: Prevent WSL from overwriting resolv.conf
    Write-Host "Configuring WSL to preserve DNS settings..."
    $wslConfCmd = @'
if grep -q 'generateResolvConf *= *false' /etc/wsl.conf 2>/dev/null; then
    : # Already configured
elif grep -q 'generateResolvConf' /etc/wsl.conf 2>/dev/null; then
    sudo sed -i 's/generateResolvConf *= *true/generateResolvConf = false/' /etc/wsl.conf
elif grep -q '^\[network\]' /etc/wsl.conf 2>/dev/null; then
    sudo sed -i '/^\[network\]/a generateResolvConf = false' /etc/wsl.conf
else
    printf '\n[network]\ngenerateResolvConf = false\n' | sudo tee -a /etc/wsl.conf > /dev/null
fi
'@
    wsl bash -c $wslConfCmd

    Write-Success "Networking fixes applied"

    # Test Docker registry connectivity
    Write-Host "Testing Docker registry connectivity..."
    $maxRetries = 5
    $backoffSeconds = 2
    $connected = $false

    for ($attempt = 1; $attempt -le $maxRetries; $attempt++) {
        Write-Host "  Attempt $attempt of $maxRetries..."

        wsl bash -c "curl -s --max-time 10 https://registry-1.docker.io/v2/" 2>$null
        if ($LASTEXITCODE -eq 0) {
            $connected = $true
            break
        }

        if ($attempt -lt $maxRetries) {
            Write-Host "  Connection failed. Waiting $backoffSeconds seconds..."
            Start-Sleep -Seconds $backoffSeconds
            $backoffSeconds *= 2
        }
    }

    if ($connected) {
        Write-Success "Docker registry is reachable"

        # Clear Docker build cache from any previous failed attempts
        Write-Host "Clearing Docker build cache..."
        wsl bash -c 'docker builder prune -af > /dev/null 2>&1 || true'
    }
    else {
        Write-WarningMsg "Cannot reach Docker registry after $maxRetries attempts"
        Write-Host ""
        Write-Host "If you continue to have connectivity issues:"
        Write-Host "1. Close this PowerShell window"
        Write-Host "2. Open PowerShell as Administrator"
        Write-Host "3. Run: wsl --shutdown"
        Write-Host "4. Run this script again"
        Write-Host ""
    }
}

function Test-DockerInstallation {
    Write-Step "Verifying Docker installation..."

    # Test docker info
    Write-Host "Running 'docker info'..."
    wsl bash -c "docker info" 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Docker info command failed"
        return $false
    }
    Write-Success "Docker daemon is responsive"

    # Test docker run
    Write-Host "Testing Docker with hello-world image..."
    wsl bash -c "docker run --rm hello-world" 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Docker run test failed"
        return $false
    }
    Write-Success "Docker run test passed"

    return $true
}

# ============================================================================
# Main Script
# ============================================================================

Write-Host @"

╔════════════════════════════════════════════════════════════════════════╗
║                                                                        ║
║              Proto Fleet - WSL2 & Docker Setup for Windows            ║
║                                                                        ║
╚════════════════════════════════════════════════════════════════════════╝

"@ -ForegroundColor Cyan

# Check for Administrator privileges
if (-not (Test-Administrator)) {
    Write-ErrorMsg "This script must be run as Administrator."
    Write-Host ""
    Write-Host "To run as Administrator:"
    Write-Host "1. Right-click on PowerShell"
    Write-Host "2. Select 'Run as Administrator'"
    Write-Host "3. Run this script again"
    exit 1
}

Write-Success "Running with Administrator privileges"

# System requirements check
Test-SystemRequirements

# Enable WSL features (will reboot if needed)
Enable-WSLFeature

# Set WSL2 as default
Set-WSL2AsDefault

# Install WSL distribution
Install-WSLDistribution

# Install Docker Engine in WSL
Install-DockerInWSL

# Apply WSL networking fixes
Set-WSLNetworkingFixes

# Verify installation
if (Test-DockerInstallation) {
    Write-Host ""
    Write-Host "╔════════════════════════════════════════════════════════════════════════╗" -ForegroundColor Green
    Write-Host "║                                                                        ║" -ForegroundColor Green
    Write-Host "║                      Setup completed successfully!                     ║" -ForegroundColor Green
    Write-Host "║                                                                        ║" -ForegroundColor Green
    Write-Host "╚════════════════════════════════════════════════════════════════════════╝" -ForegroundColor Green
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Cyan
    Write-Host "  1. Run .\install-fleet.ps1 to install Proto Fleet"
    Write-Host ""
    Write-Host "Useful commands:" -ForegroundColor Cyan
    Write-Host "  - Enter WSL:              wsl"
    Write-Host "  - Check Docker:           wsl docker info"
    Write-Host "  - Restart WSL:            wsl --shutdown"
    Write-Host ""
}
else {
    Write-Host ""
    Write-ErrorMsg "Setup verification failed."
    Write-Host ""
    Write-Host "Troubleshooting steps:"
    Write-Host "1. Run: wsl --shutdown"
    Write-Host "2. Run this script again"
    Write-Host -- "3. If issues persist, check Docker logs: wsl sudo journalctl -u docker"
    exit 1
}
