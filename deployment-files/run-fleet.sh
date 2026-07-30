#!/bin/bash

# ============================================================================
# Proto Fleet Installation and Setup Script
# ============================================================================

PROJECT_ROOT="$(pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
COMPOSE_ALERTS_FILE="$PROJECT_ROOT/docker-compose.alerts.yaml"
COMPOSE_SYSTEM_MONITORING_FILE="$PROJECT_ROOT/docker-compose.system-monitoring.yaml"
COMPOSE_TRACING_FILE="$PROJECT_ROOT/docker-compose.tracing.yaml"
ENV_FILE="$PROJECT_ROOT/.env"

ENABLE_BETA_ALERTS=false
ENABLE_SYSTEM_MONITORING=false
ENABLE_TRACING=false

# How long the post-start steps wait for fleet-api to finish its migrations.
# 300 x 2s = 10 minutes: a first boot on SD-card-class hardware (Raspberry Pi)
# runs the full migration set plus image load, which comfortably exceeds the
# old 2-4 minute caps and previously left grafana_ro unprovisioned. On a warm
# database these polls return on the first attempt, so the high cap only costs
# time when migrations are genuinely stuck.
MIGRATION_WAIT_ATTEMPTS=300

env_has_nonempty_value() {
    grep -Eq "^${1}=.+" "$ENV_FILE" 2>/dev/null
}

usage() {
    cat <<'EOF'
Usage: run-fleet.sh [options]

Options:
  --enable-beta-alerts   Layer in the beta alerts sidecar
                                (Grafana, polling TimescaleDB and running
                                the built-in Alertmanager). Off by
                                default. Can also be enabled by setting
                                ENABLE_BETA_ALERTS=true in the .env file.
  --enable-system-monitoring   Layer in host system monitoring (CPU/RAM/disk
                                alert rules and a slow-query dashboard).
                                Requires --enable-beta-alerts. Off by
                                default. Can also be enabled by setting
                                ENABLE_SYSTEM_MONITORING=true in the .env
                                file.
  --enable-tracing              Layer in request tracing (fleet-api spans
                                forwarded to Datadog APM via an OTel
                                collector sidecar). Requires DD_API_KEY in
                                the .env file. Off by default. Can also be
                                enabled by setting ENABLE_TRACING=true in
                                the .env file.
  -h, --help                    Show this help and exit.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --enable-beta-alerts)
            ENABLE_BETA_ALERTS=true
            shift
            ;;
        --enable-system-monitoring)
            ENABLE_SYSTEM_MONITORING=true
            shift
            ;;
        --enable-tracing)
            ENABLE_TRACING=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            break
            ;;
        *)
            echo "Error: unknown argument: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

# Also honor ENABLE_BETA_ALERTS=true from the .env file.
if grep -Eqi "^ENABLE_BETA_ALERTS=true$" "$ENV_FILE" 2>/dev/null; then
    ENABLE_BETA_ALERTS=true
fi
if grep -Eqi "^ENABLE_SYSTEM_MONITORING=true$" "$ENV_FILE" 2>/dev/null; then
    ENABLE_SYSTEM_MONITORING=true
fi
# [[:space:]]*$ tolerates CRLF line endings from Windows/WSL-edited .env files.
if grep -Eqi "^ENABLE_TRACING=true[[:space:]]*$" "$ENV_FILE" 2>/dev/null; then
    ENABLE_TRACING=true
fi

# System monitoring rides the alerts stack (the in-process metrics writer,
# Grafana rule evaluation, and webhook delivery are all alerts-gated), so it
# cannot run alone.
if [ "$ENABLE_SYSTEM_MONITORING" = "true" ] && [ "$ENABLE_BETA_ALERTS" != "true" ]; then
    echo "Error: --enable-system-monitoring requires the beta alerts stack." >&2
    echo "       Pass --enable-beta-alerts as well, or set ENABLE_BETA_ALERTS=true in $ENV_FILE." >&2
    exit 1
fi

