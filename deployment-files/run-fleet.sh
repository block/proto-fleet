#!/bin/bash

# ============================================================================
# Proto Fleet Installation and Setup Script
# ============================================================================

PROJECT_ROOT="$(pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
ENV_FILE="$PROJECT_ROOT/.env"
SSL_DIR="$PROJECT_ROOT/ssl"
SSL_CERT="$SSL_DIR/cert.pem"
SSL_KEY="$SSL_DIR/key.pem"
NGINX_CONF_DIR="$PROJECT_ROOT/client"

# Protocol mode: "https" or "http"
PROTOCOL_MODE="http"

# ----------------------------------------------------------------------------
# Helper Functions
# ----------------------------------------------------------------------------

# Validate if a string is valid Base64 and decodes to 32 bytes
validate_base64_key() {
    local input="$1"

    # Try to decode the Base64 input to a temporary file
    local temp_file=$(mktemp)
    if ! echo "$input" | base64 -d > "$temp_file" 2>/dev/null; then
        rm -f "$temp_file"
        return 1  # Not valid Base64
    fi

    # Check the byte length of the decoded data
    local byte_length=$(wc -c < "$temp_file")
    rm -f "$temp_file"

    if [ "$byte_length" -ne 32 ]; then
        return 2  # Not 32 bytes
    fi

    return 0  # Valid
}

# Get local network IP addresses (excludes loopback, includes IPv4 and global IPv6)
get_local_ips() {
    if [ "$(uname)" == "Darwin" ]; then
        # macOS - get IPv4 and global IPv6 from all active interfaces, exclude loopback
        ifconfig | grep "inet " | grep -v "127\." | awk '{print $2}' | tr '\n' ' '
        ifconfig | grep "inet6 " | grep -vE "fe[89ab][0-9a-f]:" | grep -v "::1" | awk '{print $2}' | tr '\n' ' '
    else
        # Linux - get IPv4 and global IPv6 from all active interfaces, exclude loopback
        hostname -I 2>/dev/null | tr ' ' '\n' | grep -v "^127\." | tr '\n' ' ' || \
        ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | grep -v "^127\." | tr '\n' ' '
        ip -6 addr show scope global 2>/dev/null | grep -oP '(?<=inet6\s)[0-9a-f:]+' | tr '\n' ' '
    fi
}

