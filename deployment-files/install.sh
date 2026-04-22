#!/usr/bin/env bash
set -euo pipefail

DEPLOYMENT_DIR="deployment"

# Function to determine installation directory by detecting previous installations
find_previous_install_dir() {
  local container_id=$(docker ps -a --filter "name=${DEPLOYMENT_DIR}-fleet-api" --filter "name=${DEPLOYMENT_DIR}_fleet-api" --format "{{.ID}}" 2>/dev/null | head -n 1 || true)
  
  if [ -z "$container_id" ]; then
    # No container found
    return 1
  fi
  
  # Get the mount point from the container - suppress failures with || true
  local mount_path=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/fleet/start"}}{{.Source}}{{end}}{{end}}' "$container_id" 2>/dev/null || true)
  
  if [ -z "$mount_path" ]; then
    # No mount path found
    return 1
  else
    # Extract install directory from mount path
    local install_dir=$(echo "$mount_path" | sed "s|/${DEPLOYMENT_DIR}.*$||" || true)
    echo "$install_dir"
    return 0
  fi
}

# Function to extract files to the installation directory and cd to it
extract_and_cd() {
  local tar_path="$1"
  local target_dir="$2"
  local env_file="${target_dir}/${DEPLOYMENT_DIR}/server/influx_config/.env"
  
  echo "📦 Extracting to ${target_dir}..."
  
  # Create target directory if it doesn't exist
  mkdir -p "$target_dir"
  
  # Check if we need to preserve existing .env file
  if [ -f "$env_file" ]; then
    echo "📦 Preserving existing $env_file file"
    tar -xzvf "$tar_path" -C "$target_dir" --exclude="${DEPLOYMENT_DIR}/server/influx_config/.env"
  else
    tar -xzvf "$tar_path" -C "$target_dir"
  fi
  
  # Clean up the tarball
  rm "$tar_path"
  
  # Change to the deployment directory
  cd "${target_dir}/${DEPLOYMENT_DIR}"
  echo "📍 Working in $(pwd)"
}

usage() {
  cat <<EOF
Usage: install.sh [VERSION]

If you omit VERSION or pass "latest", installs the latest GitHub release.
You can override by doing, e.g.:
  install.sh v0.1.0-beta-5
EOF
  exit 1
}

