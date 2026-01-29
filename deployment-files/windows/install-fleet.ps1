<#
.SYNOPSIS
    Downloads, configures, and deploys Proto Fleet on Windows using WSL2 and Docker.

.DESCRIPTION
    This script automates the complete installation of Proto Fleet on Windows systems.
    It handles:
    - Auto-detection of extracted deployment files (beta use case)
    - Version resolution and download from S3 (when not using local tarball)
    - Transfer to WSL filesystem
    - Configuration management (interactive or file-based)
    - SSL/TLS certificate setup
    - Docker Compose deployment
    - Health checks and status reporting

.PARAMETER Version
    Specific version to install (e.g., "v0.1.0-beta-5"). Defaults to "latest".
    Ignored if TarballPath is specified.

.PARAMETER TarballPath
    Path to a local tarball file (proto-fleet-*.tar.gz) to install from.
    If specified, the script will skip auto-detection and downloading from S3.
    This is useful for automation and closed beta installations where S3 is not accessible.
    If not specified, the script will auto-detect if running from an extracted tarball.

.PARAMETER ConfigFile
    Path to a pre-created configuration file (.env format). If not specified, the script
    will prompt for configuration or use existing configuration if found.

.PARAMETER InstallDir
    Installation directory in WSL. Defaults to "~/proto-fleet".

.PARAMETER Force
    Skip confirmation prompts where possible.

.EXAMPLE
    .\install-fleet.ps1
    Auto-detect deployment files in current directory, or prompt for tarball path.
    Recommended for beta customers: extract tarball, cd to deployment/windows/, then run.

.EXAMPLE
    .\install-fleet.ps1 -TarballPath C:\Downloads\proto-fleet-v0.1.0-beta-5.tar.gz
    Install from specific tarball (automation mode - skips auto-detection)

.EXAMPLE
    .\install-fleet.ps1 -Force
    Auto-detect and proceed without prompts (for automation)

.EXAMPLE
    .\install-fleet.ps1 -ConfigFile .\my-config.env
    Install with a pre-configured environment file

.EXAMPLE
    .\install-fleet.ps1 -InstallDir ~/custom-location
    Install to a custom directory

.NOTES
    - Requires WSL2 and Docker to be set up (run setup-wsl-docker.ps1 first)
    - For beta deployments: Extract tarball, cd to deployment/windows/, and run script
    - The script will auto-detect extracted deployment files
    - No internet connection required when using local tarball (all files included)
    - Supports automation with -TarballPath parameter (skips auto-detection)
    - Creates .env file with sensitive credentials (stored securely)
#>

[CmdletBinding()]
param(
    [string]$Version = "latest",
    [string]$TarballPath = "",
    [string]$ConfigFile = "",
    [string]$InstallDir = "~/proto-fleet",
    [switch]$Force
)

$ErrorActionPreference = "Stop"

# Constants
$BUCKET_URL = "https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet"
$DEPLOYMENT_DIR = "deployment"
$REQUIRED_PLUGINS = @("proto-plugin-amd64", "proto-plugin-arm64", "antminer-plugin-amd64", "antminer-plugin-arm64")

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

function Test-WSLInstalled {
    try {
        $result = wsl --status 2>&1
        return $LASTEXITCODE -eq 0
    }
    catch {
        return $false
    }
}

function Test-DockerInWSL {
    try {
        wsl bash -c "docker info" 2>$null
        return $LASTEXITCODE -eq 0
    }
    catch {
        return $false
    }
}

function Get-LatestVersion {
    Write-Step "Determining latest version..."

    $versionUrl = "$BUCKET_URL/latest/version.txt"

    try {
        $versionContent = Invoke-WebRequest -Uri $versionUrl -UseBasicParsing -ErrorAction Stop
        $versionLine = $versionContent.Content -split "`n" | Where-Object { $_ -match "^version:" } | Select-Object -First 1

        if ($versionLine -match "version:\s*(.+)") {
            $latestVersion = $matches[1].Trim()
            Write-Success "Latest version is $latestVersion"
            return $latestVersion
        }
        else {
            throw "Could not parse version from version.txt"
        }
    }
    catch {
        Write-ErrorMsg "Failed to determine latest version: $_"
        exit 1
    }
}

function Get-DownloadUrl {
    param([string]$Ver)

    $tarName = "proto-fleet-$Ver.tar.gz"
    return "$BUCKET_URL/$Ver/$tarName", $tarName
}

