#!/usr/bin/env bash
set -euo pipefail

# This value will be replaced during the build process
VERSION="__VERSION__"

# Default values
DEFAULT_NUM_MINERS=5
DEFAULT_START_IP=10  # Starting IP offset from subnet
DEFAULT_SUBNET="172.20.0"  # Default subnet for miner IPs
SIM_MINERS_DIR="sim-miners"

# Port configuration
BASE_HTTP_PORT=8000  # HTTP ports will be BASE_HTTP_PORT+i (8001, 8002, etc.)
BASE_API_PORT=9000   # API ports will be BASE_API_PORT+i (9001, 9002, etc.)

# Container resource allocation constants
MEM_LIMIT="64M"
MEM_RESERVATION="32M"
CPU_LIMIT="0.05"

# Arrays to store port mappings
declare -a HTTP_PORTS
declare -a API_PORTS

# Initialize variables used in cleanup
TEMP_DIR=""
COMPOSE_FILE=""
NETWORK_NAME="proto-sim-miners-net"
CLEANUP_DONE=0

log() { 
  local level="$1"
  local message="$2"
  
  if [ "$level" == "ERROR" ]; then
    printf '[%s] %s\n' "$level" "$message" >&2
  else
    printf '[%s] %s\n' "$level" "$message"
  fi
}

check_installed() {
  command -v "$1" >/dev/null 2>&1
}

port_is_available() {
  local port=$1
  ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1
}

find_available_port() {
  local base_port=$1
  local current_port=$base_port
  local max_attempts=1000

  if ! check_installed nc; then
    log WARN "netcat not installed, skipping port availability check"
    echo $base_port
    return
  fi

  # Try incrementing the port number until we find an available one
  for ((i=0; i<max_attempts; i++)); do
    if port_is_available $current_port; then
      echo $current_port
      return
    fi
    current_port=$((current_port + 1))
  done

  log ERROR "Could not find an available port after $max_attempts attempts"
  exit 1
}

show_usage() {
  cat <<EOF
Usage: $0 [OPTIONS]

Spin up N simulator miners with unique IPs on the host network.

Options:
  -n, --num NUM         Number of miners to create (default: $DEFAULT_NUM_MINERS)
  -s, --start-ip NUM    Starting IP offset from SUBNET.X (default: $DEFAULT_START_IP)
  -b, --subnet SUBNET   Subnet to use for miner IPs (default: $DEFAULT_SUBNET)
  -h, --help            Show this help message

Examples:
  $0 -n 10                     # Create 10 miners with IPs on host subnet
  $0 -n 5 -s 100               # Create 5 miners with IPs <subnet>.100-104
  $0 -b 192.168.50 -s 10       # Create miners on custom subnet 192.168.50.10+
EOF
  exit 1
}

parse_arguments() {
  NUM_MINERS=$DEFAULT_NUM_MINERS
  START_IP=$DEFAULT_START_IP
  SUBNET=$DEFAULT_SUBNET

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -n|--num)
        NUM_MINERS="$2"
        shift 2
        ;;
      -s|--start-ip)
        START_IP="$2"
        shift 2
        ;;
      -b|--subnet)
        SUBNET="$2"
        shift 2
        ;;
      -h|--help)
        show_usage
        ;;
      *)
        log ERROR "Unknown option: $1"
        show_usage
        ;;
    esac
  done
}