resolve_latest_version() {
  local latest_release_url effective_url curl_stderr

  latest_release_url="https://github.com/block/proto-fleet/releases/latest"
  echo "🛰  Determining latest version from ${latest_release_url}" >&2

  curl_stderr=$(mktemp)
  if ! effective_url=$(curl -fsSIL -o /dev/null -w '%{url_effective}' "${latest_release_url}" 2>"${curl_stderr}"); then
    echo "❌ Failed to query GitHub Releases." >&2
    echo "   URL: ${latest_release_url}" >&2
    echo "   curl error: $(cat "${curl_stderr}")" >&2
    rm -f "${curl_stderr}"
    exit 1
  fi
  rm -f "${curl_stderr}"

  if [[ "${effective_url}" =~ /releases/tag/([^/?#]+)/?$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return 0
  fi

  echo "❌ Failed to determine the latest version from GitHub Releases." >&2
  echo "   Resolved URL: ${effective_url}" >&2
  exit 1
}

check_page_size() {
  local page_size=$(getconf PAGE_SIZE)
  local os_type=$(uname -s)
  
  if [ "$os_type" != "Darwin" ] && [ "$page_size" -ne 4096 ]; then
    echo "❌ Error: Your system page size is $page_size bytes, but 4096 bytes (4K) is required."
    echo "This is common on Raspberry Pi devices with 16K pages and can cause issues with installation."
    echo ""
    echo "To fix this issue on Raspberry Pi:"
    echo "1. Run: sudo nano /boot/firmware/config.txt"
    echo "2. Add this line at the top: kernel=kernel8.img"
    echo "3. Save and exit (CTRL+X, then Y, then Enter)"
    echo "4. Reboot: sudo reboot"
    echo "5. Verify with: getconf PAGESIZE (should show 4096)"
    echo "6. Run this installation script again"
    read -p "Do you want to continue anyway? (y/N): " continue_anyway
      
    if [[ ! "$continue_anyway" =~ ^[Yy]$ ]]; then
      echo "Installation aborted."
      exit 1
    fi
      
    echo "Continuing installation with $page_size byte page size..."
  fi
}

# show help for -h/--help
if [[ "${1:-}" =~ ^(-h|--help)$ ]]; then
  usage
fi

check_page_size

GITHUB_RELEASES_URL="https://github.com/block/proto-fleet/releases"

# determine version and tarball name
if [[ -n "${1:-}" && "${1:-}" != "latest" ]]; then
  VERSION="$1"
else
  VERSION=$(resolve_latest_version)
  echo "🔖 Latest version is ${VERSION}"
fi

# Detect architecture
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "❌ Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

TAR_NAME="proto-fleet-${VERSION}-${ARCH}.tar.gz"
URL="${GITHUB_RELEASES_URL}/download/${VERSION}/${TAR_NAME}"

echo "🛰  Fetching proto-fleet ${VERSION} from ${URL}"
if ! curl -fsSL "${URL}" -o "/tmp/${TAR_NAME}"; then
  echo "❌ Failed to download ${TAR_NAME} from GitHub Releases — does that release asset exist?"
  usage
fi

# Function to determine default installation directory based on OS
get_default_install_dir() {
  local os_type=$(uname -s)
  
  if [ "$os_type" = "Darwin" ]; then
    echo "$HOME/Applications/ProtoFleet"
  else
    echo "$HOME/proto-fleet"
  fi
}

echo "🔍 Checking for previous ProtoFleet installations via Docker..."
PREVIOUS_INSTALL_DIR=$(find_previous_install_dir || echo "")
DEFAULT_INSTALL_DIR=$(get_default_install_dir)

if [ -n "$PREVIOUS_INSTALL_DIR" ]; then
  SUGGESTED_DIR="$PREVIOUS_INSTALL_DIR"
  echo "📌 Found previous installation at: ${SUGGESTED_DIR}"
else
  SUGGESTED_DIR="$DEFAULT_INSTALL_DIR"
  echo "📌 No previous installation detected."
  echo "   Suggested installation location: ${SUGGESTED_DIR}"
fi

read -p "   Use this location? (Y/n): " use_suggested
if [[ "$use_suggested" =~ ^[Nn]$ ]]; then
  read -p "   Enter installation directory [${DEFAULT_INSTALL_DIR}]: " custom_dir
  INSTALL_DIR="${custom_dir:-$DEFAULT_INSTALL_DIR}"
else
  INSTALL_DIR="$SUGGESTED_DIR"
fi

echo "📌 Will install to: ${INSTALL_DIR}"

extract_and_cd "/tmp/${TAR_NAME}" "$INSTALL_DIR"

# Validate plugin binaries exist
echo "🔌 Validating plugin binaries..."
PLUGIN_DIR="server"
REQUIRED_PLUGINS=("proto-plugin" "antminer-plugin" "asicrs-plugin")
MISSING_PLUGINS=()

for plugin in "${REQUIRED_PLUGINS[@]}"; do
  if [ ! -f "${PLUGIN_DIR}/${plugin}" ]; then
    MISSING_PLUGINS+=("$plugin")
  fi
done

if [ ${#MISSING_PLUGINS[@]} -ne 0 ]; then
  echo "❌ Error: Missing plugin binaries:"
  printf '   - %s\n' "${MISSING_PLUGINS[@]}"
  echo "The installation package may be incomplete. Please contact support."
  exit 1
fi

# Set executable permissions on validated plugin binaries
for plugin in "${REQUIRED_PLUGINS[@]}"; do
  chmod +x "${PLUGIN_DIR}/${plugin}"
done
echo "✅ Plugin binaries validated"

echo "🔧 Running deployment script..."
./run-fleet.sh