function Download-ProtoFleet {
    param(
        [string]$Url,
        [string]$TarName
    )

    Write-Step "Downloading Proto Fleet from $Url..."

    $tempFile = [System.IO.Path]::Combine($env:TEMP, $TarName)

    try {
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $Url -OutFile $tempFile -UseBasicParsing -ErrorAction Stop
        $ProgressPreference = 'Continue'
        Write-Success "Downloaded $TarName"
        return $tempFile
    }
    catch {
        Write-ErrorMsg "Failed to download: $_"
        Write-Host ""
        Write-Host "Please check:"
        Write-Host "  - Internet connectivity"
        Write-Host "  - Version exists: $Url"
        exit 1
    }
}

function ConvertTo-WSLPath {
    param([string]$WindowsPath)

    $result = wsl wslpath -u "$WindowsPath" 2>&1
    if ($LASTEXITCODE -eq 0) {
        return $result.Trim()
    }
    else {
        throw "Failed to convert path: $WindowsPath"
    }
}

function Find-PreviousInstallDir {
    Write-Step "Checking for previous Proto Fleet installations..."

    # Try to find container with fleet-api in the name
    $findContainerCmd = @"
docker ps -a --filter 'name=${DEPLOYMENT_DIR}-fleet-api' --filter 'name=${DEPLOYMENT_DIR}_fleet-api' --format '{{.ID}}' 2>/dev/null | head -n 1 || true
"@
    $containerId = wsl bash -c $findContainerCmd

    if ([string]::IsNullOrWhiteSpace($containerId)) {
        Write-Host "No previous installation detected."
        return $null
    }

    # Get mount path from container
    $inspectCmd = @"
docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/fleet/start"}}{{.Source}}{{end}}{{end}}' '$containerId' 2>/dev/null || true
"@
    $mountPath = wsl bash -c $inspectCmd

    if ([string]::IsNullOrWhiteSpace($mountPath)) {
        return $null
    }

    # Extract installation directory
    $sedCmd = "echo '$mountPath' | sed 's|/${DEPLOYMENT_DIR}.*`$||' || true"
    $installDir = wsl bash -c $sedCmd

    if (-not [string]::IsNullOrWhiteSpace($installDir)) {
        Write-Success "Found previous installation at: $installDir"
        return $installDir.Trim()
    }

    return $null
}

function Set-InstallDirectory {
    param([string]$DefaultDir)

    $previousDir = Find-PreviousInstallDir

    if ($previousDir) {
        $suggestedDir = $previousDir
    }
    else {
        $suggestedDir = $DefaultDir
    }

    Write-Host ""
    Write-Host "Suggested installation location: $suggestedDir"

    if ($Force) {
        $useIt = "y"
    }
    else {
        $useIt = Read-Host "Use this location? (Y/n)"
    }

    if ($useIt -match "^[Nn]$") {
        $customDir = Read-Host "Enter installation directory [$DefaultDir]"
        if ([string]::IsNullOrWhiteSpace($customDir)) {
            return $DefaultDir
        }
        return $customDir
    }

    return $suggestedDir
}

function Copy-ToWSL {
    param(
        [string]$WindowsFilePath,
        [string]$WSLTempPath
    )

    Write-Step "Transferring to WSL..."

    try {
        $wslPath = ConvertTo-WSLPath -WindowsPath $WindowsFilePath
        wsl bash -c "cp '$wslPath' '$WSLTempPath'"

        if ($LASTEXITCODE -ne 0) {
            throw "Copy to WSL failed"
        }

        Write-Success "Transferred to WSL: $WSLTempPath"
    }
    catch {
        Write-ErrorMsg "Failed to transfer to WSL: $_"
        exit 1
    }
}

function Expand-InWSL {
    param(
        [string]$TarPath,
        [string]$TargetDir
    )

    Write-Step "Extracting to $TargetDir..."

    # Create target directory
    wsl bash -c "mkdir -p '$TargetDir'"

    # Check if we need to preserve existing .env file
    $envFile = "$TargetDir/$DEPLOYMENT_DIR/server/influx_config/.env"
    $checkEnvCmd = "[ -f '$envFile' ] && echo 'yes' || echo 'no'"
    $preserveEnv = wsl bash -c $checkEnvCmd

    if ($preserveEnv -eq "yes") {
        Write-Host "Preserving existing InfluxDB config .env file"
        wsl bash -c "tar -xzf '$TarPath' -C '$TargetDir' --exclude='${DEPLOYMENT_DIR}/server/influx_config/.env'"
    }
    else {
        wsl bash -c "tar -xzf '$TarPath' -C '$TargetDir'"
    }

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to extract tarball"
        exit 1
    }

    # Clean up tarball
    wsl bash -c "rm '$TarPath'"

    Write-Success "Extraction complete"

    return "$TargetDir/$DEPLOYMENT_DIR"
}