validate_arguments() {
  # Validate subnet format
  if ! [[ "$SUBNET" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]; then
    log ERROR "Invalid subnet format: $SUBNET (should be in format XXX.XXX.XXX)"
    exit 1
  fi

  if ! [[ "$NUM_MINERS" =~ ^[0-9]+$ ]] || [ "$NUM_MINERS" -lt 1 ]; then
    log ERROR "Number of miners must be a positive integer"
    show_usage
  fi

  if ! [[ "$START_IP" =~ ^[0-9]+$ ]] || [ "$START_IP" -lt 2 ] || [ "$START_IP" -gt 254 ]; then
    log ERROR "Start IP must be between 2 and 254"
    show_usage
  fi
}

show_configuration() {
  log INFO "=============================="
  log INFO "Creating $NUM_MINERS miners"
  log INFO "IP Range: $SUBNET.$START_IP to $SUBNET.$((START_IP + NUM_MINERS - 1))"
  log INFO "Web UI: http://<miner-ip>:80"
  log INFO "API: http://<miner-ip>:2121"
  log INFO "=============================="
}

setup_sudo_access() {
  log INFO "Requesting sudo access (required for network configuration)..."
  sudo -v

  # Keep sudo credentials active throughout script execution
  (while true; do
    sudo -n true
    sleep 60
    kill -0 "$$" || exit
  done) 2>/dev/null &

  log INFO "Sudo access granted and will be maintained"
}

verify_system_requirements() {
  # Check if running on macOS
  if [[ "$(uname)" != "Darwin" ]]; then
    log ERROR "This script only supports macOS. Exiting."
    exit 1
  fi

  # Check if Docker is installed and running
  if ! check_installed docker; then
    log ERROR "Docker is not installed. Please install Docker first."
    exit 1
  fi

  if ! docker info &> /dev/null; then
    log ERROR "Docker daemon is not running. Please start Docker first."
    exit 1
  fi

  # Check if docker-compose is installed
  if ! check_installed docker-compose; then
    log INFO "Installing docker-compose..."
    log WARN "Docker Desktop should include docker-compose. If it's not working, please reinstall Docker Desktop."
    exit 1
  fi

  # Check if netcat is installed (optional but recommended)
  if ! check_installed nc; then
    log WARN "netcat is not installed. Port conflict detection will be skipped."
    log WARN "You can install it with: brew install netcat"
    log INFO "Continuing without port conflict detection..."
  fi
}

setup_loopback_aliases() {
  log INFO "Setting up loopback aliases for miner IPs..."
  
  for ((i=1; i<=NUM_MINERS; i++)); do
    IP_ADDRESS="$SUBNET.$((START_IP + i - 1))"

    # Check if alias already exists
    if ! ifconfig lo0 | grep -q "$IP_ADDRESS"; then
      sudo -n ifconfig lo0 alias $IP_ADDRESS up 2>/dev/null || {
        log ERROR "Could not add IP alias $IP_ADDRESS. This is required for port forwarding."
        log ERROR "Please run 'sudo ifconfig lo0 alias $IP_ADDRESS up' manually."
        exit 1
      }
    fi
  done
}

setup_temp_dir() {
  TEMP_DIR=$(mktemp -d)
  if [[ -z "$TEMP_DIR" || ! -d "$TEMP_DIR" ]]; then
    log ERROR "Failed to create temporary directory"
    TEMP_DIR=""
    exit 1
  fi
  log INFO "Created temporary directory: $TEMP_DIR"
}



cleanup_temp_directories() {
  if [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]]; then
    rm -rf "$TEMP_DIR"
    log INFO "Removed temporary directory: $TEMP_DIR"
  fi
}

cleanup_socat_processes() {
  # Find and kill socat processes bound to our miner subnet
  local socat_pids=$(ps aux | grep "[s]ocat.*TCP-LISTEN.*bind=$SUBNET\." | awk '{print $2}')
  
  if [[ -n "$socat_pids" ]]; then
    log INFO "Found socat processes for subnet $SUBNET.*: $socat_pids"
    echo "$socat_pids" | xargs -r kill 2>/dev/null || true
    
    # Wait a moment and force kill if still running
    sleep 1
    local remaining_pids=$(ps aux | grep "[s]ocat.*TCP-LISTEN.*bind=$SUBNET\." | awk '{print $2}')
    if [[ -n "$remaining_pids" ]]; then
      log INFO "Force killing remaining socat processes: $remaining_pids" 
      echo "$remaining_pids" | xargs -r kill -9 2>/dev/null || true
    fi
    log INFO "Socat processes stopped successfully"
  else
    log INFO "No socat processes found for subnet $SUBNET.*"
  fi
}