# Generate self-signed SSL certificate using OpenSSL
generate_self_signed_cert() {
    echo "Generating self-signed SSL certificate..."
    mkdir -p "$SSL_DIR"

    # Collect all addresses for the certificate
    local san_entries="DNS:localhost,IP:127.0.0.1,IP:::1"

    # Add local hostname
    local hostname=$(hostname)
    if [ -n "$hostname" ]; then
        san_entries="$san_entries,DNS:$hostname,DNS:${hostname}.local"
    fi

    # Add all local network IPs
    local local_ips=$(get_local_ips)
    for ip in $local_ips; do
        san_entries="$san_entries,IP:$ip"
    done

    echo "Certificate will be valid for: $san_entries"

    # Generate self-signed certificate valid for 365 days
    local openssl_output
    openssl_output=$(openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$SSL_KEY" \
        -out "$SSL_CERT" \
        -subj "/C=US/ST=Local/L=Local/O=ProtoFleet/CN=localhost" \
        -addext "subjectAltName=$san_entries" 2>&1)
    local openssl_status=$?

    if [ $openssl_status -ne 0 ]; then
        echo "Error: Failed to generate SSL certificate."
        echo "$openssl_output"
        return 1
    fi

    chmod 600 "$SSL_KEY"
    chmod 644 "$SSL_CERT"
    echo "Self-signed certificate generated successfully."
    echo ""
    echo "NOTE: Browsers will show a security warning for self-signed certificates."
    echo "      You can accept the warning to proceed, or import the certificate"
    echo "      into your browser/OS trust store."
}

# Copy appropriate nginx configuration based on protocol mode
copy_nginx_config() {
    local mode="$1"
    local src_conf="$NGINX_CONF_DIR/nginx.${mode}.conf"

    if [ ! -f "$src_conf" ]; then
        echo "Error: nginx config not found: $src_conf"
        return 1
    fi

    if ! cp "$src_conf" "$NGINX_CONF_DIR/nginx.conf"; then
        echo "Error: Failed to copy nginx config"
        return 1
    fi
}

# Detect if running inside WSL
is_wsl() {
    grep -qiE "(microsoft|wsl)" /proc/version 2>/dev/null
}

# Check and fix WSL networking issues (IPv6/DNS problems)
fix_wsl_networking() {
    echo "Detected WSL environment. Checking network connectivity..."

    # Test if we can reach Docker registry
    if ! curl -s --max-time 5 https://registry-1.docker.io/v2/ >/dev/null 2>&1; then
        echo "Network issue detected. Applying WSL networking fixes..."

        # Fix 1: Configure system to prefer IPv4 over IPv6
        echo "  - Configuring IPv4 preference..."
        if ! grep -qF "precedence ::ffff:0:0/96 100" /etc/gai.conf 2>/dev/null; then
            sudo bash -c 'echo "precedence ::ffff:0:0/96 100" >> /etc/gai.conf'
        fi
        # Fix 2: Disable IPv6 routing at kernel level (WSL-specific workaround for connectivity issues).
        # When IPv6 is disabled, the discovery pipeline gracefully falls back to IPv4-only operation.
        echo "  - Disabling IPv6 routing..."
        sudo sysctl -w net.ipv6.conf.all.disable_ipv6=1 >/dev/null 2>&1
        sudo sysctl -w net.ipv6.conf.default.disable_ipv6=1 >/dev/null 2>&1

        # Make IPv6 disabling persistent across reboots
        for setting in "net.ipv6.conf.all.disable_ipv6=1" "net.ipv6.conf.default.disable_ipv6=1"; do
            grep -q "^$setting" /etc/sysctl.conf 2>/dev/null || sudo bash -c "echo '$setting' >> /etc/sysctl.conf"
        done
        # Fix 3: Ensure Google's DNS server is available as a fallback
        echo "  - Configuring DNS..."
        if ! grep -q "nameserver 8.8.8.8" /etc/resolv.conf 2>/dev/null; then
            sudo cp /etc/resolv.conf "/etc/resolv.conf.backup.$(date +%s)" 2>/dev/null || true
            sudo bash -c 'echo "nameserver 8.8.8.8" >> /etc/resolv.conf'
        fi

        # Fix 4: Prevent WSL from overwriting resolv.conf on restart
        if grep -q "generateResolvConf *= *false" /etc/wsl.conf 2>/dev/null; then
            : # Already configured correctly
        elif grep -q "generateResolvConf" /etc/wsl.conf 2>/dev/null; then
            # Setting exists but is true - change to false
            sudo sed -i 's/generateResolvConf *= *true/generateResolvConf = false/' /etc/wsl.conf
        elif grep -q "^\[network\]" /etc/wsl.conf 2>/dev/null; then
            # [network] section exists - add setting to it
            sudo sed -i '/^\[network\]/a generateResolvConf = false' /etc/wsl.conf
        else
            # No [network] section - append new section
            sudo bash -c 'printf "\n[network]\ngenerateResolvConf = false\n" >> /etc/wsl.conf'
        fi

        echo "Fixes applied. Testing connectivity..."

        max_retries=5
        backoff_seconds=2
        attempt=1
        connectivity_restored=0

        while [ "$attempt" -le "$max_retries" ]; do
            echo "  - Connectivity test attempt $attempt of $max_retries..."
            if curl -s --max-time 10 https://registry-1.docker.io/v2/ >/dev/null 2>&1; then
                connectivity_restored=1
                break
            fi

            if [ "$attempt" -lt "$max_retries" ]; then
                echo "    Connection still failing. Waiting ${backoff_seconds}s before retry..."
                sleep "$backoff_seconds"
                backoff_seconds=$((backoff_seconds * 2))
            fi

            attempt=$((attempt + 1))
        done

        if [ "$connectivity_restored" -ne 1 ]; then
            echo ""
            echo "ERROR: Still cannot reach Docker registry."
            echo "Please try the following:"
            echo "  1. Open PowerShell as Administrator"
            echo "  2. Run: wsl --shutdown"
            echo "  3. Re-open WSL and run this script again"
            echo ""
            exit 1
        fi

        echo "Network connectivity restored!"

        # Clear any corrupted Docker build cache from previous failed attempts
        echo "Clearing Docker build cache from any previous failed attempts..."
        docker builder prune -af >/dev/null 2>&1 || true
    else
        echo "Network connectivity OK."
    fi
}

# ----------------------------------------------------------------------------
# Docker Installation Check and Setup
# ----------------------------------------------------------------------------

if ! command -v docker &> /dev/null; then
    echo "Docker is not installed. Attempting to install Docker..."

    if [ "$(uname)" == "Linux" ]; then
        curl -fsSL https://get.docker.com | sudo sh

        if ! command -v docker &> /dev/null; then
            echo "Error: Docker installation failed. Please install Docker manually:"
            echo "Visit https://docs.docker.com/engine/install/"
            exit 1
        fi

        echo "Docker installed successfully!"
    else
        echo "Please install Docker manually:"
        echo "Visit https://docs.docker.com/get-docker/"
        exit 1
    fi
fi

# Configure Docker for Linux systems
if [ "$(uname)" == "Linux" ]; then
    # Check if Docker is set to start on boot
    if ! systemctl is-enabled docker &>/dev/null; then
        echo "Configuring Docker to start on system boot..."
        sudo systemctl enable docker
    fi

    # Check if current user is in the docker group
    if ! groups $USER | grep -q '\bdocker\b'; then
        echo "Adding current user to the docker group for passwordless Docker usage..."
        sudo usermod -aG docker $USER
        echo "Please log out and log back in to apply group changes, then re-run this script."
        exit 0
    fi
fi

# ----------------------------------------------------------------------------
# Docker Daemon Check and Startup
# ----------------------------------------------------------------------------

if ! docker info > /dev/null 2>&1; then
    echo "Docker daemon is not running. Starting Docker..."

    # For macOS, attempt to start Docker Desktop
    if [ "$(uname)" == "Darwin" ]; then
        open -a Docker

        echo "Waiting for Docker to start..."
        for i in {1..30}; do
            if docker info > /dev/null 2>&1; then
                echo "Docker daemon is now running."
                break
            fi
            sleep 1
            if [ $i -eq 30 ]; then
                echo "Error: Docker failed to start within 30 seconds."
                exit 1
            fi
        done
    else
        # For Linux systems
        echo "Attempting to start Docker service..."
        sudo systemctl start docker

        for i in {1..10}; do
            if docker info > /dev/null 2>&1; then
                echo "Docker daemon is now running."
                break
            fi
            sleep 1
            if [ $i -eq 10 ]; then
                echo "Error: Docker failed to start."
                exit 1
            fi
        done
    fi
else
    echo "Docker daemon is already running."
fi

# ----------------------------------------------------------------------------
# WSL Networking Fix
# ----------------------------------------------------------------------------

# Fix WSL networking issues (IPv6/DNS) if running in WSL
if is_wsl; then
    fix_wsl_networking
fi

# ----------------------------------------------------------------------------
# Docker Compose Installation Check
# ----------------------------------------------------------------------------

if ! docker compose version &> /dev/null; then
    echo "docker compose is not installed. Attempting to install it..."

    if [ "$(uname)" == "Linux" ]; then
        # For Linux
        if command -v apt-get &> /dev/null; then
            sudo apt-get install -y docker-compose-plugin
        elif command -v yum &> /dev/null; then
            sudo yum install -y docker-compose-plugin
        else
            echo "Could not automatically install docker compose. Please install it manually. https://docs.docker.com/compose/install/linux/"
            exit 1
        fi
    else
        echo "Please install docker compose manually. https://docs.docker.com/compose/install/"
        exit 1
    fi
fi

# ----------------------------------------------------------------------------
# Database Volume Management Function
# ----------------------------------------------------------------------------

# Prompt user to reinitialize TimescaleDB data volume if it exists
prompt_store_reinit() {
  local proj=$(basename "$PROJECT_ROOT")
  local vol=$(docker volume ls -q | grep -E "^${proj}[-_]timescaledb-data$")
  if [[ -n $vol ]]; then
    echo "⚠️  Detected existing TimescaleDB data volume: $vol"
    read -p "   Remove & reinitialize this volume now? ALL DATA WILL BE LOST (y/N): " answer
    if [[ $answer =~ ^[Yy]$ ]]; then
      echo "   Shutting down containers…"
      docker compose -f "$COMPOSE_FILE" down
      echo "   Removing volume $vol…"
      docker volume rm "$vol"
      echo "   Volume removed; new credentials will apply next startup."
    else
      return 1
    fi
  fi
  return 0
}

# ----------------------------------------------------------------------------
# Environment File Validation and Setup
# ----------------------------------------------------------------------------

use_existing="no"

# Check if environment file exists and validate its contents
if [ -f "$ENV_FILE" ]; then
    required_keys=(
        "DB_USERNAME"
        "DB_PASSWORD"
        "AUTH_CLIENT_SECRET_KEY"
        "ENCRYPT_SERVICE_MASTER_KEY"
    )

    # Check for missing required keys
    missing_keys=0
    for key in "${required_keys[@]}"; do
        if ! grep -q "^$key=" "$ENV_FILE"; then
            missing_keys=1
            echo "Missing required key in environment file: $key"
        fi
    done

    if [ $missing_keys -eq 0 ]; then
        echo -n "Existing environment file found with all required keys. Use it? (Y/n): "
        read use_existing_creds
        if [[ -z "$use_existing_creds" || $use_existing_creds =~ ^[Yy]$ ]]; then
            use_existing="yes"
            echo "Using existing environment file."
        else
            prompt_store_reinit || { echo "Aborting due to existing data volume."; exit 1; }
        fi
    else
        echo "Existing environment file is incomplete. Regenerating…"
        prompt_store_reinit || { echo "Cannot proceed with incomplete env + existing data."; exit 1; }
    fi
fi

# ----------------------------------------------------------------------------
# Generate New Environment Configuration
# ----------------------------------------------------------------------------

if [ "$use_existing" == "no" ]; then
    # Initialize empty env file
    > "$ENV_FILE"

    # Database user configuration
    echo -n "Enter username for the Database user [fleet]: "
    read DB_USERNAME
    DB_USERNAME=${DB_USERNAME:-fleet}
    echo "DB_USERNAME=$DB_USERNAME" >> "$ENV_FILE"

    echo -n "Generate a random password for the Database user? (Y/n): "
    read gen_db_pass
    if [[ -z "$gen_db_pass" || $gen_db_pass =~ ^[Yy]$ ]]; then
        DB_PASSWORD=$(openssl rand -base64 16)
        echo "Generated secure password for the Database user."
    else
        echo -n "Enter password for the Database user: "
        read -s DB_PASSWORD
        echo
    fi
    echo "DB_PASSWORD=$DB_PASSWORD" >> "$ENV_FILE"

    # Auth client secret key configuration
    echo -n "Generate a random Auth client secret key? (Y/n): "
    read gen_auth_key
    if [[ -z "$gen_auth_key" || $gen_auth_key =~ ^[Yy]$ ]]; then
        AUTH_CLIENT_SECRET_KEY=$(openssl rand -base64 32)
        echo "Generated secure Auth client secret key."
    else
        while true; do
            echo -n "Enter Auth client secret key (minimum 32 characters for security): "
            read -s AUTH_CLIENT_SECRET_KEY
            echo

            byte_length=${#AUTH_CLIENT_SECRET_KEY}
            if [ "$byte_length" -lt 32 ]; then
                echo "Error: Secret key must be at least 32 characters long."
                echo "Current length: $byte_length characters"
            else
                echo "Auth client secret key accepted."
                break
            fi
        done
    fi
    echo "AUTH_CLIENT_SECRET_KEY=$AUTH_CLIENT_SECRET_KEY" >> "$ENV_FILE"

    # Encryption service master key configuration
    echo -n "Generate a random encryption service master key? (Y/n): "
    read gen_key
    if [[ -z "$gen_key" || $gen_key =~ ^[Yy]$ ]]; then
        ENCRYPT_SERVICE_MASTER_KEY=$(openssl rand -base64 32)
        echo "Generated encryption service master key."
    else
        while true; do
            echo -n "Enter Encryption service master key: "
            read -s ENCRYPT_SERVICE_MASTER_KEY
            echo
            if ! validate_base64_key "$ENCRYPT_SERVICE_MASTER_KEY"; then
                echo "Error: The provided key is not valid Base64 or doesn't decode to 32 bytes."
            else
                echo "Encryption service master key accepted."
                break
            fi
        done
    fi
    echo "ENCRYPT_SERVICE_MASTER_KEY=$ENCRYPT_SERVICE_MASTER_KEY" >> "$ENV_FILE"

    # Secure the env file
    chmod 600 "$ENV_FILE"
    echo "Environment variables saved to $ENV_FILE"
fi

# ----------------------------------------------------------------------------
# Docker Compose File Validation
# ----------------------------------------------------------------------------

if [ ! -f "$COMPOSE_FILE" ]; then
    echo "Error: Docker Compose file not found at $COMPOSE_FILE"
    exit 1
fi

# ----------------------------------------------------------------------------
# SSL/TLS Configuration
# ----------------------------------------------------------------------------

echo ""
echo "============================================================================"
echo "SSL/TLS Configuration"
echo "============================================================================"

# Check if user-provided certificates exist
if [ -f "$SSL_CERT" ] && [ -f "$SSL_KEY" ]; then
    echo "Found existing SSL certificates in $SSL_DIR"
    echo "  Certificate: $SSL_CERT"
    echo "  Private Key: $SSL_KEY"
    PROTOCOL_MODE="https"
else
    echo ""
    echo "No SSL certificates found in $SSL_DIR"
    echo ""
    echo "Options:"
    echo "  1) HTTP only (no encryption) - simplest for isolated LANs"
    echo "  2) HTTPS with self-signed certificate - browsers will show warnings"
    echo "  3) HTTPS with your own certificates - place cert.pem and key.pem in $SSL_DIR"
    echo ""
    echo -n "Select option [1]: "
    read ssl_choice
    ssl_choice=${ssl_choice:-1}

    case "$ssl_choice" in
        2)
            if generate_self_signed_cert; then
                PROTOCOL_MODE="https"
            else
                echo "Falling back to HTTP mode."
                PROTOCOL_MODE="http"
            fi
            ;;
        3)
            echo ""
            echo "Please place your SSL certificates in $SSL_DIR:"
            echo "  - $SSL_CERT (certificate)"
            echo "  - $SSL_KEY (private key)"
            echo ""
            echo "Then re-run this script."
            exit 0
            ;;
        *)
            echo "Using HTTP mode (no encryption)."
            PROTOCOL_MODE="http"
            ;;
    esac
