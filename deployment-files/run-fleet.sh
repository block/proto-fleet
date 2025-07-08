#!/bin/bash

PROJECT_ROOT="$(pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
ENV_FILE="$PROJECT_ROOT/.env"

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

if ! command -v docker &> /dev/null; then
    echo "Docker is not installed. Attempting to install Docker..."

    if [ "$(uname)" == "Darwin" ]; then
        echo "Docker is not installed. You can:"
        echo "1. Let this script download and install Docker Desktop automatically"
        echo "2. Install Docker manually yourself"
        read -p "Choose option (1/2): " docker_install_choice

        if [ "$docker_install_choice" == "1" ]; then
            DOCKER_VERSION="4.41.2"
            if [ "$(uname -m)" == "arm64" ]; then
                DOWNLOAD_URL="https://desktop.docker.com/mac/main/arm64/191736/Docker.dmg"
                CHECKSUM="19c69b358a8ee1b94e308648a2853e398f4bff29f0f74f00ef2d1b462ced1d1c"
            else
                DOWNLOAD_URL="https://desktop.docker.com/mac/main/amd64/191736/Docker.dmg"
                CHECKSUM="51a14a53808659f02b48f571dcf0e3cdb03a7e69cc51cc9ecb519bf6b10403df"
            fi

            echo "Detected $(uname -m) architecture"
            echo "Downloading Docker Desktop $DOCKER_VERSION..."
            curl -L -o /tmp/Docker.dmg "$DOWNLOAD_URL"

            # Verify checksum
            ACTUAL_CHECKSUM=$(shasum -a 256 /tmp/Docker.dmg | cut -d ' ' -f 1)
            if [ "$ACTUAL_CHECKSUM" != "$CHECKSUM" ]; then
                echo "Error: Docker download checksum verification failed."
                echo "Expected: $CHECKSUM"
                echo "Actual:   $ACTUAL_CHECKSUM"
                echo "Please install Docker manually from: https://docs.docker.com/desktop/install/mac-install/"
                exit 1
            fi

            echo "Installing Docker Desktop..."
            hdiutil attach /tmp/Docker.dmg
            cp -R "/Volumes/Docker/Docker.app" /Applications
            hdiutil detach "/Volumes/Docker"
            rm /tmp/Docker.dmg

            echo "Docker Desktop has been installed. Please open it manually to complete the setup."
            echo "After Docker is running, please re-run this script."
            exit 0
        else
            echo "Please install Docker manually from: https://docs.docker.com/desktop/install/mac-install/"
            exit 1
        fi
    elif [ "$(uname)" == "Linux" ]; then
        # Linux - use apt or yum based on the distribution
        if command -v apt-get &> /dev/null; then
            echo "Installing Docker using apt..."
            sudo apt-get update
            sudo apt-get install -y docker.io docker-compose
        elif command -v yum &> /dev/null; then
            echo "Installing Docker using yum..."
            sudo yum install -y docker docker-compose
        else
            echo "Could not determine package manager. Please install Docker manually:"
            echo "Visit https://docs.docker.com/engine/install/"
            exit 1
        fi

        # Start Docker service on Linux
        sudo systemctl enable docker
        sudo systemctl start docker

        echo "Docker has been installed. Adding current user to the docker group..."
        sudo usermod -aG docker $USER
        echo "Please log out and log back in to apply group changes, then re-run this script."
        exit 0
    else
        echo "Unsupported operating system. Please install Docker manually:"
        echo "Visit https://docs.docker.com/get-docker/"
        exit 1
    fi
fi

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

if ! command -v docker-compose &> /dev/null; then
    echo "docker-compose is not installed. Attempting to install it..."

    if [ "$(uname)" == "Darwin" ]; then
        # For macOS, docker-compose is included in Docker Desktop
        echo "Docker Desktop should include docker-compose. If it's not working, please reinstall Docker Desktop."
        exit 1
    elif [ "$(uname)" == "Linux" ]; then
        # For Linux
        if command -v apt-get &> /dev/null; then
            sudo apt-get install -y docker-compose
        elif command -v yum &> /dev/null; then
            sudo yum install -y docker-compose
        else
            echo "Could not automatically install docker-compose. Please install it manually."
            exit 1
        fi
    fi
fi

# Set up environment variables
use_existing="no"

if [ -f "$ENV_FILE" ]; then
    echo -n "Existing environment file found. Would you like to use it? (Y/n): "
    read use_existing_creds
    if [[ -z "$use_existing_creds" || $use_existing_creds =~ ^[Yy]$ ]]; then
        use_existing="yes"
        echo "Using existing environment file."
    else
        echo "You'll be prompted to enter new credentials."
        rm -f "$ENV_FILE"
    fi
fi

if [ "$use_existing" == "no" ]; then
    # Initialize empty env file
    > "$ENV_FILE"

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
    echo "AUTH_CLIENT_SECRET_KEY=$AUTH_CLIENT_SECRET_KEY" >> "$ENV_FILE"


    while true; do
        echo -n "Enter Pairing secret key (32-48 characters): "
        read -s PAIRING_SECRET_KEY
        echo

        byte_length=${#PAIRING_SECRET_KEY}
        if [ "$byte_length" -lt 32 ]; then
            echo "Error: Pairing secret key must be at least 32 characters long."
            echo "Current length: $byte_length characters"
        elif [ "$byte_length" -gt 48 ]; then
            echo "Error: Pairing secret key must be at most 48 characters long."
            echo "Current length: $byte_length characters"
        else
            echo "Pairing secret key accepted."
            break
        fi
    done
    echo "PAIRING_SECRET_KEY=$PAIRING_SECRET_KEY" >> "$ENV_FILE"

    # Generate random encryption key
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

if [ ! -f "$COMPOSE_FILE" ]; then
    echo "Error: Docker Compose file not found at $COMPOSE_FILE"
    exit 1
fi

echo "Pulling latest Docker images..."
docker-compose -f "$COMPOSE_FILE" pull

echo "Starting services..."
docker-compose -f "$COMPOSE_FILE" up -d

# Check if docker-compose was successful
if [ $? -eq 0 ]; then
    echo "Services started successfully."

    # Extract the port where fleet-client is exposed
    CLIENT_PORT=$(grep -E '.*"([0-9]+):80"' "$COMPOSE_FILE" | sed -E 's/.*"([0-9]+):80".*/\1/' || echo "80")

    echo "--------------------------------------------------------------"
    echo "Fleet Client is now running at: http://localhost:$CLIENT_PORT"
    echo "--------------------------------------------------------------"
else
    echo "Error: Failed to start services. Check docker-compose logs for details."
    exit 1
fi

exit 0