function Test-PluginBinaries {
    param([string]$DeploymentPath)

    Write-Step "Validating plugin binaries..."

    $missingPlugins = @()

    foreach ($plugin in $REQUIRED_PLUGINS) {
        $checkPluginCmd = "[ -f '$DeploymentPath/server/$plugin' ] && echo 'yes' || echo 'no'"
        $exists = wsl bash -c $checkPluginCmd
        if ($exists -ne "yes") {
            $missingPlugins += $plugin
        }
    }

    if ($missingPlugins.Count -gt 0) {
        Write-ErrorMsg "Missing plugin binaries:"
        foreach ($plugin in $missingPlugins) {
            Write-Host "  - $plugin" -ForegroundColor Red
        }
        Write-Host ""
        Write-Host "The installation package may be incomplete. Please contact support."
        exit 1
    }

    # Set executable permissions
    wsl bash -c "chmod +x '$DeploymentPath/server'/*-plugin-*"

    Write-Success "Plugin binaries validated"
}

function Test-EnvFileComplete {
    param([string]$EnvFilePath)

    $requiredKeys = @(
        "MYSQL_ROOT_PASSWORD",
        "DB_USERNAME",
        "DB_PASSWORD",
        "AUTH_CLIENT_SECRET_KEY",
        "ENCRYPT_SERVICE_MASTER_KEY"
    )

    $allPresent = $true

    foreach ($key in $requiredKeys) {
        $checkKeyCmd = "grep -q '^$key=' '$EnvFilePath' 2>/dev/null && echo 'yes' || echo 'no'"
        $hasKey = wsl bash -c $checkKeyCmd
        if ($hasKey -ne "yes") {
            Write-Host "Missing required key: $key" -ForegroundColor Red
            $allPresent = $false
        }
    }

    return $allPresent
}

function New-RandomPassword {
    param([int]$Length = 24)

    $result = wsl bash -c "openssl rand -base64 $Length"
    return $result.Trim()
}

function New-Base64Key {
    param([int]$Bytes = 32)

    $result = wsl bash -c "openssl rand -base64 $Bytes"
    return $result.Trim()
}

function Read-SecureInput {
    param([string]$Prompt)

    $secureString = Read-Host -Prompt $Prompt -AsSecureString
    $bstr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureString)
    $plainText = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)

    return $plainText
}