cleanup_docker_resources() {
  if ! docker info &>/dev/null; then
    log INFO "Docker not running, skipping Docker cleanup"
    return 0
  fi

  if [[ -n "$COMPOSE_FILE" && -f "$COMPOSE_FILE" ]]; then
    docker-compose -f "$COMPOSE_FILE" down 2>/dev/null || log WARN "Failed to stop Docker containers"
  fi

  log INFO "Removing Docker network..."
  docker network rm "$NETWORK_NAME" 2>/dev/null || log WARN "Could not remove Docker network $NETWORK_NAME"
  
  if [[ -d "proto-os-web" ]]; then
    log INFO "Removing ProtoOS web assets directory..."
    rm -rf "proto-os-web"
  fi
}

cleanup_loopback_aliases() {
  if [[ -z "$NUM_MINERS" || -z "$SUBNET" || -z "$START_IP" ]]; then
    log INFO "Missing configuration for loopback cleanup, skipping"
    return 0
  fi

  log INFO "Removing loopback aliases..."
  local cleanup_errors=0
  
  for ((i=1; i<=NUM_MINERS; i++)); do
    local IP_ADDRESS="$SUBNET.$((START_IP + i - 1))"

    if ifconfig lo0 | grep -q "$IP_ADDRESS"; then
      if ! sudo -n ifconfig lo0 -alias "$IP_ADDRESS" 2>/dev/null; then
        cleanup_errors=$((cleanup_errors + 1))
      fi
    fi
  done

  if [ $cleanup_errors -eq 0 ]; then
    log INFO "All loopback aliases removed successfully"
  else
    log WARN "Failed to remove $cleanup_errors loopback aliases"
  fi
}

cleanup() {
  if [ $CLEANUP_DONE -ne 0 ]; then
    return 0
  fi
  CLEANUP_DONE=1

  log INFO "Starting cleanup process..."

  cleanup_temp_directories
  cleanup_socat_processes  
  cleanup_docker_resources
  cleanup_loopback_aliases

  log INFO "Cleanup complete."
}

setup_socat_forwarding() {
  if ! check_installed socat; then
    log INFO "Installing socat via Homebrew..."
    brew install socat || {
      log ERROR "Failed to install socat. Please install it manually. If you are on macOS, ensure Homebrew is installed and working, then run: brew install socat. On Linux, try: sudo apt-get install socat (Debian/Ubuntu) or sudo yum install socat (CentOS/Fedora). For more help, see: https://www.dest-unreach.org/socat/"
      return 1
    }
  fi

  log INFO "Setting up socat port forwarding using sudo (required for privileged ports)..."
  
  for ((i=1; i<=NUM_MINERS; i++)); do
    local IP_ADDRESS="$SUBNET.$((START_IP + i - 1))"
    local HTTP_PORT=${HTTP_PORTS[$i]}
    local API_PORT=${API_PORTS[$i]}
    
    # Start socat for HTTP port 80 - forward from static IP to localhost mapped port
    sudo -n socat TCP-LISTEN:80,bind=$IP_ADDRESS,fork,reuseaddr TCP:127.0.0.1:$HTTP_PORT,retry=5 &
    
    # Start socat for API port 2121
    sudo -n socat TCP-LISTEN:2121,bind=$IP_ADDRESS,fork,reuseaddr TCP:127.0.0.1:$API_PORT,retry=5 &
  done
  
  log INFO "Port forwarding set up successfully"
  log INFO ""
  for ((i=1; i<=NUM_MINERS; i++)); do
    local IP_ADDRESS="$SUBNET.$((START_IP + i - 1))"
    log INFO "Miner $i is accessible at: $IP_ADDRESS"
  done
}

