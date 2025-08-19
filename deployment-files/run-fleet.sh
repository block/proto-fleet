#!/bin/bash

# ============================================================================
# Proto Fleet Installation and Setup Script
# ============================================================================

PROJECT_ROOT="$(pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
ENV_FILE="$PROJECT_ROOT/.env"

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

# Prompt user to reinitialize MySQL data volume if it exists
prompt_store_reinit() {
  local proj=$(basename "$PROJECT_ROOT")
  local vol=$(docker volume ls -q | grep -E "^${proj}[-_]mysql$")
  if [[ -n $vol ]]; then
    echo "⚠️  Detected existing MySQL data volume: $vol"
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
        "MYSQL_ROOT_PASSWORD"
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

    # Database root password configuration
    echo -n "Generate a random password for the Database root user? (Y/n): "
    read gen_mysql_pass
    if [[ -z "$gen_mysql_pass" || $gen_mysql_pass =~ ^[Yy]$ ]]; then
        MYSQL_ROOT_PASSWORD=$(openssl rand -base64 16)
        echo "Generated secure password for the Database root user."
    else
        echo -n "Enter password for the Database root user: "
        read -s MYSQL_ROOT_PASSWORD
        echo
    fi
    echo "MYSQL_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD" >> "$ENV_FILE"

    # Database user configuration
    echo -n "Enter username for the Database user [fleet_user]: "
    read DB_USERNAME
    DB_USERNAME=${DB_USERNAME:-fleet_user}
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
# Docker Image Preparation
# ----------------------------------------------------------------------------

echo "Pulling latest Docker images..."
docker compose -f "$COMPOSE_FILE" pull

# Detect system architecture and set appropriate build target
if [ "$(uname -m)" == "arm64" ] || [ "$(uname -m)" == "aarch64" ]; then
    export TARGETARCH="arm64"
    echo "Detected ARM64 architecture, setting TARGETARCH=arm64"
else
    export TARGETARCH="amd64"
    echo "Detected x86_64 architecture, setting TARGETARCH=amd64"
fi

# Build Docker images
docker compose -f "$COMPOSE_FILE" build --no-cache || { echo "Error: Build failed. Exiting."; exit 1; }

# ----------------------------------------------------------------------------
# Service Management
# ----------------------------------------------------------------------------

echo "Stopping any running services..."
docker compose -f "$COMPOSE_FILE" down

echo "Starting services..."
docker compose -f "$COMPOSE_FILE" up -d

# ----------------------------------------------------------------------------
# Final Status Check
# ----------------------------------------------------------------------------

# Check if docker compose was successful
if [ $? -eq 0 ]; then
    echo "--------------------------------------------------------------"
    echo "Proto Fleet is now running at: http://localhost:80"
    echo "--------------------------------------------------------------"
else
    echo "Error: Failed to start services. Check docker compose logs for details."
    exit 1
fi

exit 0
