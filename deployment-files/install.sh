#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<EOF
Usage: $0 [VERSION]

If you omit VERSION or pass "latest", installs the latest release by picking the first tarball found in the latest folder.
You can override by doing, e.g.:
  $0 v0.1.0-beta-5
EOF
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

BUCKET_URL="https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet"

# determine directory and tarball name
if [[ -n "${1:-}" && "${1:-}" != "latest" ]]; then
  VERSION="$1"
  DIR="$VERSION"
else
  DIR="latest"
  META_URL="${BUCKET_URL}/${DIR}/version.txt"
  echo "🛰  Determining latest version from ${META_URL}"
  VERSION=$(curl -fsSL "${META_URL}" | awk '/^version:/ {print $2}')
  if [[ -z "${VERSION}" ]]; then
    echo "❌ Failed to determine the latest version — version.txt is missing or malformed."
    exit 1
  fi
  echo "🔖 Latest version is ${VERSION}"
fi

TAR_NAME="proto-fleet-${VERSION}.tar.gz"
URL="${BUCKET_URL}/${VERSION}/${TAR_NAME}"

echo "🛰  Fetching proto-fleet ${VERSION} from ${URL}"
if ! curl -fsSL "${URL}" -o "/tmp/${TAR_NAME}"; then
  echo "❌ Failed to download ${TAR_NAME} — does that release exist?"
  usage
fi

echo "📦 Extracting to $(pwd)..."
tar -xzvf "/tmp/${TAR_NAME}" -C .
rm "/tmp/${TAR_NAME}"

cd "deployment"

echo "🔧 Running deployment script..."
./run-fleet.sh
