# Proto Fleet Installation for Windows

Complete guide for installing and running Proto Fleet on Windows using WSL2 and Docker Engine.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Detailed Installation](#detailed-installation)
- [Configuration Options](#configuration-options)
- [Managing Proto Fleet](#managing-proto-fleet)
- [Troubleshooting](#troubleshooting)
- [Advanced Usage](#advanced-usage)

## Overview

Proto Fleet runs on Windows via WSL2 (Windows Subsystem for Linux) with Docker Engine. This approach provides:

- **Native Linux performance** - Full Linux kernel in WSL2
- **Lightweight** - No Docker Desktop required
- **Network isolation** - Uses WSL networking with automatic fixes
- **Easy management** - PowerShell scripts handle everything

### Architecture

```
Windows Host
    ↓
WSL2 (Ubuntu)
    ↓
Docker Engine
    ↓
Proto Fleet (Docker Compose)
```

## Prerequisites

### System Requirements

- **Operating System**: Windows 10 (build 19041+) or Windows 11
- **RAM**: 8 GB minimum (16 GB recommended)
- **Disk Space**: 20 GB free space minimum
- **Processor**: 64-bit processor with virtualization support
- **Network**: Internet connection for downloads

### Required Software

All required software will be installed automatically by the setup script:

- WSL2 (Windows Subsystem for Linux)
- Ubuntu distribution (installed in WSL)
- Docker Engine (installed inside WSL)

### Administrator Privileges

The setup script requires Administrator privileges to:

- Enable WSL and Virtual Machine Platform features
- Configure system settings
- Install and configure Docker

### PowerShell Execution Policy

**Important**: Windows blocks unsigned PowerShell scripts by default. You must allow script execution before running the installation scripts.

#### Option 1: Bypass Policy for Single Execution (Recommended)

Run scripts with the `-ExecutionPolicy Bypass` flag:

```powershell
powershell -ExecutionPolicy Bypass -File .\setup-wsl-docker.ps1
powershell -ExecutionPolicy Bypass -File .\install-fleet.ps1
```

This temporarily bypasses the policy for that single command without changing system settings.

#### Option 2: Unblock Downloaded Files

If you downloaded the scripts from the internet, unblock them:

```powershell
Unblock-File -Path .\setup-wsl-docker.ps1
Unblock-File -Path .\install-fleet.ps1
```

Then run normally:

```powershell
.\setup-wsl-docker.ps1
.\install-fleet.ps1
```

#### Option 3: Set Policy for Current Session

Temporarily allow scripts for your current PowerShell session:

```powershell
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process
```

Then run scripts normally. The policy reverts when you close PowerShell.

**Note**: Once code signing is implemented in CI/CD, these scripts will be signed and will run without requiring policy changes.

## Quick Start

For a fast installation with default settings:

```powershell
# 1. Open PowerShell as Administrator
# (Right-click PowerShell → "Run as Administrator")

# 2. Navigate to the windows directory
cd path\to\proto-fleet\deployment-files\windows

# 3. Allow script execution (scripts are unsigned until code signing is implemented)
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process

# 4. Run setup script (sets up WSL2 and Docker)
.\setup-wsl-docker.ps1

# 5. Run installation script (installs Proto Fleet)
.\install-fleet.ps1
```

**Notes**:
- If the setup script prompts for a reboot, restart your computer and run the setup script again before proceeding to the installation script.
- The execution policy change in step 3 only affects the current PowerShell session. See [PowerShell Execution Policy](#powershell-execution-policy) for alternative approaches.

## Detailed Installation

### Step 1: Download Installation Scripts

Download the Proto Fleet installation scripts:

1. Clone the repository, or
2. Download the `windows` folder from the repository

### Step 2: Open PowerShell as Administrator

1. Press `Win + X`
2. Select **Windows PowerShell (Admin)** or **Terminal (Admin)**
3. Click **Yes** when prompted by User Account Control

### Step 3: Allow Script Execution

Navigate to the scripts directory:

```powershell
cd C:\path\to\proto-fleet\deployment-files\windows
```

Allow the unsigned scripts to run (required until code signing is implemented):

```powershell
# Option A: Set policy for current session (recommended)
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process

# Option B: Unblock the downloaded files
Unblock-File -Path .\setup-wsl-docker.ps1
Unblock-File -Path .\install-fleet.ps1

# Option C: Run with bypass flag (shown in Step 4)
```

See the [PowerShell Execution Policy](#powershell-execution-policy) section for more details.

### Step 4: Run the Setup Script

Run the setup script:

```powershell
# If you used Option A or B above:
.\setup-wsl-docker.ps1

# If you prefer Option C (bypass flag):
powershell -ExecutionPolicy Bypass -File .\setup-wsl-docker.ps1
```

The script will:

1. ✓ Check system requirements (warnings only, not blocking)
2. ✓ Enable WSL and Virtual Machine Platform features
3. ✓ Install Ubuntu distribution (if not present)
4. ✓ Set WSL2 as default version
5. ✓ Install Docker Engine inside WSL
6. ✓ Configure Docker to start automatically
7. ✓ Apply networking fixes for Docker registry access
8. ✓ Verify installation with test containers

**Reboot if Required**: If the script reports that a reboot is needed, restart your computer and run the setup script again (remember to set the execution policy again after reboot).

### Step 5: Run the Installation Script

After WSL2 and Docker are set up:

```powershell
# If you used Option A or B in Step 3:
.\install-fleet.ps1

# If you prefer Option C (bypass flag):
powershell -ExecutionPolicy Bypass -File .\install-fleet.ps1
```

The script will:

1. ✓ Check prerequisites (WSL2 and Docker)
2. ✓ Download Proto Fleet from S3
3. ✓ Transfer to WSL filesystem
4. ✓ Configure environment variables
5. ✓ Set up SSL/TLS (optional)
6. ✓ Deploy services with Docker Compose
7. ✓ Verify services are healthy

### Step 6: Access Proto Fleet

Once installation completes, access Proto Fleet in your browser:

- **Local**: http://localhost (or https://localhost if you chose HTTPS)
- **LAN**: Use the IP addresses shown in the installation output

## Configuration Options

### Installation Parameters

The installation script supports several parameters:

```powershell
# Install a specific version
.\install-fleet.ps1 -Version v0.1.0-beta-5

# Use a pre-configured environment file
.\install-fleet.ps1 -ConfigFile .\my-config.env

# Install to a custom directory
.\install-fleet.ps1 -InstallDir ~/custom-location

# Skip confirmation prompts
.\install-fleet.ps1 -Force
```

### Environment Configuration

You have two options for configuring Proto Fleet:

#### Option 1: Auto-Generated Credentials (Recommended)

The script will automatically generate secure random passwords for:

- MySQL root password
- Database user password
- Auth client secret key
- Encryption service master key

**Benefits**:
- Strong cryptographically secure passwords
- No need to remember or manage passwords
- Stored securely in `.env` file

#### Option 2: Custom Credentials

Provide your own passwords during installation. Useful for:

- Organizations with specific password policies
- Integration with existing systems
- Compliance requirements

**Requirements**:
- Auth client secret key: minimum 32 characters
- Encryption master key: 32-byte Base64-encoded value

### SSL/TLS Options

Choose your security mode during installation:

#### 1. HTTP Only (No Encryption)

- **Use case**: Isolated LANs, testing, development
- **Pros**: Simplest setup, no certificate warnings
- **Cons**: No encryption, not suitable for untrusted networks

#### 2. HTTPS with Self-Signed Certificate

- **Use case**: Most installations, internal networks
- **Pros**: Encrypted traffic, automatically generated
- **Cons**: Browser warnings (can be accepted or cert can be trusted)

The script generates certificates valid for:
- `localhost`
- All local IP addresses
- Machine hostname

#### 3. HTTPS with Custom Certificates

- **Use case**: Production, public access, corporate environments
- **Pros**: No browser warnings, professional setup
- **Cons**: Requires obtaining certificates

To use custom certificates:

1. Obtain certificate files: `cert.pem` and `key.pem`
2. Place them in `deployment/ssl/` directory
3. Run the installation script

### Pre-Configured Installation

For automated/scripted deployments, create a `.env` file with required variables:

```bash
MYSQL_ROOT_PASSWORD=your-secure-password
DB_USERNAME=fleet_user
DB_PASSWORD=your-secure-password
AUTH_CLIENT_SECRET_KEY=your-32-character-minimum-secret-key
ENCRYPT_SERVICE_MASTER_KEY=your-base64-encoded-32-byte-key
SESSION_COOKIE_SECURE=false
```

Then install:

```powershell
.\install-fleet.ps1 -ConfigFile .\my-config.env
```

## Managing Proto Fleet

### Accessing WSL

Enter the WSL environment:

```powershell
wsl
```

Exit WSL:

```bash
exit
```

### Service Management

All commands can be run from PowerShell or from within WSL.

#### View Logs

```powershell
# From PowerShell
wsl bash -c "cd ~/proto-fleet/deployment && docker compose logs -f"

# From WSL
cd ~/proto-fleet/deployment
docker compose logs -f
```

Press `Ctrl+C` to stop following logs.

#### Stop Services

```powershell
# From PowerShell
wsl bash -c "cd ~/proto-fleet/deployment && docker compose down"

# From WSL
cd ~/proto-fleet/deployment
docker compose down
```

#### Start Services

```powershell
# From PowerShell
wsl bash -c "cd ~/proto-fleet/deployment && docker compose up -d"

# From WSL
cd ~/proto-fleet/deployment
docker compose up -d
```

#### Restart Services

```powershell
# From PowerShell
wsl bash -c "cd ~/proto-fleet/deployment && docker compose restart"

# From WSL
cd ~/proto-fleet/deployment
docker compose restart
```

#### Check Service Status

```powershell
# From PowerShell
wsl bash -c "cd ~/proto-fleet/deployment && docker compose ps"

# From WSL
cd ~/proto-fleet/deployment
docker compose ps
```

### Upgrading Proto Fleet

To upgrade to a new version:

```powershell
# Install new version (existing config will be preserved)
.\install-fleet.ps1 -Version v0.2.0
```

Your configuration (`.env` file) and data volumes will be preserved during upgrades.

### Uninstalling

To completely remove Proto Fleet:

```powershell
# Enter WSL
wsl

# Navigate to deployment directory
cd ~/proto-fleet/deployment

# Stop and remove containers
docker compose down

# Remove volumes (WARNING: deletes all data)
docker compose down -v

# Exit WSL
exit

# Remove installation directory (from WSL)
wsl bash -c "rm -rf ~/proto-fleet"
```

## Troubleshooting

### Common Issues

#### Issue: "Cannot run scripts" or "Execution policy" error

When trying to run the PowerShell scripts, you may see an error like:

```
.\setup-wsl-docker.ps1 : File cannot be loaded because running scripts is disabled on this system.
```

or

```
.\setup-wsl-docker.ps1 : File cannot be loaded. The file is not digitally signed.
```

**Cause**: Windows blocks unsigned PowerShell scripts by default for security. Our scripts are currently unsigned (code signing will be implemented in CI/CD pipeline).

**Solution** (choose one):

**Option 1: Run with bypass flag** (recommended for one-time execution):
```powershell
powershell -ExecutionPolicy Bypass -File .\setup-wsl-docker.ps1
powershell -ExecutionPolicy Bypass -File .\install-fleet.ps1
```

**Option 2: Unblock downloaded files**:
```powershell
Unblock-File -Path .\setup-wsl-docker.ps1
Unblock-File -Path .\install-fleet.ps1
```

**Option 3: Set policy for current session**:
```powershell
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process
# Now run scripts normally
.\setup-wsl-docker.ps1
```

**Option 4: Check current policy**:
```powershell
Get-ExecutionPolicy -List
```

See the [PowerShell Execution Policy](#powershell-execution-policy) section for detailed explanations.

#### Issue: "WSL is not installed"

**Solution**:
1. Ensure you ran `setup-wsl-docker.ps1` as Administrator
2. If the script requested a reboot, restart and run the script again
3. Verify WSL is enabled: `wsl --status`

#### Issue: "Docker is not running in WSL"

**Solution**:
```powershell
# Restart WSL
wsl --shutdown

# Wait a few seconds, then start Docker
wsl bash -c "sudo service docker start"

# Verify Docker is running
wsl bash -c "docker info"
```

#### Issue: "Cannot reach Docker registry"

This indicates networking issues in WSL.

**Solution**:
```powershell
# Shutdown WSL completely
wsl --shutdown

# Restart and re-run setup script
.\setup-wsl-docker.ps1
```

The setup script applies networking fixes automatically.

#### Issue: "Browser shows security warning" (HTTPS with self-signed cert)

This is expected behavior for self-signed certificates.

**Options**:
1. **Accept the warning**: Click "Advanced" → "Proceed to localhost"
2. **Trust the certificate**: Import `ssl/cert.pem` into your OS trust store
3. **Use custom certificate**: Obtain a proper certificate and re-run installation

#### Issue: "Services fail to start"

**Solution**:
```powershell
# Check service logs
wsl bash -c "cd ~/proto-fleet/deployment && docker compose logs"

# Check for port conflicts
wsl bash -c "docker compose ps -a"

# Restart Docker daemon
wsl --shutdown
wsl bash -c "sudo service docker start"

# Try starting services again
wsl bash -c "cd ~/proto-fleet/deployment && docker compose up -d"
```

#### Issue: "Out of disk space"

**Solution**:
```powershell
# Check disk usage
wsl bash -c "df -h"

# Clean up Docker resources
wsl bash -c "docker system prune -a --volumes"

# Free up WSL disk space (from PowerShell)
wsl --shutdown
Optimize-VHD -Path $env:LOCALAPPDATA\Packages\CanonicalGroupLimited.Ubuntu_*\LocalState\ext4.vhdx -Mode Full
```

### WSL-Specific Issues

#### Issue: WSL is slow or unresponsive

**Solution**:
```powershell
# Restart WSL
wsl --shutdown

# Wait 8-10 seconds for complete shutdown
Start-Sleep -Seconds 10

# Restart WSL
wsl
```

#### Issue: WSL uses too much memory

Create or edit `%USERPROFILE%\.wslconfig`:

```ini
[wsl2]
memory=4GB
processors=2
```

Then restart WSL:

```powershell
wsl --shutdown
```

### Getting Help

#### View Detailed Logs

```powershell
# All services
wsl bash -c "cd ~/proto-fleet/deployment && docker compose logs"

# Specific service
wsl bash -c "cd ~/proto-fleet/deployment && docker compose logs fleet-api"

# Follow logs in real-time
wsl bash -c "cd ~/proto-fleet/deployment && docker compose logs -f"
```

#### Check Docker Status

```powershell
# Docker daemon info
wsl bash -c "docker info"

# Running containers
wsl bash -c "docker ps"

# All containers (including stopped)
wsl bash -c "docker ps -a"

# Docker disk usage
wsl bash -c "docker system df"
```

#### Verify Network Connectivity

```powershell
# Test Docker registry
wsl bash -c "curl -s https://registry-1.docker.io/v2/"

# Test DNS resolution
wsl bash -c "nslookup google.com"

# Check WSL IP configuration
wsl bash -c "ip addr show"
```

## Advanced Usage

### Custom Installation Directory

By default, Proto Fleet installs to `~/proto-fleet` in WSL. To use a different location:

```powershell
.\install-fleet.ps1 -InstallDir ~/custom-location
```

### Non-Interactive Installation

For automation or scripting:

```powershell
# Create config file
$config = @"
MYSQL_ROOT_PASSWORD=$(wsl bash -c 'openssl rand -base64 16')
DB_USERNAME=fleet_user
DB_PASSWORD=$(wsl bash -c 'openssl rand -base64 16')
AUTH_CLIENT_SECRET_KEY=$(wsl bash -c 'openssl rand -base64 32')
ENCRYPT_SERVICE_MASTER_KEY=$(wsl bash -c 'openssl rand -base64 32')
SESSION_COOKIE_SECURE=false
"@

Set-Content -Path "config.env" -Value $config

# Install with config file
.\install-fleet.ps1 -ConfigFile .\config.env -Force
```

### Accessing WSL Filesystem from Windows

WSL filesystems are accessible from Windows:

```
\\wsl$\Ubuntu\home\<username>\proto-fleet
```

You can browse this in File Explorer, but always use WSL commands for file operations to avoid permission issues.

### Port Forwarding

WSL2 automatically forwards ports from WSL to Windows. Proto Fleet is accessible at:

- `localhost` from Windows
- WSL IP address from other devices on your LAN

To find your WSL IP:

```powershell
wsl bash -c "hostname -I"
```

### Custom Docker Compose Configuration

The Docker Compose file is located at:

```
~/proto-fleet/deployment/docker-compose.yaml
```

After modifying, restart services:

```powershell
wsl bash -c "cd ~/proto-fleet/deployment && docker compose up -d"
```

### Database Access

Proto Fleet uses MySQL and InfluxDB:

#### MySQL (Fleet Data)

```bash
# Enter MySQL container
wsl bash -c "cd ~/proto-fleet/deployment && docker compose exec mysql mysql -u root -p"

# Enter password from .env file (MYSQL_ROOT_PASSWORD)
```

#### InfluxDB (Telemetry Data)

```bash
# Access InfluxDB CLI
wsl bash -c "cd ~/proto-fleet/deployment && docker compose exec influxdb influx"
```

### Backup and Restore

#### Backup

```powershell
# Create backup directory
wsl bash -c "mkdir -p ~/proto-fleet-backups"

# Backup configuration
wsl bash -c "cp ~/proto-fleet/deployment/.env ~/proto-fleet-backups/env-backup-$(date +%Y%m%d).txt"

# Backup database
wsl bash -c "cd ~/proto-fleet/deployment && docker compose exec -T mysql mysqldump -u root -p<password> fleet > ~/proto-fleet-backups/mysql-backup-$(date +%Y%m%d).sql"
```

#### Restore

```powershell
# Restore configuration
wsl bash -c "cp ~/proto-fleet-backups/env-backup-<date>.txt ~/proto-fleet/deployment/.env"

# Restore database
wsl bash -c "cd ~/proto-fleet/deployment && docker compose exec -T mysql mysql -u root -p<password> fleet < ~/proto-fleet-backups/mysql-backup-<date>.sql"

# Restart services
wsl bash -c "cd ~/proto-fleet/deployment && docker compose restart"
```

## Additional Resources

### Useful Commands Reference

```powershell
# WSL Management
wsl --list --verbose              # List WSL distributions
wsl --status                      # WSL status
wsl --shutdown                    # Shutdown all WSL instances
wsl --update                      # Update WSL

# Docker in WSL
wsl bash -c "docker ps"           # List running containers
wsl bash -c "docker images"       # List images
wsl bash -c "docker system df"    # Disk usage
wsl bash -c "docker system prune" # Clean up

# Proto Fleet
wsl bash -c "cd ~/proto-fleet/deployment && docker compose ps"      # Status
wsl bash -c "cd ~/proto-fleet/deployment && docker compose logs -f" # Logs
wsl bash -c "cd ~/proto-fleet/deployment && docker compose restart" # Restart
```

### Documentation Links

- [WSL Documentation](https://docs.microsoft.com/en-us/windows/wsl/)
- [Docker Documentation](https://docs.docker.com/)
- [Docker Compose Documentation](https://docs.docker.com/compose/)

### Support

For issues or questions:

1. Check the [Troubleshooting](#troubleshooting) section
2. Review Docker Compose logs
3. Contact your Proto Fleet administrator or support team

---

**Version**: 1.0
**Last Updated**: January 2026
**Compatibility**: Windows 10 (19041+), Windows 11