# Validate tracing prerequisites before the overlay is layered: its ${DD_API_KEY:?}
# interpolation would otherwise abort every compose command, even `compose down`.
if [ "$ENABLE_TRACING" = "true" ]; then
    if [ ! -f "$COMPOSE_TRACING_FILE" ]; then
        echo "Error: --enable-tracing was passed but $COMPOSE_TRACING_FILE is missing." >&2
        exit 1
    fi
    # Compose interpolation reads the shell environment and the .env file; accept either.
    if [ -z "${DD_API_KEY:-}" ] && ! env_has_nonempty_value DD_API_KEY; then
        echo "Error: --enable-tracing requires DD_API_KEY (a Datadog API key) in $ENV_FILE." >&2
        exit 1
    fi
fi

refresh_compose_files() {
    COMPOSE_FILES=(-f "$COMPOSE_FILE")
    if [ "$ENABLE_BETA_ALERTS" = "true" ] && [ -f "$COMPOSE_ALERTS_FILE" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_ALERTS_FILE")
    fi
    # After alerts so its grafana mounts shadow the rules tombstone and the
    # dashboards placeholder inside the alerts overlay's provisioning mount.
    if [ "$ENABLE_SYSTEM_MONITORING" = "true" ] && [ -f "$COMPOSE_SYSTEM_MONITORING_FILE" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_SYSTEM_MONITORING_FILE")
    fi
    if [ "$ENABLE_TRACING" = "true" ] && [ -f "$COMPOSE_TRACING_FILE" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_TRACING_FILE")
    fi
}
refresh_compose_files

# Layered compose interpolation: host profile file first, operator .env
# last so any key set in .env overrides the profile. Passing --env-file
# disables compose's automatic ./.env loading, so .env must be passed
# explicitly whenever it exists.
refresh_compose_env_args() {
    COMPOSE_ENV_ARGS=()
    local profile profile_file
    # `|| true` keeps a missing FLEET_PROFILE line from killing set -euo
    # pipefail callers; tail -1 matches compose's last-wins env semantics.
    # Normalize whitespace/CR/quotes and case: compose accepts .env syntax
    # (CRLF edits on WSL, quoted values) that the filename match would reject
    profile=$(grep -E '^FLEET_PROFILE=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '[:space:]"'"'" | tr '[:upper:]' '[:lower:]' || true)
    if [ -n "$profile" ]; then
        profile_file="$PROJECT_ROOT/profiles/${profile}.env"
        if [[ "$profile" =~ ^[a-z]+$ ]] && [ -f "$profile_file" ]; then
            COMPOSE_ENV_ARGS+=(--env-file "$profile_file")
        else
            echo "Warning: FLEET_PROFILE='$profile' does not match a profile in $PROJECT_ROOT/profiles/; using default configuration." >&2
        fi
    fi
    if [ -f "$ENV_FILE" ]; then
        COMPOSE_ENV_ARGS+=(--env-file "$ENV_FILE")
    fi
}
refresh_compose_env_args

compose() {
    docker compose ${COMPOSE_ENV_ARGS[@]+"${COMPOSE_ENV_ARGS[@]}"} "${COMPOSE_FILES[@]}" "$@"
}

# Poll psql until the query returns true; caller owns the warning.
wait_for_psql_true() {
    local query="$1" attempt result
    for attempt in $(seq 1 "$MIGRATION_WAIT_ATTEMPTS"); do
        result=$(compose exec -T timescaledb \
            bash -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -tAc \"$query\"" \
            2>/dev/null | tr -d '[:space:]')
        if [ "$result" = "t" ]; then
            return 0
        fi
        sleep 2
    done
    return 1
}
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

    # Check if current user is in the docker group.
    # Skip when running as root: root accesses /var/run/docker.sock directly
    # via socket-file permissions and does not need docker-group membership.
    # Without this skip, `sudo bash install.sh ...` (the recommended remediation
    # for the sudo-mismatch detection in install.sh) would exit here telling
    # the user to log out and back in, leaving the upgrade half-applied.
    if [ "$(id -u)" -ne 0 ] && ! groups $USER | grep -q '\bdocker\b'; then
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

# The post-start readiness check below uses both `--wait` and `--wait-timeout`
# (Compose v2.17.0+). Fail fast here, before `docker compose down` takes an
# existing stack offline.
compose_up_help=$(docker compose up --help 2>&1 || true)
for flag in --wait --wait-timeout; do
    if ! grep -qE -- "(^|[[:space:]])${flag}([[:space:]]|$)" <<<"$compose_up_help"; then
        echo "Error: your docker compose does not support ${flag}."
        echo "run-fleet.sh requires Compose v2.17.0+. Upgrade: https://docs.docker.com/compose/install/"
        exit 1
    fi
done

scrub_env_key() {
    local key="$1"
    local tmp
    if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
        tmp=$(mktemp)
        grep -v "^${key}=" "$ENV_FILE" > "$tmp" || true
        # Overwrite in place to preserve the 0600 perms set elsewhere.
        cat "$tmp" > "$ENV_FILE"
        rm -f "$tmp"
    fi
}

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
      compose down --remove-orphans
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

prompt_fleet_profile() {
    local choice
    echo ""
    echo "Select a host profile (tunes the database and poller for this hardware):"
    echo "  1) standard - Raspberry Pi 5 class host, 16GB RAM with SSD; up to ~5000 miners (default)"
    echo "  2) mini     - low-power or SD-card host, <=4GB RAM; up to ~200 miners"
    echo "  3) max      - dedicated server, 32GB+ RAM, 8+ cores, NVMe; 5000+ miners"
    echo -n "Profile [1]: "
    read -r profile_choice
    profile_choice=$(printf '%s' "$profile_choice" | tr '[:upper:]' '[:lower:]')
    case "$profile_choice" in
        2|mini) choice="mini" ;;
        3|max) choice="max" ;;
        *) choice="standard" ;;
    esac
    # A hand-edited .env may lack a trailing newline; a bare append would
    # glue FLEET_PROFILE onto the last line and corrupt that key
    if [ -s "$ENV_FILE" ] && [ -n "$(tail -c1 "$ENV_FILE")" ]; then
        echo >> "$ENV_FILE"
    fi
    echo "FLEET_PROFILE=$choice" >> "$ENV_FILE"
    echo "Host profile '$choice' saved to $ENV_FILE (edit FLEET_PROFILE there to change it)."
}

# curl | bash installs reach prompts with stdin at EOF; never let an
# unanswered prompt persist a profile
maybe_prompt_fleet_profile() {
    if [ -t 0 ]; then
        prompt_fleet_profile
    else
        echo "Hint: host profiles are available; set FLEET_PROFILE=standard|mini|max in $ENV_FILE and re-run to tune for this hardware."
    fi
}

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
            # Pre-profile installs upgrading: ask once
            if ! grep -q "^FLEET_PROFILE=" "$ENV_FILE"; then
                maybe_prompt_fleet_profile
            fi
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
    # Create with 0600 from birth; secrets land in this file before the
    # final chmod, and umask perms would expose them in the interim
    rm -f "$ENV_FILE"
    (umask 077; : > "$ENV_FILE")

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

    maybe_prompt_fleet_profile

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

if [ "$ENABLE_BETA_ALERTS" = "true" ]; then
    if [ ! -f "$COMPOSE_ALERTS_FILE" ]; then
        echo "Error: --enable-beta-alerts was passed but $COMPOSE_ALERTS_FILE is missing."
        exit 1
    fi

    # The Grafana sidecar runs the alerting engine + UI; give it a
    # rotated admin password the first time we boot the stack so the
    # default "admin / admin" never ships.
    if ! env_has_nonempty_value GRAFANA_ADMIN_PASSWORD; then
        GRAFANA_ADMIN_PASSWORD=$(openssl rand -base64 24)
        echo "GRAFANA_ADMIN_PASSWORD=$GRAFANA_ADMIN_PASSWORD" >> "$ENV_FILE"
        echo "Generated Grafana admin password (stored in $ENV_FILE)."
    fi

    if ! env_has_nonempty_value GRAFANA_DB_USERNAME; then
        scrub_env_key GRAFANA_DB_USERNAME
        echo "GRAFANA_DB_USERNAME=grafana_ro" >> "$ENV_FILE"
    fi
    if ! env_has_nonempty_value GRAFANA_DB_PASSWORD; then
        scrub_env_key GRAFANA_DB_PASSWORD
        GRAFANA_DB_PASSWORD=$(openssl rand -base64 24)
        echo "GRAFANA_DB_PASSWORD=$GRAFANA_DB_PASSWORD" >> "$ENV_FILE"
        echo "Generated Grafana DB password (stored in $ENV_FILE)."
    fi

    # Shared secret the alertmanager webhook receiver requires on every
    # delivery. Mounted on the same listener as the public Connect-RPC
    # services, so without this token an unauthenticated caller on the
    # api-proxy network could forge system alert activity rows.
    if ! env_has_nonempty_value FLEET_ALERTS_WEBHOOK_TOKEN; then
        FLEET_ALERTS_WEBHOOK_TOKEN=$(openssl rand -base64 32)
        echo "FLEET_ALERTS_WEBHOOK_TOKEN=$FLEET_ALERTS_WEBHOOK_TOKEN" >> "$ENV_FILE"
        echo "Generated alertmanager webhook token (stored in $ENV_FILE)."
    fi

    # Grafana's secret_key encrypts secure settings persisted to the
    # grafana-data volume (datasource credentials, encrypted secrets).
    if ! env_has_nonempty_value GRAFANA_SECRET_KEY; then
        GRAFANA_SECRET_KEY=$(openssl rand -base64 32)
        echo "GRAFANA_SECRET_KEY=$GRAFANA_SECRET_KEY" >> "$ENV_FILE"
        echo "Generated Grafana secret key (stored in $ENV_FILE)."
    fi

    # Re-tighten in case the env file was carried over from an older
    # deployment with permissive (e.g. 0644) permissions.
    chmod 600 "$ENV_FILE"

    echo "Alerts stack: enabled (Grafana sidecar over TimescaleDB)"
else
    echo "Alerts stack: disabled (pass --enable-beta-alerts to turn on the beta alerts sidecars)"
fi

if [ "$ENABLE_SYSTEM_MONITORING" = "true" ]; then
    if [ ! -f "$COMPOSE_SYSTEM_MONITORING_FILE" ]; then
        echo "Error: --enable-system-monitoring was passed but $COMPOSE_SYSTEM_MONITORING_FILE is missing."
        exit 1
    fi
    echo "System monitoring: enabled (host CPU/RAM/disk alerts + slow-query dashboard)"
else
    echo "System monitoring: disabled (pass --enable-system-monitoring alongside --enable-beta-alerts to turn it on)"
fi

if [ "$ENABLE_TRACING" = "true" ]; then
    # Re-check after env setup: regenerating .env above drops keys the pre-layering check accepted.
    if [ -z "${DD_API_KEY:-}" ] && ! env_has_nonempty_value DD_API_KEY; then
        echo "Error: $ENV_FILE was regenerated without DD_API_KEY; add it and re-run with --enable-tracing." >&2
        exit 1
    fi
    echo "Tracing: enabled (fleet-api request spans forwarded to Datadog APM)"
else
    echo "Tracing: disabled (pass --enable-tracing with DD_API_KEY in .env to forward request spans to Datadog)"
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

chmod 600 "$ENV_FILE"

# Pick up FLEET_PROFILE written during env setup
refresh_compose_env_args

# ----------------------------------------------------------------------------
# Docker Image Preparation
# ----------------------------------------------------------------------------

echo "Pulling latest Docker images..."
compose pull

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
compose build --no-cache || { echo "Error: Build failed. Exiting."; exit 1; }

# ----------------------------------------------------------------------------
# Service Management
# ----------------------------------------------------------------------------

echo "Stopping any running services..."
compose down --remove-orphans

echo "Starting services..."
# --wait blocks until every service is running (or healthy, when a healthcheck is defined).
# Without it, `up -d` can exit 0 while containers stay in Created (e.g. port conflicts under
# host networking), producing a false "Proto Fleet is now running!" banner.
if ! compose up --remove-orphans -d --wait --wait-timeout 300; then
    echo "Error: services failed to reach running state."
    echo "Check logs with: docker compose ${COMPOSE_ENV_ARGS[*]} ${COMPOSE_FILES[*]} logs"
    exit 1
fi

# ----------------------------------------------------------------------------
# Database Post-Start Tuning
# ----------------------------------------------------------------------------

# Needs a running, migrated database: pg_stat_statements for query
# diagnostics, and staggered Timescale policy job starts so compression and
# rollup refreshes don't all wake at once on small hosts. Idempotent;
# re-applied on every run so jobs added by new migrations get staggered on
# the next upgrade.
apply_database_tuning() {
    # fleetd binds its HTTP listener only after applying every pending
    # migration (cmd/fleetd/main.go), so a responding API is the reliable
    # all-migrations-applied signal. schema_migrations.dirty=false is not:
    # during an upgrade the previous deploy's row already reads clean while
    # migrations (and the policy jobs they create) are still pending.
    local api_addr api_port attempt
    api_addr=$(grep -E '^HTTP_LISTEN_ADDRESS=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- || true)
    api_port="${api_addr##*:}"
    case "$api_port" in *[!0-9]*|"") api_port=4000 ;; esac

    echo "Waiting for fleet-api to finish database migrations before applying tuning…"
    for attempt in $(seq 1 "$MIGRATION_WAIT_ATTEMPTS"); do
        if curl -s -o /dev/null --max-time 2 "http://127.0.0.1:${api_port}/"; then
            break
        fi
        if [ "$attempt" -eq "$MIGRATION_WAIT_ATTEMPTS" ]; then
            echo "Warning: fleet-api did not come up within $((MIGRATION_WAIT_ATTEMPTS * 2))s; skipping database tuning." >&2
            return 1
        fi
        sleep 2
    done

    compose exec -T timescaledb \
        bash -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
DO $$
DECLARE
    r RECORD;
    n integer := 0;
BEGIN
    FOR r IN SELECT job_id FROM timescaledb_information.jobs
             WHERE proc_name IN ('policy_compression',
                                 'policy_refresh_continuous_aggregate',
                                 'policy_retention')
             ORDER BY job_id
    LOOP
        -- initial_start (not next_start) re-phases fixed-schedule jobs so
        -- the stagger survives future scheduled runs
        PERFORM public.alter_job(r.job_id, initial_start => now() + (n * interval '45 seconds'));
        n := n + 1;
    END LOOP;
END $$;
SQL
}

if ! apply_database_tuning; then
    echo "Warning: database tuning step failed; the stack is running, but pg_stat_statements and policy staggering are not applied. Re-run this script to retry." >&2
fi

# ----------------------------------------------------------------------------
# Grafana Read-Only DB Role Provisioning
# ----------------------------------------------------------------------------

# Create or rotate the dedicated `grafana_ro` DB role Grafana uses to
# query notification_metric_sample. We do this here (not in a SQL
# migration) so the password never has to be committed to source and
# can be rotated just by editing $ENV_FILE and re-running this script.
provision_grafana_db_role() {
    local grafana_user grafana_pass db_name app_user pw_escaped stats_grant stats_smoke

    grafana_user=$(grep -E '^GRAFANA_DB_USERNAME=' "$ENV_FILE" | head -1 | cut -d= -f2-)
    grafana_pass=$(grep -E '^GRAFANA_DB_PASSWORD=' "$ENV_FILE" | head -1 | cut -d= -f2-)
    db_name=$(grep -E '^DB_NAME=' "$ENV_FILE" | head -1 | cut -d= -f2-)
    db_name="${db_name:-fleet}"
    app_user=$(grep -E '^DB_USERNAME=' "$ENV_FILE" | head -1 | cut -d= -f2-)
    app_user="${app_user:-fleet}"

    if [ -z "$grafana_user" ] || [ -z "$grafana_pass" ]; then
        echo "Error: GRAFANA_DB_USERNAME / GRAFANA_DB_PASSWORD are missing or empty in $ENV_FILE." >&2
        echo "       Delete those entries from $ENV_FILE and re-run this script to regenerate them." >&2
        return 1
    fi

    # We splice these as SQL identifiers, so require them to match the
    # safe identifier shape rather than try to quote arbitrary input.
    if ! [[ "$grafana_user" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        echo "Error: GRAFANA_DB_USERNAME must be a valid SQL identifier (got: $grafana_user)" >&2
        return 1
    fi
    if ! [[ "$db_name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        echo "Error: DB_NAME must be a valid SQL identifier (got: $db_name)" >&2
        return 1
    fi

    if [ "$grafana_user" = "$app_user" ] || [ "$grafana_user" = "postgres" ]; then
        echo "Error: GRAFANA_DB_USERNAME ('$grafana_user') must not match the application DB role ('$app_user') or the postgres superuser." >&2
        echo "       Pick a dedicated read-only role name (e.g. grafana_ro) and re-run." >&2
        return 1
    fi

    # SQL-escape single quotes in the password so the inlined literal
    # parses regardless of what openssl rand produced.
    pw_escaped="${grafana_pass//\'/\'\'}"

    # fleet_slow_statements() is SECURITY DEFINER (migration 000115), so the
    # Grafana role reads this database's normalized statement stats without
    # pg_read_all_stats (which would also expose cluster-wide query text).
    # The reuse path's REVOKE-ALL-ON-ALL-FUNCTIONS wipes the grant each run;
    # it is re-granted here only while the feature is on. The smoke count()
    # executes the function, so it doubles as end-to-end preload verification.
    stats_grant=""
    stats_smoke=""
    if [ "$ENABLE_SYSTEM_MONITORING" = "true" ]; then
        stats_grant="GRANT EXECUTE ON FUNCTION fleet_slow_statements() TO \"${grafana_user}\";"
        stats_smoke="SELECT count(*) FROM fleet_slow_statements();"
    fi

    # `up --wait` only confirms containers are running, not that
    # fleet-api has finished its migration pass. Poll for every object
    # the Grafana alert rules read — the raw hypertable, the
    # fleet_telemetry_poll_heartbeat continuous aggregate, and the
    # fleet_pollable_device_presence / fleet_active_organization views
    # the protofleet-ingest-stalled and proto-fleet-system rules query.
    echo "Waiting for notification_metric_sample, fleet_telemetry_poll_heartbeat, fleet_pollable_device_presence and fleet_active_organization to be available…"
    if ! wait_for_psql_true "SELECT to_regclass('public.notification_metric_sample') IS NOT NULL AND to_regclass('public.fleet_telemetry_poll_heartbeat') IS NOT NULL AND to_regclass('public.fleet_pollable_device_presence') IS NOT NULL AND to_regclass('public.fleet_active_organization') IS NOT NULL"; then
        echo "Warning: notification_metric_sample / fleet_telemetry_poll_heartbeat / fleet_pollable_device_presence / fleet_active_organization did not appear; Grafana role not provisioned (datasource will fail until fleet-api migrations finish)." >&2
        return 1
    fi

    echo "Provisioning Grafana read-only DB role (${grafana_user})…"
    compose exec -T timescaledb \
        bash -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<SQL
-- The DO block below inlines the role password; keep this session out of
-- the slow-statement log (pg_stat_statements.track_utility=off does not
-- cover duration logging)
SET log_min_duration_statement = -1;
-- Create or rotate the Grafana DB role.
DO \$do\$
DECLARE
    target_role         text := '${grafana_user}';
    target_pass         text := '${pw_escaped}';
    target_db           text := '${db_name}';
    marker_comment      text := 'managed by proto-fleet run-fleet.sh (Grafana read-only role)';
    target_oid          oid;
    is_super            boolean;
    is_createdb         boolean;
    is_createrole       boolean;
    is_replication      boolean;
    is_bypassrls        boolean;
    existing_comment    text;
    member_count        integer;
    has_members_count   integer;
    owned_objects_count integer;
BEGIN
    SELECT oid, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls
      INTO target_oid, is_super, is_createdb, is_createrole, is_replication, is_bypassrls
      FROM pg_roles
     WHERE rolname = target_role;

    IF NOT FOUND THEN
        -- New role: create locked down so it has no path to escalate
        EXECUTE format(
            'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT',
            target_role, target_pass);
        EXECUTE format('COMMENT ON ROLE %I IS %L', target_role, marker_comment);
    ELSE
        -- Existing role: only repurpose when our own marker is present.
        existing_comment := shobj_description(target_oid, 'pg_authid');
        IF existing_comment IS DISTINCT FROM marker_comment THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: role exists but was not provisioned by this script (no managed-by marker on pg_authid). Pick a different GRAFANA_DB_USERNAME or drop the existing role first.',
                target_role;
        END IF;

        IF is_super OR is_createdb OR is_createrole OR is_replication OR is_bypassrls THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: existing role has elevated attributes (super/createdb/createrole/replication/bypassrls). Pick a different GRAFANA_DB_USERNAME or drop the existing role first.',
                target_role;
        END IF;

        SELECT count(*) INTO member_count
          FROM pg_auth_members
         WHERE member = target_oid;
        IF member_count > 0 THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: existing role is a member of other roles, which could grant inherited privileges. Pick a different GRAFANA_DB_USERNAME or drop the existing role first.',
                target_role;
        END IF;

        SELECT count(*) INTO has_members_count
          FROM pg_auth_members
         WHERE roleid = target_oid;
        IF has_members_count > 0 THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: other roles/users have been granted membership in this role and would inherit Grafana''s read-only access. Investigate and drop the role before re-running.',
                target_role;
        END IF;

        SELECT count(*) INTO owned_objects_count
          FROM pg_class
         WHERE relowner = target_oid;
        IF owned_objects_count > 0 THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: role owns % database objects, which suggests it is in use for something other than Grafana. Investigate and drop the role before re-running.',
                target_role, owned_objects_count;
        END IF;

        -- Wipe any direct grants accumulated outside of this script.
        EXECUTE format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON DATABASE %I FROM %I', target_db, target_role);

        -- Known-safe: rotate the password.
        EXECUTE format(
            'ALTER ROLE %I WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT',
            target_role, target_pass);
        EXECUTE format('COMMENT ON ROLE %I IS %L', target_role, marker_comment);
    END IF;
END
\$do\$;

GRANT CONNECT ON DATABASE "${db_name}" TO "${grafana_user}";
GRANT USAGE ON SCHEMA public TO "${grafana_user}";
GRANT SELECT ON notification_metric_sample TO "${grafana_user}";
GRANT SELECT ON fleet_telemetry_poll_heartbeat TO "${grafana_user}";
-- Owner-privilege view: grafana_ro reads the boolean without grants on device/device_pairing.
GRANT SELECT ON fleet_pollable_device_presence TO "${grafana_user}";
-- Owner-privilege view: grafana_ro reads live org ids without grants on organization (miner_auth_private_key).
GRANT SELECT ON fleet_active_organization TO "${grafana_user}";
${stats_grant}

-- smoke check
SET ROLE "${grafana_user}";
SELECT 1 FROM notification_metric_sample LIMIT 0;
SELECT 1 FROM fleet_telemetry_poll_heartbeat LIMIT 0;
SELECT 1 FROM fleet_pollable_device_presence LIMIT 0;
SELECT 1 FROM fleet_active_organization LIMIT 0;
${stats_smoke}
RESET ROLE;
SQL
}

provision_grafana_service_account_token() {
    local admin_pass grafana_url sa_name token_name attempt sa_id token create_body

    if env_has_nonempty_value FLEET_ALERTS_GRAFANA_TOKEN; then
        echo "Grafana service-account token already present in $ENV_FILE; leaving it as-is."
        return 0
    fi

    admin_pass=$(grep -E '^GRAFANA_ADMIN_PASSWORD=' "$ENV_FILE" | head -1 | cut -d= -f2-)
    if [ -z "$admin_pass" ]; then
        echo "Error: GRAFANA_ADMIN_PASSWORD missing/empty in $ENV_FILE; cannot mint a Grafana token." >&2
        return 1
    fi

    grafana_url="http://127.0.0.1:3030"
    sa_name="fleet-api"
    token_name="fleet-api-$(date +%Y%m%d%H%M%S)"

    for attempt in $(seq 1 30); do
        if curl -fsS --max-time 5 -u "admin:${admin_pass}" "${grafana_url}/api/user" >/dev/null 2>&1; then
            break
        fi
        if [ "$attempt" -eq 30 ]; then
            echo "Error: Grafana API at ${grafana_url} not reachable with admin credentials; token not provisioned." >&2
            return 1
        fi
        sleep 2
    done

    create_body=$(curl -fsS --max-time 10 -u "admin:${admin_pass}" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"${sa_name}\",\"role\":\"Editor\",\"isDisabled\":false}" \
        "${grafana_url}/api/serviceaccounts" 2>/dev/null || true)
    sa_id=$(printf '%s' "$create_body" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)

    if [ -z "$sa_id" ]; then
        create_body=$(curl -fsS --max-time 10 -u "admin:${admin_pass}" \
            "${grafana_url}/api/serviceaccounts/search?query=${sa_name}" 2>/dev/null || true)
        sa_id=$(printf '%s' "$create_body" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
    fi

    if [ -z "$sa_id" ]; then
        echo "Error: could not create or locate the Grafana '${sa_name}' service account." >&2
        return 1
    fi

    token=$(curl -fsS --max-time 10 -u "admin:${admin_pass}" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"${token_name}\"}" \
        "${grafana_url}/api/serviceaccounts/${sa_id}/tokens" 2>/dev/null \
        | grep -o '"key":"[^"]*"' | head -1 | sed -E 's/.*"key":"([^"]+)".*/\1/')

    if [ -z "$token" ]; then
        echo "Error: failed to mint a token for the Grafana '${sa_name}' service account." >&2
        return 1
    fi

    scrub_env_key FLEET_ALERTS_GRAFANA_TOKEN
    echo "FLEET_ALERTS_GRAFANA_TOKEN=$token" >> "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    echo "Provisioned Grafana service-account token for fleet-api (stored in $ENV_FILE)."

    echo "Restarting fleet-api to pick up the Grafana token…"
    if ! compose up -d --no-deps --force-recreate fleet-api; then
        echo "Error: wrote the Grafana token to $ENV_FILE but failed to restart fleet-api; it is still" >&2
        echo "       running with the pre-token environment and will 401 against Grafana. Restart it with:" >&2
        echo "         docker compose ${COMPOSE_ENV_ARGS[*]} ${COMPOSE_FILES[*]} up -d --no-deps --force-recreate fleet-api" >&2
        return 1
    fi
}

if [ "$ENABLE_BETA_ALERTS" = "true" ]; then
    if ! provision_grafana_db_role; then
        echo "Error: Grafana DB role provisioning failed; Grafana alerting cannot query notification_metric_sample." >&2
        echo "       Fix the underlying cause (DB reachable, migrations complete) and re-run this script." >&2
        exit 1
    fi
    if ! provision_grafana_service_account_token; then
        echo "Warning: Grafana service-account token provisioning failed; fleet-api cannot authenticate to Grafana" >&2
        echo "         so alert channel/rule/silence management will 401 until this succeeds." >&2
        echo "         Re-run this script (Grafana must be reachable on 127.0.0.1:3030) to retry." >&2
    fi
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