function New-EnvironmentFile {
    param([string]$DeploymentPath)

    Write-Step "Configuring Proto Fleet..."

    $envFile = "$DeploymentPath/.env"

    # Check for existing complete env file
    $checkExistingCmd = "[ -f '$envFile' ] && echo 'yes' || echo 'no'"
    $existingEnvFile = wsl bash -c $checkExistingCmd

    if ($existingEnvFile -eq "yes") {
        if (Test-EnvFileComplete -EnvFilePath $envFile) {
            Write-Host ""
            Write-Host "Existing environment file found with all required keys."

            if ($Force) {
                $useExisting = "y"
            }
            else {
                $useExisting = Read-Host "Use existing configuration? (Y/n)"
            }

            if ($useExisting -notmatch "^[Nn]$") {
                Write-Success "Using existing environment file"
                return $envFile
            }

            # User wants new config - prompt to remove MySQL volume
            Invoke-MySQLVolumePrompt -DeploymentPath $DeploymentPath
        }
    }

    # Create new configuration
    Write-Host ""
    Write-Host "Proto Fleet requires secure credentials for its backend services."
    Write-Host ""
    Write-Host "How would you like to configure backend passwords?"
    Write-Host "  1) Auto-generate secure passwords (Recommended)"
    Write-Host "     - System will create strong random passwords"
    Write-Host "     - You don't need to remember these passwords"
    Write-Host "     - Suitable for most installations"
    Write-Host "  2) Set custom passwords"
    Write-Host "     - You will specify each password manually"
    Write-Host "     - Only needed for specific security policies"
    Write-Host ""

    if ($Force) {
        $choice = "1"
    }
    else {
        $choice = Read-Host "Select option [1]"
        if ([string]::IsNullOrWhiteSpace($choice)) {
            $choice = "1"
        }
    }

    # Initialize env file
    wsl bash -c "touch '$envFile'"

    if ($choice -eq "2") {
        # Custom passwords
        Write-Host ""
        $mysqlRootPass = Read-SecureInput "Enter password for Database root user"
        wsl bash -c "echo 'MYSQL_ROOT_PASSWORD=$mysqlRootPass' >> '$envFile'"

        $dbUsername = Read-Host "Enter username for Database user [fleet_user]"
        if ([string]::IsNullOrWhiteSpace($dbUsername)) {
            $dbUsername = "fleet_user"
        }
        wsl bash -c "echo 'DB_USERNAME=$dbUsername' >> '$envFile'"

        $dbPassword = Read-SecureInput "Enter password for Database user"
        wsl bash -c "echo 'DB_PASSWORD=$dbPassword' >> '$envFile'"

        Write-Host ""
        Write-Host "Auth client secret key (minimum 32 characters):"
        $authKey = Read-SecureInput "Enter Auth client secret key"
        while ($authKey.Length -lt 32) {
            Write-WarningMsg "Secret key must be at least 32 characters long (current: $($authKey.Length))"
            $authKey = Read-SecureInput "Enter Auth client secret key"
        }
        wsl bash -c "echo 'AUTH_CLIENT_SECRET_KEY=$authKey' >> '$envFile'"

        Write-Host ""
        Write-Host "Encryption service master key (must be 32-byte Base64-encoded):"
        $encryptKey = Read-SecureInput "Enter Encryption service master key"

        # Validate Base64 key
        $validateKeyCmd = @"
validate_key() {
    local input="`$1"
    local temp_file=`$(mktemp)
    if ! echo "`$input" | base64 -d > "`$temp_file" 2>/dev/null; then
        rm -f "`$temp_file"
        return 1
    fi
    local byte_length=`$(wc -c < "`$temp_file")
    rm -f "`$temp_file"
    if [ "`$byte_length" -ne 32 ]; then
        return 2
    fi
    return 0
}
validate_key '$encryptKey' && echo 'valid' || echo 'invalid'
"@
        $valid = wsl bash -c $validateKeyCmd

        while ($valid -ne "valid") {
            Write-WarningMsg "The provided key is not valid Base64 or doesn't decode to 32 bytes"
            $encryptKey = Read-SecureInput "Enter Encryption service master key"
            $validateKeyCmd = @"
validate_key() {
    local input="`$1"
    local temp_file=`$(mktemp)
    if ! echo "`$input" | base64 -d > "`$temp_file" 2>/dev/null; then
        rm -f "`$temp_file"
        return 1
    fi
    local byte_length=`$(wc -c < "`$temp_file")
    rm -f "`$temp_file"
    if [ "`$byte_length" -ne 32 ]; then
        return 2
    fi
    return 0
}
validate_key '$encryptKey' && echo 'valid' || echo 'invalid'
"@
            $valid = wsl bash -c $validateKeyCmd
        }

        wsl bash -c "echo 'ENCRYPT_SERVICE_MASTER_KEY=$encryptKey' >> '$envFile'"
    }
    else {
        # Auto-generate passwords
        Write-Host ""
        Write-Host "Generating secure backend credentials..."

        $mysqlRootPass = New-RandomPassword
        $dbUsername = "fleet_user"
        $dbPassword = New-RandomPassword
        $authKey = New-Base64Key
        $encryptKey = New-Base64Key

        wsl bash -c "echo 'MYSQL_ROOT_PASSWORD=$mysqlRootPass' >> '$envFile'"
        wsl bash -c "echo 'DB_USERNAME=$dbUsername' >> '$envFile'"
        wsl bash -c "echo 'DB_PASSWORD=$dbPassword' >> '$envFile'"
        wsl bash -c "echo 'AUTH_CLIENT_SECRET_KEY=$authKey' >> '$envFile'"
        wsl bash -c "echo 'ENCRYPT_SERVICE_MASTER_KEY=$encryptKey' >> '$envFile'"

        Write-Success "Generated secure backend credentials"
    }

    # Set permissions
    wsl bash -c "chmod 600 '$envFile'"

    Write-Success "Environment configuration saved to $envFile"

    return $envFile
}