download_and_load_images() {
  log INFO "Downloading sim-miners package for version $VERSION..."
  TARBALL_URL="https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/$VERSION/proto-fleet-sim-miners.tar.gz"
  if ! curl -fsSL "$TARBALL_URL" -o "$TEMP_DIR/proto-fleet-sim-miners.tar.gz"; then
    log ERROR "Failed to download sim-miners package for version $VERSION"
    exit 1
  fi

  log INFO "Extracting package..."
  tar -xzf "$TEMP_DIR/proto-fleet-sim-miners.tar.gz" -C "."

  cd "$SIM_MINERS_DIR"

  # Load Docker image from tarball
  if [ -f "proto-sim-miner-image.tar" ]; then
    log INFO "Loading Docker image from tarball..."
    docker load < proto-sim-miner-image.tar
    
    IMAGE_TAG=$(docker images --format "{{.Repository}}:{{.Tag}}" | grep "protofleet/sim-miner" | head -1)
    log INFO "Using Docker image: $IMAGE_TAG"
  else
    log ERROR "Docker image tarball not found in the package."
    exit 1
  fi

  # Download ProtoOS web assets
  log INFO "Downloading ProtoOS web assets for version $VERSION..."
  PROTO_OS_URL="https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/$VERSION/proto-os-$VERSION.tar.gz"
  mkdir -p "proto-os-web"
  
  if ! curl -fsSL "$PROTO_OS_URL" -o "$TEMP_DIR/proto-os.tar.gz"; then
    log ERROR "Failed to download ProtoOS assets. Web UI will not be available."
    return
  fi

  log INFO "Extracting ProtoOS web assets..."
  mkdir -p "proto-os-web"
  tar -xzf "$TEMP_DIR/proto-os.tar.gz" -C "proto-os-web"
}

setup_docker_network() {
  NETWORK_NAME="proto-sim-miners-net"
  log INFO "Creating Docker bridge network: $NETWORK_NAME"
  docker network rm $NETWORK_NAME 2>/dev/null || true
  docker network create --subnet $SUBNET.0/24 $NETWORK_NAME || {
    log ERROR "Failed to create network $NETWORK_NAME with subnet $SUBNET.0/24"
    exit 1
  }
}

