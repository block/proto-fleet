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
Pass "nightly" to install the latest successful nightly prerelease.
You can override by doing, e.g.:
  install.sh v0.1.0-beta-5
  install.sh nightly
  install.sh nightly-20260424-68712dfabc12
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

resolve_latest_nightly_version() {
  local nightly_channel_url nightly_version curl_stderr

  nightly_channel_url="https://raw.githubusercontent.com/block/proto-fleet/nightly-channel/latest.txt"
  echo "🛰  Determining latest nightly version from ${nightly_channel_url}" >&2

  curl_stderr=$(mktemp)
  if ! nightly_version=$(curl -fsSL "${nightly_channel_url}" 2>"${curl_stderr}"); then
    echo "❌ Failed to query the nightly channel pointer." >&2
    echo "   URL: ${nightly_channel_url}" >&2
    echo "   curl error: $(cat "${curl_stderr}")" >&2
    rm -f "${curl_stderr}"
    exit 1
  fi
  rm -f "${curl_stderr}"

  nightly_version=$(printf '%s' "${nightly_version}" | tr -d '[:space:]')
  if [[ ! "${nightly_version}" =~ ^nightly-[0-9]{8}-[0-9a-f]{12}$ ]]; then
    echo "❌ Nightly channel pointer returned an invalid version: ${nightly_version}" >&2
    exit 1
  fi

  echo "${nightly_version}"
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
case "${1:-latest}" in
  latest)
    VERSION=$(resolve_latest_version)
    echo "🔖 Latest version is ${VERSION}"
    ;;
  nightly)
    VERSION=$(resolve_latest_nightly_version)
    echo "🔖 Latest nightly version is ${VERSION}"
    ;;
  *)
    VERSION="$1"
    ;;
esac

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