function Set-SSLConfiguration {
    param([string]$DeploymentPath)

    Write-Step "Configuring SSL/TLS..."

    $sslDir = "$DeploymentPath/ssl"
    $sslCert = "$sslDir/cert.pem"
    $sslKey = "$sslDir/key.pem"

    # Ensure SSL directory exists
    wsl bash -c "mkdir -p '$sslDir'"

    # Check if certificates already exist
    $checkCertCmd = "[ -f '$sslCert' ] && [ -f '$sslKey' ] && echo 'yes' || echo 'no'"
    $certExists = wsl bash -c $checkCertCmd

    $protocolMode = "http"

    if ($certExists -eq "yes") {
        Write-Host "Found existing SSL certificates in $sslDir"
        Write-Host "  Certificate: $sslCert"
        Write-Host "  Private Key: $sslKey"
        $protocolMode = "https"
    }
    else {
        Write-Host ""
        Write-Host "No SSL certificates found."
        Write-Host ""
        Write-Host "Options:"
        Write-Host "  1) HTTP only (no encryption) - simplest for isolated LANs"
        Write-Host "  2) HTTPS with self-signed certificate - browsers will show warnings"
        Write-Host "  3) HTTPS with your own certificates - place cert.pem and key.pem in ssl/ dir"
        Write-Host ""

        if ($Force) {
            $sslChoice = "1"
        }
        else {
            $sslChoice = Read-Host "Select option [1]"
            if ([string]::IsNullOrWhiteSpace($sslChoice)) {
                $sslChoice = "1"
            }
        }

        switch ($sslChoice) {
            "2" {
                if (New-SelfSignedCertificate -DeploymentPath $DeploymentPath) {
                    $protocolMode = "https"
                }
                else {
                    Write-WarningMsg "Falling back to HTTP mode"
                    $protocolMode = "http"
                }
            }
            "3" {
                Write-Host ""
                Write-Host "Please place your SSL certificates in the ssl/ directory:"
                Write-Host "  - cert.pem (certificate)"
                Write-Host "  - key.pem (private key)"
                Write-Host ""
                Write-Host "Then run this script again."
                exit 0
            }
            default {
                Write-Host "Using HTTP mode (no encryption)."
                $protocolMode = "http"
            }
        }
    }

    Write-Host ""
    Write-Host "Protocol mode: $protocolMode" -ForegroundColor Cyan

    # Copy appropriate nginx configuration
    $nginxSrc = "$DeploymentPath/client/nginx.$protocolMode.conf"
    $nginxDest = "$DeploymentPath/client/nginx.conf"

    wsl bash -c "cp '$nginxSrc' '$nginxDest'"

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to copy nginx configuration"
        exit 1
    }

    # Update SESSION_COOKIE_SECURE in .env
    $envFile = "$DeploymentPath/.env"
    $cookieSecure = if ($protocolMode -eq "https") { "true" } else { "false" }

    $checkSettingCmd = "grep -q '^SESSION_COOKIE_SECURE=' '$envFile' && echo 'yes' || echo 'no'"
    $hasSettingResult = wsl bash -c $checkSettingCmd

    if ($hasSettingResult -eq "yes") {
        wsl bash -c "sed -i 's/^SESSION_COOKIE_SECURE=.*/SESSION_COOKIE_SECURE=$cookieSecure/' '$envFile'"
    }
    else {
        wsl bash -c "echo 'SESSION_COOKIE_SECURE=$cookieSecure' >> '$envFile'"
    }

    Write-Success "SSL/TLS configuration complete"

    return $protocolMode
}

function New-SelfSignedCertificate {
    param([string]$DeploymentPath)

    Write-Host "Generating self-signed SSL certificate..."

    $sslDir = "$DeploymentPath/ssl"
    $sslCert = "$sslDir/cert.pem"
    $sslKey = "$sslDir/key.pem"

    wsl bash -c "mkdir -p '$sslDir'"

    # Collect all addresses for certificate
    $localIps = wsl bash -c "hostname -I 2>/dev/null | tr ' ' '\n' | grep -v '^127\.' | tr '\n' ' '"
    $hostname = wsl bash -c "hostname"

    $sanEntries = "DNS:localhost,IP:127.0.0.1,IP:::1"

    if (-not [string]::IsNullOrWhiteSpace($hostname)) {
        $hostnameClean = $hostname.Trim()
        $sanEntries += ",DNS:$hostnameClean,DNS:${hostnameClean}.local"
    }

    if (-not [string]::IsNullOrWhiteSpace($localIps)) {
        foreach ($ip in ($localIps -split '\s+')) {
            if (-not [string]::IsNullOrWhiteSpace($ip)) {
                $sanEntries += ",IP:$ip"
            }
        }
    }

    Write-Host "Certificate will be valid for: $sanEntries"

    $opensslCmd = @"
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout '$sslKey' \
    -out '$sslCert' \
    -subj '/C=US/ST=Local/L=Local/O=ProtoFleet/CN=localhost' \
    -addext 'subjectAltName=$sanEntries' 2>&1
"@

    $result = wsl bash -c $opensslCmd

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to generate SSL certificate"
        Write-Host $result
        return $false
    }

    # Set permissions
    wsl bash -c "chmod 600 '$sslKey'"
    wsl bash -c "chmod 644 '$sslCert'"

    Write-Success "Self-signed certificate generated successfully"
    Write-Host ""
    Write-Host "NOTE: Browsers will show a security warning for self-signed certificates."
    Write-Host "      You can accept the warning to proceed, or import the certificate"
    Write-Host "      into your browser/OS trust store."

    return $true
}

