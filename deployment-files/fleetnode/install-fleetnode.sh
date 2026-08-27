#!/usr/bin/env bash
set -euo pipefail

VERSION=""
DOWNLOAD_BASE_URL="${FLEETNODE_DOWNLOAD_BASE_URL:-}"
TEST_MODE="${FLEETNODE_TEST_MODE:-0}"
ROOT_PREFIX="${FLEETNODE_ROOT_PREFIX:-}"

usage() {
  cat <<'EOF'
Usage: install-fleetnode.sh --version VERSION

Install one exact Proto Fleet Node release on Linux. VERSION must be a
release-specific tag such as v1.2.3 or nightly-20260825-0123456789ab.

The installer creates the service but leaves a fresh installation disabled.
After installation, enroll the node and then explicitly enable the service.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      [[ $# -ge 2 && -n "${2:-}" ]] || { echo "--version requires a value" >&2; exit 2; }
      VERSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! "$VERSION" =~ ^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$ ]] || [[ "$VERSION" == "latest" ]]; then
  echo "version must be an explicit release identifier (1-128 letters, numbers, dots, underscores, or hyphens; not latest)" >&2
  exit 2
fi
if [[ "$TEST_MODE" != "1" ]]; then
  if [[ -n "$ROOT_PREFIX" || -n "${FLEETNODE_ARCH:-}" || -n "$DOWNLOAD_BASE_URL" || -n "${FLEETNODE_SYSTEMCTL:-}" ]]; then
    echo "Fleet Node installer overrides are restricted to automated tests" >&2
    exit 1
  fi
fi
if [[ "$(uname -s)" != "Linux" && "$TEST_MODE" != "1" ]]; then
  echo "Fleet Node installation currently supports Linux only" >&2
  exit 1
fi
if [[ "$TEST_MODE" != "1" ]]; then
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "run this installer as root (for example: sudo ./install-fleetnode.sh --version $VERSION)" >&2
    exit 1
  fi
fi

case "${FLEETNODE_ARCH:-$(uname -m)}" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported Linux architecture: ${FLEETNODE_ARCH:-$(uname -m)}" >&2; exit 1 ;;
esac

if [[ "$TEST_MODE" == "1" ]]; then
  [[ -n "$ROOT_PREFIX" && "$ROOT_PREFIX" == /* && "$ROOT_PREFIX" != "/" ]] || {
    echo "test mode requires an absolute, non-root FLEETNODE_ROOT_PREFIX" >&2
    exit 1
  }
fi

PROGRAM_DIR="${ROOT_PREFIX}/opt/fleetnode"
CONFIG_DIR="${ROOT_PREFIX}/etc/fleetnode"
STATE_DIR="${ROOT_PREFIX}/var/lib/fleetnode"
UNIT_PATH="${ROOT_PREFIX}/etc/systemd/system/fleetnode.service"
SYSTEMCTL="${FLEETNODE_SYSTEMCTL:-systemctl}"
ARCHIVE_ROOT="fleetnode-${VERSION}-linux-${ARCH}"
ARCHIVE_NAME="${ARCHIVE_ROOT}.tar.gz"
if [[ -z "$DOWNLOAD_BASE_URL" ]]; then
  DOWNLOAD_BASE_URL="https://github.com/block/proto-fleet/releases/download/${VERSION}"
fi

for command in curl sha256sum tar install "$SYSTEMCTL"; do
  command -v "$command" >/dev/null 2>&1 || { echo "required command not found: $command" >&2; exit 1; }
done
if ! command -v nmap >/dev/null 2>&1; then
  echo "required command not found: nmap; install nmap and ensure it is on PATH" >&2
  exit 1
fi
if [[ "$TEST_MODE" != "1" ]]; then
  for command in getent useradd nologin; do
    command -v "$command" >/dev/null 2>&1 || { echo "required command not found: $command" >&2; exit 1; }
  done
fi

for path in "$PROGRAM_DIR" "$CONFIG_DIR" "$STATE_DIR" "$UNIT_PATH"; do
  if [[ -L "$path" ]]; then
    echo "refusing to install through symlink: $path" >&2
    exit 1
  fi
done
if [[ -e "$PROGRAM_DIR" && ! -d "$PROGRAM_DIR" ]]; then
  echo "program path exists but is not a directory: $PROGRAM_DIR" >&2
  exit 1
fi

work_dir=$(mktemp -d)
incoming="${PROGRAM_DIR%/*}/.fleetnode.install.$$"
previous="${PROGRAM_DIR%/*}/.fleetnode.previous.$$"
unit_backup="$work_dir/fleetnode.service.previous"
fresh_install=1
[[ -d "$PROGRAM_DIR" ]] && fresh_install=0
was_active=0
previous_saved=0
program_replaced=0
unit_replaced=0
install_complete=0

cleanup() {
  status=$?
  if [[ "$install_complete" != "1" ]]; then
    if [[ "$unit_replaced" == "1" ]]; then
      if [[ -f "$unit_backup" ]]; then
        install -m 0644 "$unit_backup" "$UNIT_PATH"
      else
        rm -f "$UNIT_PATH"
      fi
      "$SYSTEMCTL" daemon-reload >/dev/null 2>&1 || true
    fi
    if [[ "$program_replaced" == "1" ]]; then
      rm -rf "$PROGRAM_DIR"
    fi
    if [[ "$previous_saved" == "1" && -d "$previous" ]]; then
      mv "$previous" "$PROGRAM_DIR"
    fi
    if [[ "$was_active" == "1" ]]; then
      "$SYSTEMCTL" start fleetnode.service >/dev/null 2>&1 || true
    fi
  fi
  rm -rf "$work_dir" "$incoming"
  if [[ "$install_complete" == "1" ]]; then
    rm -rf "$previous"
  fi
  exit "$status"
}
trap cleanup EXIT

echo "Downloading Fleet Node $VERSION for linux/$ARCH..."
curl_options=(--disable --fail --location --silent --show-error)
if [[ "$TEST_MODE" != "1" ]]; then
  curl_options+=(--proto '=https' --proto-redir '=https')
fi
curl "${curl_options[@]}" \
  --output "$work_dir/$ARCHIVE_NAME" "$DOWNLOAD_BASE_URL/$ARCHIVE_NAME"
curl "${curl_options[@]}" \
  --output "$work_dir/$ARCHIVE_NAME.sha256" "$DOWNLOAD_BASE_URL/$ARCHIVE_NAME.sha256"

(
  cd "$work_dir"
  sha256sum "$ARCHIVE_NAME" > expected.sha256
  if ! cmp -s expected.sha256 "$ARCHIVE_NAME.sha256"; then
    echo "checksum sidecar is not bound to $ARCHIVE_NAME" >&2
    exit 1
  fi
  sha256sum -c "$ARCHIVE_NAME.sha256"
)

while IFS= read -r entry; do
  case "$entry" in
    "$ARCHIVE_ROOT"|"$ARCHIVE_ROOT/"|"$ARCHIVE_ROOT/"*) ;;
    *) echo "archive contains an unexpected path: $entry" >&2; exit 1 ;;
  esac
  case "/$entry/" in
    */../*|*/./*) echo "archive contains an unsafe path: $entry" >&2; exit 1 ;;
  esac
done < <(tar -tzf "$work_dir/$ARCHIVE_NAME")

mkdir -p "$work_dir/extracted"
tar -xzf "$work_dir/$ARCHIVE_NAME" -C "$work_dir/extracted"
source_dir="$work_dir/extracted/$ARCHIVE_ROOT"
[[ -d "$source_dir" && ! -L "$source_dir" ]] || { echo "archive root must be a directory" >&2; exit 1; }
for required in \
  fleetnode \
  version.txt \
  fleetnode.service \
  plugins/proto-plugin \
  plugins/antminer-plugin \
  plugins/virtual-plugin \
  plugins/virtual-plugin.json \
  plugins/asicrs-plugin \
  plugins/asicrs-config.yaml; do
  [[ -f "$source_dir/$required" ]] || { echo "archive is missing $required" >&2; exit 1; }
done
if find "$source_dir" -type l -print -quit | grep -q .; then
  echo "archive must not contain symlinks" >&2
  exit 1
fi
if find "$source_dir" -type f -links +1 -print -quit | grep -q .; then
  echo "archive must not contain hard links" >&2
  exit 1
fi
while IFS= read -r -d '' path; do
  relative_path="${path#"$source_dir"/}"
  case "$relative_path" in
    fleetnode|version.txt|fleetnode.service|plugins|plugins/proto-plugin|plugins/antminer-plugin|plugins/virtual-plugin|plugins/virtual-plugin.json|plugins/asicrs-plugin|plugins/asicrs-config.yaml) ;;
    *) echo "archive contains an unexpected entry: $relative_path" >&2; exit 1 ;;
  esac
done < <(find "$source_dir" -mindepth 1 -print0)
if ! grep -Fxq "version: $VERSION" "$source_dir/version.txt"; then
  echo "Fleet Node archive metadata does not match requested version '$VERSION'" >&2
  exit 1
fi

if "$SYSTEMCTL" is-active --quiet fleetnode.service; then
  was_active=1
  "$SYSTEMCTL" stop fleetnode.service
fi

install -d -m 0755 "${PROGRAM_DIR%/*}" "${UNIT_PATH%/*}"
if [[ "$TEST_MODE" == "1" ]]; then
  install -d -m 0750 "$CONFIG_DIR"
  install -d -m 0700 "$STATE_DIR"
else
  if ! getent passwd fleetnode >/dev/null; then
    if getent group fleetnode >/dev/null; then
      useradd --system --gid fleetnode --home-dir /var/lib/fleetnode --shell "$(command -v nologin)" fleetnode
    else
      useradd --system --user-group --home-dir /var/lib/fleetnode --shell "$(command -v nologin)" fleetnode
    fi
  fi
  getent group fleetnode >/dev/null || { echo "fleetnode user exists without a fleetnode group" >&2; exit 1; }
  install -d -o root -g fleetnode -m 0750 "$CONFIG_DIR"
  install -d -o fleetnode -g fleetnode -m 0700 "$STATE_DIR"
fi

rm -rf "$incoming" "$previous"
install -d -m 0755 "$incoming" "$incoming/plugins"
install -m 0755 "$source_dir/fleetnode" "$incoming/fleetnode"
install -m 0644 "$source_dir/version.txt" "$incoming/version.txt"
install -m 0644 "$source_dir/fleetnode.service" "$incoming/fleetnode.service"
install -m 0755 "$source_dir/plugins/proto-plugin" "$incoming/plugins/proto-plugin"
install -m 0755 "$source_dir/plugins/antminer-plugin" "$incoming/plugins/antminer-plugin"
install -m 0755 "$source_dir/plugins/virtual-plugin" "$incoming/plugins/virtual-plugin"
install -m 0644 "$source_dir/plugins/virtual-plugin.json" "$incoming/plugins/virtual-plugin.json"
install -m 0755 "$source_dir/plugins/asicrs-plugin" "$incoming/plugins/asicrs-plugin"
install -m 0644 "$source_dir/plugins/asicrs-config.yaml" "$incoming/plugins/asicrs-config.yaml"
if [[ "$TEST_MODE" != "1" ]]; then
  chown -R root:root "$incoming"
fi
if [[ -d "$PROGRAM_DIR" ]]; then
  mv "$PROGRAM_DIR" "$previous"
  previous_saved=1
fi
mv "$incoming" "$PROGRAM_DIR"
program_replaced=1

if [[ -f "$UNIT_PATH" ]]; then
  cp -p "$UNIT_PATH" "$unit_backup"
fi
install -m 0644 "$PROGRAM_DIR/fleetnode.service" "$UNIT_PATH"
unit_replaced=1
"$SYSTEMCTL" daemon-reload

if [[ "$fresh_install" == "1" ]]; then
  if "$SYSTEMCTL" is-enabled --quiet fleetnode.service; then
    "$SYSTEMCTL" disable fleetnode.service
  fi
elif [[ "$was_active" == "1" ]]; then
  "$SYSTEMCTL" start fleetnode.service
fi

install_complete=1
echo "Fleet Node $VERSION installed."
echo "Configuration: $CONFIG_DIR"
echo "Protected state: $STATE_DIR"
if [[ "$fresh_install" == "1" ]]; then
  echo "The new service remains disabled until enrollment is complete."
elif [[ "$was_active" == "1" ]]; then
  echo "The previously running service was restarted; its enablement state was preserved."
else
  echo "The service was not started; its enablement state was preserved."
fi
echo
echo "Enroll:"
echo "  sudo -u fleetnode $PROGRAM_DIR/fleetnode --state-dir $STATE_DIR enroll --server-url=https://YOUR-FLEET-SERVER"
echo "Then enable and start:"
echo "  sudo systemctl enable --now fleetnode.service"
echo "Inspect:"
echo "  systemctl status fleetnode.service"
echo "  journalctl -u fleetnode.service"