configure_stratum_v2() {
  local env_file="./.env"
  local toml_template="./sv2/tproxy.toml"

  # If the operator already configured SV2 on a prior install, preserve
  # their answers. .env is extracted on first install and left alone on
  # subsequent upgrades (see extract_and_cd), so a prior STRATUM_V2_*
  # setting is load-bearing.
  if [ -f "$env_file" ] && grep -q '^STRATUM_V2_PROXY_ENABLED=' "$env_file"; then
    echo "🛰  Keeping existing Stratum V2 configuration in ${env_file}"
    return 0
  fi

  echo ""
  echo "🛰  Stratum V2 translator proxy (optional)"
  echo "   Enables SV1-only miners to mine SV2 pools through a bundled"
  echo "   translator. Fleet rewrites SV2 pool URLs to the proxy's"
  echo "   LAN-facing URL at pool-assignment time."
  echo "   Native SV2 miners do NOT need this — they mine SV2 pools directly."
  read -p "   Enable Stratum V2 translation proxy? [y/N]: " enable_sv2

  if [[ ! "$enable_sv2" =~ ^[Yy]$ ]]; then
    cat >> "$env_file" <<EOF
# Stratum V2 translator proxy (disabled)
STRATUM_V2_PROXY_ENABLED=false
EOF
    echo "   Stratum V2 proxy disabled. Re-run installer or edit .env to enable later."
    return 0
  fi

  local sv2_upstream=""
  while [ -z "$sv2_upstream" ]; do
    read -p "   Upstream SV2 pool URL (stratum2+tcp://host:port): " sv2_upstream
  done

  local sv2_miner_url=""
  while [ -z "$sv2_miner_url" ]; do
    read -p "   Miner-facing proxy URL (stratum+tcp://host:port, default port 34255): " sv2_miner_url
  done

  local sv2_pool_noise_key=""
  read -p "   Pool's Noise authority pubkey (from pool operator docs, required): " sv2_pool_noise_key

  cat >> "$env_file" <<EOF
# Stratum V2 translator proxy
# Enables SV1-only miners to mine SV2 pools; change requires compose restart.
STRATUM_V2_PROXY_ENABLED=true
STRATUM_V2_PROXY_UPSTREAM_URL=${sv2_upstream}
STRATUM_V2_PROXY_MINER_URL=${sv2_miner_url}
# Fleet probes the proxy over TCP; default works under Compose's host network.
STRATUM_V2_PROXY_HEALTH_ADDR=127.0.0.1:34255
STRATUM_V2_PROXY_HEALTH_INTERVAL=30s
EOF

  # Render the tProxy TOML from operator input. Host/port come out of the
  # stratum2+(tcp|ssl)://host:port[/pubkey] URL via bash's regex so a
  # malformed URL fails the match outright rather than silently passing
  # the original string into the TOML.
  if [ -f "$toml_template" ] && [ -n "$sv2_pool_noise_key" ]; then
    if [[ "$sv2_upstream" =~ ^stratum2\+(tcp|ssl)://([^:/]+):([0-9]+)(/.*)?$ ]]; then
      local upstream_host="${BASH_REMATCH[2]}"
      local upstream_port="${BASH_REMATCH[3]}"
      sed -i.bak \
        -e "s|^upstream_address = .*|upstream_address = \"${upstream_host}\"|" \
        -e "s|^upstream_port = .*|upstream_port = ${upstream_port}|" \
        -e "s|^upstream_authority_pubkey = .*|upstream_authority_pubkey = \"${sv2_pool_noise_key}\"|" \
        "$toml_template"
      rm -f "${toml_template}.bak"
      echo "   Rendered ${toml_template} upstream → ${upstream_host}:${upstream_port}"
    else
      echo "   ⚠️  Upstream URL '${sv2_upstream}' does not match stratum2+(tcp|ssl)://host:port; edit ${toml_template} manually before starting the proxy."
    fi

    # The downstream listener (what SV1 miners actually connect to) must
    # match the miner-facing URL we wrote to .env: an operator who chose a
    # custom port in STRATUM_V2_PROXY_MINER_URL would otherwise be silently
    # routed to a port the proxy isn't listening on. Render it from the
    # same regex; default to listening on all interfaces inside the
    # container so containerised miners and host miners both reach it.
    if [[ "$sv2_miner_url" =~ ^stratum\+tcp://([^:/]+):([0-9]+).*$ ]]; then
      local downstream_port="${BASH_REMATCH[2]}"
      sed -i.bak \
        -e "s|^downstream_port = .*|downstream_port = ${downstream_port}|" \
        "$toml_template"
      rm -f "${toml_template}.bak"
      echo "   Rendered ${toml_template} downstream → 0.0.0.0:${downstream_port}"
      # Also align the Fleet-side health probe address with the chosen
      # port so the health gauge isn't pinned to a stale 34255 default.
      if grep -q '^STRATUM_V2_PROXY_HEALTH_ADDR=127\.0\.0\.1:' "$env_file"; then
        sed -i.bak \
          -e "s|^STRATUM_V2_PROXY_HEALTH_ADDR=127\.0\.0\.1:.*|STRATUM_V2_PROXY_HEALTH_ADDR=127.0.0.1:${downstream_port}|" \
          "$env_file"
        rm -f "${env_file}.bak"
      fi
      # Compose substitutes ${STRATUM_V2_PROXY_DOWNSTREAM_PORT} into the
      # sv2-tproxy `ports` mapping so the host-published port matches the
      # in-container listener. Without this the installer would render a
      # custom port into tproxy.toml but Docker would still publish 34255,
      # leaving miners trying to reach a port the host isn't forwarding.
      cat >> "$env_file" <<EOF
STRATUM_V2_PROXY_DOWNSTREAM_PORT=${downstream_port}
EOF
    elif [ -n "$sv2_miner_url" ]; then
      echo "   ⚠️  Miner URL '${sv2_miner_url}' does not match stratum+tcp://host:port; downstream_port left at the template default. Plain TCP only in v1; edit ${toml_template} if you chose a non-default port."
    fi
  fi

  echo "   To start the proxy: docker compose --profile sv2 up -d"
}

configure_stratum_v2

echo "🔧 Running deployment script..."
./run-fleet.sh