function Invoke-MySQLVolumePrompt {
    param([string]$DeploymentPath)

    $baseNameCmd = "basename '$DeploymentPath' | sed 's|/$DEPLOYMENT_DIR`$||'"
    $projectName = wsl bash -c $baseNameCmd
    $volumeCmd = "docker volume ls -q | grep -E '^${projectName}[-_]mysql`$' || true"
    $volumeName = wsl bash -c $volumeCmd

    if (-not [string]::IsNullOrWhiteSpace($volumeName)) {
        Write-Host ""
        Write-WarningMsg "Detected existing MySQL data volume: $volumeName"
        Write-Host ""

        if ($Force) {
            $remove = "n"
        }
        else {
            $remove = Read-Host "Remove & reinitialize this volume now? ALL DATA WILL BE LOST (y/N)"
        }

        if ($remove -match "^[Yy]$") {
            Write-Host "Shutting down containers..."
            wsl bash -c "cd '$DeploymentPath' && docker compose -f docker-compose.yaml down"

            Write-Host "Removing volume $volumeName..."
            wsl bash -c "docker volume rm '$volumeName'"

            Write-Success "Volume removed; new credentials will apply on next startup"
        }
        else {
            Write-WarningMsg "Keeping existing MySQL data. New credentials will NOT be applied."
            Write-Host "If you want to use new credentials, run this script again and choose to remove the volume."
        }
    }
}

function Start-DockerCompose {
    param([string]$DeploymentPath)

    Write-Step "Deploying Proto Fleet with Docker Compose..."

    # Detect system architecture
    $arch = wsl bash -c "uname -m"
    $targetArch = if ($arch -match "arm64|aarch64") { "arm64" } else { "amd64" }

    Write-Host "Detected architecture: $arch (using TARGETARCH=$targetArch)"

    # Pull images
    Write-Host "Pulling Docker images..."
    wsl bash -c "cd '$DeploymentPath' && docker compose -f docker-compose.yaml pull"

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to pull Docker images"
        exit 1
    }

    # Build images
    Write-Host "Building Docker images (this may take several minutes)..."
    wsl bash -c "cd '$DeploymentPath' && export TARGETARCH='$targetArch' && docker compose -f docker-compose.yaml build --no-cache"

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Docker build failed"
        exit 1
    }

    # Stop any running services
    Write-Host "Stopping any running services..."
    wsl bash -c "cd '$DeploymentPath' && docker compose -f docker-compose.yaml down"

    # Start services
    Write-Host "Starting services..."
    wsl bash -c "cd '$DeploymentPath' && docker compose -f docker-compose.yaml up -d"

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to start services"
        exit 1
    }

    Write-Success "Services started successfully"
}

function Wait-ForHealthyServices {
    param([string]$DeploymentPath)

    Write-Step "Waiting for services to become healthy..."

    $maxWait = 60
    $elapsed = 0

    while ($elapsed -lt $maxWait) {
        Start-Sleep -Seconds 2
        $elapsed += 2

        # Check service status
        $statusCmd = "cd '$DeploymentPath' && docker compose ps --format json 2>/dev/null || echo '[]'"
        $status = wsl bash -c $statusCmd

        if ($status -ne "[]") {
            # Simple check: if containers are running
            $runningCmd = "cd '$DeploymentPath' && docker compose ps --format '{{.State}}' 2>/dev/null | grep -c 'running' || echo '0'"
            $runningCount = wsl bash -c $runningCmd

            if ([int]$runningCount -gt 0) {
                Write-Success "Services are running"
                return $true
            }
        }

        Write-Host "  Waiting... ($elapsed / $maxWait seconds)" -NoNewline
        Write-Host "`r" -NoNewline
    }

    Write-WarningMsg "Services may still be starting up"
    return $false
}