fi

echo ""
echo "Protocol mode: $PROTOCOL_MODE"

# Ensure SSL directory exists (required for docker-compose volume mount)
mkdir -p "$SSL_DIR"

# Write appropriate nginx configuration
if ! copy_nginx_config "$PROTOCOL_MODE"; then
    echo "Error: Failed to set up nginx configuration. Exiting."
    exit 1
fi

# Update environment file with cookie security setting
if grep -q "^SESSION_COOKIE_SECURE=" "$ENV_FILE"; then
    # Update existing setting
    if [ "$PROTOCOL_MODE" == "https" ]; then
        sed -i.bak 's/^SESSION_COOKIE_SECURE=.*/SESSION_COOKIE_SECURE=true/' "$ENV_FILE" && rm -f "$ENV_FILE.bak"
    else
        sed -i.bak 's/^SESSION_COOKIE_SECURE=.*/SESSION_COOKIE_SECURE=false/' "$ENV_FILE" && rm -f "$ENV_FILE.bak"
    fi
else
    # Add new setting
    if [ "$PROTOCOL_MODE" == "https" ]; then
        echo "SESSION_COOKIE_SECURE=true" >> "$ENV_FILE"
    else
        echo "SESSION_COOKIE_SECURE=false" >> "$ENV_FILE"
    fi