generate_compose_file() {
  COMPOSE_FILE="docker-compose.yaml"
  echo "services:" > $COMPOSE_FILE

  # Clear the port arrays
  HTTP_PORTS=()
  API_PORTS=()

  log INFO "Finding available ports for miner containers..."
  for ((i=1; i<=NUM_MINERS; i++)); do
    IP_ADDRESS="$SUBNET.$((START_IP + i - 1))"
    MAC_ADDRESS=$(printf '02:00:00:%02x:%02x:%02x' $((RANDOM%256)) $((RANDOM%256)) $i)

    # Find available ports
    HTTP_PORT_BASE=$((BASE_HTTP_PORT + i))
    API_PORT_BASE=$((BASE_API_PORT + i))

    HTTP_PORT=$(find_available_port $HTTP_PORT_BASE)
    API_PORT=$(find_available_port $API_PORT_BASE)

    # Store ports in the arrays
    HTTP_PORTS[$i]=$HTTP_PORT
    API_PORTS[$i]=$API_PORT

    echo "  proto-sim-miner-$i:" >> $COMPOSE_FILE
    echo "    image: $IMAGE_TAG" >> $COMPOSE_FILE
    echo "    platform: linux/arm64" >> $COMPOSE_FILE
    echo "    container_name: proto-sim-miner-$i" >> $COMPOSE_FILE
    echo "    hostname: proto-sim-miner-$i" >> $COMPOSE_FILE
    echo "    privileged: true" >> $COMPOSE_FILE
    echo "    networks:" >> $COMPOSE_FILE
    echo "      $NETWORK_NAME:" >> $COMPOSE_FILE
    echo "        ipv4_address: $IP_ADDRESS" >> $COMPOSE_FILE
    echo "    ports:" >> $COMPOSE_FILE
    echo "      - 127.0.0.1:${HTTP_PORTS[$i]}:8080" >> $COMPOSE_FILE
    echo "      - 127.0.0.1:${API_PORTS[$i]}:2121" >> $COMPOSE_FILE
    echo "    mem_limit: $MEM_LIMIT" >> $COMPOSE_FILE
    echo "    mem_reservation: $MEM_RESERVATION" >> $COMPOSE_FILE
    echo "    cpus: $CPU_LIMIT" >> $COMPOSE_FILE
    echo "    restart: unless-stopped" >> $COMPOSE_FILE
    echo "    environment:" >> $COMPOSE_FILE
    echo "      - PORT=80" >> $COMPOSE_FILE
    echo "    mac_address: $MAC_ADDRESS" >> $COMPOSE_FILE
    echo "    volumes:" >> $COMPOSE_FILE
    echo "      - ./proto-os-web/dist/protoOS:/app/miner-web/dist/protoOS" >> $COMPOSE_FILE
    echo "    cap_add:" >> $COMPOSE_FILE
    echo "      - NET_ADMIN" >> $COMPOSE_FILE
    echo "      - NET_RAW" >> $COMPOSE_FILE
    echo "" >> $COMPOSE_FILE
  done

  echo "networks:" >> $COMPOSE_FILE
  echo "  $NETWORK_NAME:" >> $COMPOSE_FILE
  echo "    external: true" >> $COMPOSE_FILE
}

cleanup_existing_containers() {
  log INFO "Tearing down any existing sim miner containers..."
  docker-compose -f $COMPOSE_FILE down 2>/dev/null || true

  log INFO "Removing any leftover miner containers..."
  for i in $(docker ps -a --format '{{.Names}}' | grep proto-sim-miner); do
    docker rm -f $i 2>/dev/null || true
  done
}

start_miner_containers() {
  log INFO "Starting $NUM_MINERS miners..."
  docker-compose -f $COMPOSE_FILE up -d
}

start_miner_services() {
  log INFO "Starting miner services..."
  for ((i=1; i<=NUM_MINERS; i++)); do
    IP_ADDRESS="$SUBNET.$((START_IP + i - 1))"

    for svc in miner-services mcdd miner-api-server; do
      output=$(docker exec proto-sim-miner-$i systemctl start $svc 2>&1 || true)
      exit_code=$?
      if [[ $exit_code -ne 0 ]]; then
        log WARN "Failed to start $svc for miner $i (exit $exit_code): $output"
      fi
    done
  done
}

print_comma_separated_miner_ips() {
  local ips=""
  for ((i=1; i<=NUM_MINERS; i++)); do
    if [ $i -gt 1 ]; then
      ips="$ips,"
    fi
    ips="$ips$SUBNET.$((START_IP + i - 1))"
  done
  log INFO "Miner IPs: $ips"
}

wait_for_termination() {
  log INFO "Waiting for Ctrl+C to terminate..."
  while true; do
    sleep 1
  done
}

trap cleanup SIGINT SIGTERM EXIT

parse_arguments "$@"
validate_arguments
show_configuration
verify_system_requirements

setup_sudo_access
setup_temp_dir
download_and_load_images
setup_docker_network
generate_compose_file
cleanup_existing_containers
start_miner_containers

# Check if docker-compose was successful
if [ $? -eq 0 ]; then
  setup_loopback_aliases
  start_miner_services
  setup_socat_forwarding

  log INFO "===================================="
  log INFO "Miners are now running."
  log INFO "===================================="
  
  print_comma_separated_miner_ips

  wait_for_termination
else
  log ERROR "Failed to start miners."
  exit 1
fi