function Test-ExtractedDeployment {
    param([string]$ScriptDir)

    # Check if running from deployment/windows/ in extracted tarball
    $parentDir = Split-Path -Parent (Split-Path -Parent $ScriptDir)
    $dockerCompose = Join-Path $parentDir "docker-compose.yaml"
    $serverDir = Join-Path $parentDir "server"
    $clientDir = Join-Path $parentDir "client"

    if ((Test-Path $dockerCompose) -and
        (Test-Path $serverDir) -and
        (Test-Path $clientDir)) {
        return $parentDir
    }

    return $null
}

function Show-Status {
    param(
        [string]$DeploymentPath,
        [string]$ProtocolMode
    )

    Write-Step "Checking service status..."

    wsl bash -c "cd '$DeploymentPath' && docker compose ps"

    Write-Host ""
    Write-Host "╔════════════════════════════════════════════════════════════════════════╗" -ForegroundColor Green
    Write-Host "║                                                                        ║" -ForegroundColor Green
    Write-Host "║                   Proto Fleet is now running!                          ║" -ForegroundColor Green
    Write-Host "║                                                                        ║" -ForegroundColor Green
    Write-Host "╚════════════════════════════════════════════════════════════════════════╝" -ForegroundColor Green
    Write-Host ""

    $protocol = if ($ProtocolMode -eq "https") { "https" } else { "http" }

    Write-Host "Access URLs:" -ForegroundColor Cyan
    Write-Host "  Local:  ${protocol}://localhost"

    # Get WSL IP addresses
    $localIps = wsl bash -c "hostname -I 2>/dev/null | tr ' ' '\n' | grep -v '^127\.' | head -n 3"
    if (-not [string]::IsNullOrWhiteSpace($localIps)) {
        foreach ($ip in ($localIps -split "`n")) {
            $ipTrimmed = $ip.Trim()
            if (-not [string]::IsNullOrWhiteSpace($ipTrimmed)) {
                Write-Host "  LAN:    ${protocol}://$ipTrimmed"
            }
        }
    }

    Write-Host ""
    Write-Host "Useful commands:" -ForegroundColor Cyan
    Write-Host "  View logs:    wsl bash -c `"cd $DeploymentPath && docker compose logs -f`""
    Write-Host "  Stop:         wsl bash -c `"cd $DeploymentPath && docker compose down`""
    Write-Host "  Restart:      wsl bash -c `"cd $DeploymentPath && docker compose restart`""
    Write-Host "  Check status: wsl bash -c `"cd $DeploymentPath && docker compose ps`""
    Write-Host ""
}

# ============================================================================
# Main Script
# ============================================================================

Write-Host @"

╔════════════════════════════════════════════════════════════════════════╗
║                                                                        ║
║                   Proto Fleet Installation for Windows                ║
║                                                                        ║
╚════════════════════════════════════════════════════════════════════════╝

"@ -ForegroundColor Cyan

# Check prerequisites
Write-Step "Checking prerequisites..."

if (-not (Test-WSLInstalled)) {
    Write-ErrorMsg "WSL is not installed or not running."
    Write-Host ""
    Write-Host "Please run setup-wsl-docker.ps1 first to set up WSL2 and Docker."
    exit 1
}
Write-Success "WSL is installed"

if (-not (Test-DockerInWSL)) {
    Write-ErrorMsg "Docker is not running in WSL."
    Write-Host ""
    Write-Host "Please run setup-wsl-docker.ps1 first to set up Docker Engine."
    exit 1
}
Write-Success "Docker is running in WSL"

# Handle tarball acquisition (local, detected, or download)
$skipExtraction = $false