fi

# ----------------------------------------------------------------------------
# Docker Image Preparation
# ----------------------------------------------------------------------------

echo "Pulling latest Docker images..."
docker compose -f "$COMPOSE_FILE" pull

# Load pre-built TimescaleDB image if available (built in CI for the target architecture)
TSDB_IMAGE="$PROJECT_ROOT/images/timescaledb.tar.gz"
if [ -f "$TSDB_IMAGE" ]; then
    echo "Loading pre-built TimescaleDB image..."
    if gunzip -c "$TSDB_IMAGE" | docker load; then
        echo "TimescaleDB image loaded successfully."
    else
        echo "Error: Failed to load TimescaleDB image from $TSDB_IMAGE"
        exit 1
    fi
else
    echo "Warning: Pre-built TimescaleDB image not found at $TSDB_IMAGE."
    echo "The deployment will fail unless the image 'proto-fleet-timescaledb:latest' already exists locally."
fi

# Build Docker images (fleet-api and fleet-client only; TimescaleDB uses pre-built image)
docker compose -f "$COMPOSE_FILE" build --no-cache || { echo "Error: Build failed. Exiting."; exit 1; }

# ----------------------------------------------------------------------------
# Service Management
# ----------------------------------------------------------------------------

echo "Stopping any running services..."
docker compose -f "$COMPOSE_FILE" down

echo "Starting services..."
# --wait blocks until every service is running (or healthy, when a healthcheck is defined).
# Without it, `up -d` can exit 0 while containers stay in Created (e.g. port conflicts under
# host networking), producing a false "Proto Fleet is now running!" banner.
if ! docker compose -f "$COMPOSE_FILE" up -d --wait --wait-timeout 60; then
    echo "Error: services failed to reach running state."
    echo "Check logs with: docker compose -f \"$COMPOSE_FILE\" logs"
    exit 1
fi

# ----------------------------------------------------------------------------
# Docker Cleanup
# ----------------------------------------------------------------------------

echo "Cleaning up old Docker images and build cache..."
docker image prune -f 2>/dev/null || true
docker builder prune -f 2>/dev/null || true

echo "--------------------------------------------------------------"
echo "Proto Fleet is now running!"
echo ""
echo "Access URLs:"
protocol="http"
[ "$PROTOCOL_MODE" == "https" ] && protocol="https"
echo "  Local:  ${protocol}://localhost"
for ip in $(get_local_ips); do
    echo "  LAN:    ${protocol}://$ip"
done
echo "--------------------------------------------------------------"

exit 0