# 1. Check if -TarballPath was explicitly provided (automation case)
if (-not [string]::IsNullOrWhiteSpace($TarballPath)) {
    # Using local tarball
    Write-Step "Using local tarball: $TarballPath"

    if (-not (Test-Path $TarballPath)) {
        Write-ErrorMsg "Tarball file not found: $TarballPath"
        exit 1
    }

    $tarName = Split-Path -Leaf $TarballPath
    if ($tarName -notmatch '^proto-fleet-.*\.tar\.gz$') {
        Write-ErrorMsg "Invalid tarball name. Expected format: proto-fleet-*.tar.gz"
        exit 1
    }

    # Transfer to WSL
    $wslTempPath = "/tmp/$tarName"
    Copy-ToWSL -WindowsFilePath $TarballPath -WSLTempPath $wslTempPath
    Write-Success "Local tarball copied to WSL"
}
# 2. Check if running from extracted tarball directory
elseif ($detectedDeployment = Test-ExtractedDeployment -ScriptDir $PSScriptRoot) {
    if (-not $Force) {
        Write-Host ""
        Write-Host "Detected Proto Fleet deployment files in: $detectedDeployment"
        Write-Host ""
        $useDetected = Read-Host "Use these files? (Y/n)"
    } else {
        $useDetected = "y"
    }

    if ($useDetected -match "^[Yy]$" -or [string]::IsNullOrWhiteSpace($useDetected)) {
        # Use detected deployment directly (no extraction needed)
        Write-Success "Using deployment files from: $detectedDeployment"
        $deploymentPath = $detectedDeployment
        $skipExtraction = $true
    } else {
        # User declined, prompt for tarball path
        Write-Host ""
        $TarballPath = Read-Host "Enter path to proto-fleet-*.tar.gz tarball"
        if ([string]::IsNullOrWhiteSpace($TarballPath) -or -not (Test-Path $TarballPath)) {
            Write-ErrorMsg "Invalid tarball path"
            exit 1
        }

        $tarName = Split-Path -Leaf $TarballPath
        if ($tarName -notmatch '^proto-fleet-.*\.tar\.gz$') {
            Write-ErrorMsg "Invalid tarball name. Expected format: proto-fleet-*.tar.gz"
            exit 1
        }

        # Transfer to WSL
        $wslTempPath = "/tmp/$tarName"
        Copy-ToWSL -WindowsFilePath $TarballPath -WSLTempPath $wslTempPath
        Write-Success "Local tarball copied to WSL"
    }
}
# 3. Not detected - prompt for tarball path
else {
    Write-Host ""
    Write-Host "No deployment files detected in current directory."
    Write-Host ""
    Write-Host "Please provide the path to your proto-fleet-*.tar.gz tarball."
    Write-Host ""

    if (-not $Force) {
        $TarballPath = Read-Host "Enter tarball path"
    }

    if ([string]::IsNullOrWhiteSpace($TarballPath) -or -not (Test-Path $TarballPath)) {
        Write-ErrorMsg "Invalid or missing tarball path"
        exit 1
    }

    # Validate tarball name
    $tarName = Split-Path -Leaf $TarballPath
    if ($tarName -notmatch '^proto-fleet-.*\.tar\.gz$') {
        Write-ErrorMsg "Invalid tarball name. Expected format: proto-fleet-*.tar.gz"
        exit 1
    }

    # Transfer to WSL
    $wslTempPath = "/tmp/$tarName"
    Copy-ToWSL -WindowsFilePath $TarballPath -WSLTempPath $wslTempPath
    Write-Success "Local tarball copied to WSL"
}

# Handle extraction or convert detected path to WSL path
if ($skipExtraction) {
    # Using detected deployment directory - convert Windows path to WSL path
    $wslDeploymentPath = ConvertTo-WSLPath -WindowsPath $deploymentPath
    $deploymentPath = "$wslDeploymentPath/$DEPLOYMENT_DIR"
} else {
    # Determine installation directory
    $finalInstallDir = Set-InstallDirectory -DefaultDir $InstallDir

    # Extract tarball
    $deploymentPath = Expand-InWSL -TarPath $wslTempPath -TargetDir $finalInstallDir
}

# Validate plugin binaries
Test-PluginBinaries -DeploymentPath $deploymentPath

# Handle configuration
if (-not [string]::IsNullOrWhiteSpace($ConfigFile)) {
    # User provided config file
    Write-Step "Using provided configuration file: $ConfigFile"

    if (-not (Test-Path $ConfigFile)) {
        Write-ErrorMsg "Config file not found: $ConfigFile"
        exit 1
    }

    # Copy config file to WSL
    $wslConfigPath = ConvertTo-WSLPath -WindowsPath $ConfigFile
    $targetEnvFile = "$deploymentPath/.env"

    wsl bash -c "cp '$wslConfigPath' '$targetEnvFile'"
    wsl bash -c "chmod 600 '$targetEnvFile'"

    # Validate it has required keys
    if (-not (Test-EnvFileComplete -EnvFilePath $targetEnvFile)) {
        Write-ErrorMsg "Provided config file is missing required keys"
        exit 1
    }

    Write-Success "Configuration file copied"
}
else {
    # Interactive or existing configuration
    New-EnvironmentFile -DeploymentPath $deploymentPath | Out-Null
}

# Configure SSL/TLS
$protocolMode = Set-SSLConfiguration -DeploymentPath $deploymentPath

# Start Docker Compose
Start-DockerCompose -DeploymentPath $deploymentPath

# Wait for services
Wait-ForHealthyServices -DeploymentPath $deploymentPath | Out-Null

# Show status
Show-Status -DeploymentPath $deploymentPath -ProtocolMode $protocolMode
