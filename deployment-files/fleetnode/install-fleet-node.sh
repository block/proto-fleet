#!/usr/bin/env bash
set -euo pipefail

DOWNLOAD_BASE_URL="${FLEETNODE_DOWNLOAD_BASE_URL:-}"
TEST_MODE="${FLEETNODE_TEST_MODE:-0}"
ROOT_PREFIX="${FLEETNODE_ROOT_PREFIX:-}"
LINUX_SERVICE_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
USE_SUDO=0
INSTALL_LOCK_PID=""
INSTALL_LOCK_RELEASE_PATH=""
ACTION=install
VERSION=""

if [[ "$TEST_MODE" != "1" ]]; then
  PATH="$LINUX_SERVICE_PATH"
  export PATH
fi

usage() {
  cat <<'EOF'
Usage: install-fleet-node.sh VERSION
       install-fleet-node.sh uninstall

Install one exact Proto Fleet Node release on Linux. VERSION must be a
release-specific tag such as v1.2.3 or nightly-20260825-0123456789ab.

uninstall removes the program and systemd unit while preserving configuration,
state, and the service account.

The installer leaves a fresh installation stopped and disabled at boot. After
installation, enroll the node and then enable and start the service.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi
case "${1:-}" in
  uninstall)
    ACTION=uninstall
    [[ $# -eq 1 ]] || { usage >&2; exit 2; }
    ;;
  "") usage >&2; exit 2 ;;
  *)
    [[ $# -eq 1 ]] || { usage >&2; exit 2; }
    VERSION="$1"
    if [[ ! "$VERSION" =~ ^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$ ]] || [[ "$VERSION" == "latest" ]]; then
      echo "version must be an explicit release identifier (1-128 letters, numbers, dots, underscores, or hyphens; not latest)" >&2
      exit 2
    fi
    ;;
esac
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
if [[ "$(id -u)" -ne 0 ]]; then
  command -v sudo >/dev/null 2>&1 || { echo "required command not found: sudo" >&2; exit 1; }
  USE_SUDO=1
fi

as_root() {
  if [[ "$USE_SUDO" == "1" ]]; then
    sudo "$@"
  else
    "$@"
  fi
}

if [[ "$ACTION" == "install" ]]; then
  case "${FLEETNODE_ARCH:-$(uname -m)}" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported Linux architecture: ${FLEETNODE_ARCH:-$(uname -m)}" >&2; exit 1 ;;
  esac
fi

if [[ "$TEST_MODE" == "1" ]]; then
  [[ -n "$ROOT_PREFIX" && "$ROOT_PREFIX" == /* && "$ROOT_PREFIX" != "/" ]] || {
    echo "test mode requires an absolute, non-root FLEETNODE_ROOT_PREFIX" >&2
    exit 1
  }
fi

PROGRAM_DIR="${ROOT_PREFIX}/opt/fleetnode"
CONFIG_DIR="${ROOT_PREFIX}/etc/fleetnode"
STATE_DIR="${ROOT_PREFIX}/var/lib/fleetnode"
UNIT_PATH="${ROOT_PREFIX}/etc/systemd/system/fleet-node.service"
INSTALL_LOCK_DIR="${ROOT_PREFIX}/run/proto-fleet"
INSTALL_LOCK_PATH="$INSTALL_LOCK_DIR/fleetnode-installer.lock"
SYSTEMCTL="${FLEETNODE_SYSTEMCTL:-systemctl}"
ARCHIVE_ROOT=""
ARCHIVE_NAME=""
if [[ "$ACTION" == "install" ]]; then
  ARCHIVE_ROOT="fleetnode-${VERSION}-linux-${ARCH}"
  ARCHIVE_NAME="${ARCHIVE_ROOT}.tar.gz"
fi
if [[ "$ACTION" == "install" && -z "$DOWNLOAD_BASE_URL" ]]; then
  DOWNLOAD_BASE_URL="https://github.com/block/proto-fleet/releases/download/${VERSION}"
fi

for command in install flock "$SYSTEMCTL"; do
  command -v "$command" >/dev/null 2>&1 || { echo "required command not found: $command" >&2; exit 1; }
done
if [[ "$ACTION" == "install" ]]; then
  for command in curl sha256sum tar; do
    command -v "$command" >/dev/null 2>&1 || { echo "required command not found: $command" >&2; exit 1; }
  done
fi
if [[ "$TEST_MODE" != "1" && ! -x /usr/bin/env ]]; then
  echo "required command not found: /usr/bin/env" >&2
  exit 1
fi
if [[ "$ACTION" == "install" ]] && ! command -v nmap >/dev/null 2>&1; then
  echo "required command not found: nmap; install nmap in the Fleet Node service PATH ($LINUX_SERVICE_PATH)" >&2
  exit 1
fi
if [[ "$TEST_MODE" != "1" && "$ACTION" == "install" ]]; then
  for command in getent id nologin runuser useradd; do
    command -v "$command" >/dev/null 2>&1 || { echo "required command not found: $command" >&2; exit 1; }
  done
fi

SYSTEMD_RUNTIME_DIR="${ROOT_PREFIX}/run/systemd/system"
if [[ ! -d "$SYSTEMD_RUNTIME_DIR" || -L "$SYSTEMD_RUNTIME_DIR" ]]; then
  echo "Fleet Node installation requires a running systemd system manager" >&2
  exit 1
fi
if ! as_root "$SYSTEMCTL" show --property=Version --value >/dev/null 2>&1; then
  echo "Fleet Node installation requires a running systemd system manager" >&2
  exit 1
fi

for path in "${PROGRAM_DIR%/*}" "${UNIT_PATH%/*}" "$PROGRAM_DIR" "$CONFIG_DIR" "$STATE_DIR" "$UNIT_PATH" "$INSTALL_LOCK_DIR" "$INSTALL_LOCK_PATH"; do
  if [[ -L "$path" ]]; then
    echo "refusing to install through symlink: $path" >&2
    exit 1
  fi
done
for path in "${PROGRAM_DIR%/*}" "${UNIT_PATH%/*}" "$PROGRAM_DIR" "$CONFIG_DIR" "$STATE_DIR"; do
  if [[ -e "$path" && ! -d "$path" ]]; then
    echo "directory path exists but is not a directory: $path" >&2
    exit 1
  fi
done

ensure_shared_directory() {
  local path="$1"
  if [[ -e "$path" ]]; then
    [[ -d "$path" && ! -L "$path" ]] || { echo "shared path is not a directory: $path" >&2; return 1; }
    return
  fi
  if [[ "$TEST_MODE" == "1" ]]; then
    as_root install -d -m 0755 "$path"
  else
    as_root install -d -o root -g root -m 0755 "$path"
  fi
}

group_record=""
passwd_record=""
account_uid=""

load_service_account() {
  passwd_record=""
  group_record=""
  if getent passwd fleetnode >/dev/null 2>&1; then
    passwd_record=$(getent passwd fleetnode)
  fi
  if getent group fleetnode >/dev/null 2>&1; then
    group_record=$(getent group fleetnode)
  fi
}

validate_group() {
  [[ -n "$group_record" ]] || { echo "fleetnode user exists without a fleetnode group" >&2; return 1; }
  local _name _password _uid _rest group_gid members member passwd_entries passwd_name passwd_gid
  local -a group_members=()
  IFS=: read -r _name _password group_gid members <<< "$group_record"
  if [[ -n "$members" ]]; then
    IFS=, read -ra group_members <<< "$members"
    for member in "${group_members[@]}"; do
      [[ "$member" == "fleetnode" ]] || {
        echo "fleetnode group contains unrelated member: $member" >&2
        return 1
      }
    done
  fi

  passwd_entries=$(getent passwd) || { echo "cannot inspect primary group ownership for fleetnode group" >&2; return 1; }
  while IFS=: read -r passwd_name _password _uid passwd_gid _rest; do
    if [[ "$passwd_gid" == "$group_gid" && "$passwd_name" != "fleetnode" ]]; then
      echo "fleetnode group is the primary group for unrelated account: $passwd_name" >&2
      return 1
    fi
  done <<< "$passwd_entries"
}

validate_service_account() {
  load_service_account
  [[ -n "$passwd_record" ]] || { echo "fleetnode account does not exist" >&2; return 1; }
  validate_group

  local _name _password primary_gid _gecos home shell group_gid groups group
  IFS=: read -r _name _password account_uid primary_gid _gecos home shell <<< "$passwd_record"
  IFS=: read -r _name _password group_gid _ <<< "$group_record"
  [[ "$account_uid" != "0" ]] || { echo "fleetnode account must not be root" >&2; return 1; }
  [[ "$primary_gid" == "$group_gid" ]] || { echo "fleetnode account must use the fleetnode group as its primary group" >&2; return 1; }
  [[ "$home" == "/var/lib/fleetnode" ]] || { echo "fleetnode account must use home /var/lib/fleetnode" >&2; return 1; }
  [[ "$shell" == "$(command -v nologin)" ]] || { echo "fleetnode account must use the system nologin shell" >&2; return 1; }
  groups=$(id -nG fleetnode) || { echo "cannot inspect fleetnode group membership" >&2; return 1; }
  for group in $groups; do
    [[ "$group" == "fleetnode" ]] || { echo "fleetnode account belongs to unrelated group: $group" >&2; return 1; }
  done
  [[ " $groups " == *" fleetnode "* ]] || { echo "fleetnode account is not a member of fleetnode group" >&2; return 1; }
}

validate_existing_service_account() {
  load_service_account
  if [[ -n "$passwd_record" ]]; then
    validate_service_account
  elif [[ -n "$group_record" ]]; then
    validate_group
    local _name _password _gid members
    IFS=: read -r _name _password _gid members <<< "$group_record"
    [[ -z "$members" ]] || { echo "fleetnode group exists without its service account" >&2; return 1; }
  elif [[ -e "$STATE_DIR" || -e "$CONFIG_DIR" ]]; then
    echo "preserved Fleet Node data exists without the fleetnode service account" >&2
    return 1
  fi
}

validate_preserved_access() {
  load_service_account
  [[ -n "$passwd_record" ]] || return 0
  if [[ -e "$STATE_DIR" ]]; then
    [[ -d "$STATE_DIR" && ! -L "$STATE_DIR" ]] || { echo "state path is not a directory: $STATE_DIR" >&2; return 1; }
    as_root runuser -u fleetnode -- test -x "$STATE_DIR" && \
      as_root runuser -u fleetnode -- test -w "$STATE_DIR" || {
        echo "fleetnode cannot traverse and write preserved state directory: $STATE_DIR" >&2
        return 1
      }
  fi
  local path
  for path in "$STATE_DIR/state.yaml" "$CONFIG_DIR/config.yaml" "$CONFIG_DIR/fleetnode.env"; do
    if [[ -e "$path" ]] && ! as_root runuser -u fleetnode -- test -r "$path"; then
      echo "fleetnode cannot read preserved file: $path" >&2
      return 1
    fi
  done
}

if [[ "$ACTION" == "install" ]]; then
  validate_existing_service_account
  validate_preserved_access
fi

ensure_shared_directory "$INSTALL_LOCK_DIR"
as_root touch "$INSTALL_LOCK_PATH"
if [[ "$TEST_MODE" != "1" ]]; then
  as_root chown root:root "$INSTALL_LOCK_PATH"
fi
as_root chmod 0600 "$INSTALL_LOCK_PATH"

work_dir=$(mktemp -d)
INSTALL_LOCK_RELEASE_PATH="$work_dir/release-install-lock"

# Keep the privileged flock process alive through a pipe while this unprivileged
# installer performs downloads and invokes only the mutations that need sudo.
exec 8< <(
  # $1 is expanded by the privileged child shell.
  # shellcheck disable=SC2016
  if as_root flock -n "$INSTALL_LOCK_PATH" bash -c '
    printf "%s\n" locked
    while [[ ! -e "$1" ]] && kill -0 "$2" 2>/dev/null; do sleep 0.1; done
  ' bash "$INSTALL_LOCK_RELEASE_PATH" "$$"; then
    :
  else
    printf '%s\n' failed
  fi
)
INSTALL_LOCK_PID=$!
if ! IFS= read -r lock_state <&8 || [[ "$lock_state" != "locked" ]]; then
  wait "$INSTALL_LOCK_PID" 2>/dev/null || true
  exec 8<&-
  rm -rf "$work_dir"
  echo "another Fleet Node installer is running" >&2
  exit 1
fi

incoming="${PROGRAM_DIR%/*}/.fleetnode.install.$$"
previous="${PROGRAM_DIR%/*}/.fleetnode.previous.$$"
unit_backup="$work_dir/fleet-node.service.previous"
fresh_install=1
[[ -x "$PROGRAM_DIR/fleetnode" && -f "$UNIT_PATH" ]] && fresh_install=0
service_stopped=0
previous_saved=0
program_replaced=0
unit_replaced=0
install_complete=0

cleanup() {
  status=$?
  set +e
  rollback_failed=0
  rollback_error() {
    echo "rollback failed: $*" >&2
    rollback_failed=1
  }
  if [[ "$install_complete" != "1" ]]; then
    if [[ "$service_stopped" == "1" ]]; then
      as_root "$SYSTEMCTL" stop fleet-node.service || rollback_error "stop candidate Fleet Node service"
    fi
    if [[ "$unit_replaced" == "1" ]]; then
      if [[ -f "$unit_backup" ]]; then
        as_root install -m 0644 "$unit_backup" "$UNIT_PATH" || rollback_error "restore previous systemd unit"
      else
        as_root rm -f "$UNIT_PATH" || rollback_error "remove candidate systemd unit"
      fi
      as_root "$SYSTEMCTL" daemon-reload || rollback_error "reload systemd after restoring previous unit"
    fi
    if [[ "$program_replaced" == "1" ]]; then
      as_root rm -rf "$PROGRAM_DIR" || rollback_error "remove candidate Fleet Node payload"
    fi
    if [[ "$previous_saved" == "1" && -d "$previous" ]]; then
      as_root mv "$previous" "$PROGRAM_DIR" || rollback_error "restore previous Fleet Node payload"
    fi
    if [[ "$service_stopped" == "1" ]]; then
      as_root "$SYSTEMCTL" start fleet-node.service || rollback_error "restart previous Fleet Node service"
    fi
  fi
  if [[ -n "$INSTALL_LOCK_PID" ]]; then
    touch "$INSTALL_LOCK_RELEASE_PATH"
    wait "$INSTALL_LOCK_PID" 2>/dev/null || true
    exec 8<&-
  fi
  as_root rm -rf "$work_dir" "$incoming"
  if [[ "$install_complete" == "1" ]]; then
    as_root rm -rf "$previous"
  fi
  if [[ "$rollback_failed" == "1" ]]; then
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT

remove_fleet_node() {
  local load_state
  load_state=$(as_root "$SYSTEMCTL" show --property=LoadState --value fleet-node.service) || {
    echo "cannot inspect Fleet Node systemd unit" >&2
    return 1
  }
  case "$load_state" in
    loaded) as_root "$SYSTEMCTL" stop fleet-node.service ;;
    not-found) ;;
    *) echo "unexpected Fleet Node systemd unit load state: $load_state" >&2; return 1 ;;
  esac
  if as_root "$SYSTEMCTL" is-enabled --quiet fleet-node.service; then
    as_root "$SYSTEMCTL" disable fleet-node.service
  fi

  as_root rm -rf "$PROGRAM_DIR"
  as_root rm -f "$UNIT_PATH"
  as_root "$SYSTEMCTL" daemon-reload
  as_root "$SYSTEMCTL" reset-failed fleet-node.service >/dev/null 2>&1 || true

  echo "Fleet Node uninstalled. Preserved:"
  echo "  $CONFIG_DIR"
  echo "  $STATE_DIR"
  echo "  fleetnode service account"
}

if [[ "$ACTION" == "uninstall" ]]; then
  remove_fleet_node
  install_complete=1
  exit 0
fi

echo "Downloading Fleet Node $VERSION for linux/$ARCH..."
curl_options=(
  --disable
  --fail
  --location
  --silent
  --show-error
  --connect-timeout 10
  --max-time 300
  --retry 3
  --retry-delay 2
  --retry-connrefused
)
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
  fleet-node.service \
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
    fleetnode|version.txt|fleet-node.service|plugins|plugins/proto-plugin|plugins/antminer-plugin|plugins/virtual-plugin|plugins/virtual-plugin.json|plugins/asicrs-plugin|plugins/asicrs-config.yaml) ;;
    *) echo "archive contains an unexpected entry: $relative_path" >&2; exit 1 ;;
  esac
done < <(find "$source_dir" -mindepth 1 -print0)
if ! grep -Fxq "version: $VERSION" "$source_dir/version.txt"; then
  echo "Fleet Node archive metadata does not match requested version '$VERSION'" >&2
  exit 1
fi

if [[ "$fresh_install" == "0" ]]; then
  if as_root "$SYSTEMCTL" is-active --quiet fleet-node.service; then
    as_root "$SYSTEMCTL" stop fleet-node.service
    service_stopped=1
  fi
fi

ensure_shared_directory "${PROGRAM_DIR%/*}"
ensure_shared_directory "${UNIT_PATH%/*}"

if ! getent passwd fleetnode >/dev/null 2>&1; then
  if getent group fleetnode >/dev/null 2>&1; then
    as_root useradd --system --gid fleetnode --home-dir /var/lib/fleetnode --shell "$(command -v nologin)" fleetnode
  else
    as_root useradd --system --user-group --home-dir /var/lib/fleetnode --shell "$(command -v nologin)" fleetnode
  fi
fi
validate_service_account

if [[ "$TEST_MODE" == "1" ]]; then
  as_root install -d -m 0750 "$CONFIG_DIR"
  as_root install -d -m 0700 "$STATE_DIR"
else
  as_root install -d -o root -g fleetnode -m 0750 "$CONFIG_DIR"
  as_root install -d -o fleetnode -g fleetnode -m 0700 "$STATE_DIR"
fi

as_root rm -rf "$incoming" "$previous"
as_root install -d -m 0755 "$incoming" "$incoming/plugins"
as_root install -m 0755 "$source_dir/fleetnode" "$incoming/fleetnode"
as_root install -m 0644 "$source_dir/version.txt" "$incoming/version.txt"
as_root install -m 0644 "$source_dir/fleet-node.service" "$incoming/fleet-node.service"
as_root install -m 0755 "$source_dir/plugins/proto-plugin" "$incoming/plugins/proto-plugin"
as_root install -m 0755 "$source_dir/plugins/antminer-plugin" "$incoming/plugins/antminer-plugin"
as_root install -m 0755 "$source_dir/plugins/virtual-plugin" "$incoming/plugins/virtual-plugin"
as_root install -m 0644 "$source_dir/plugins/virtual-plugin.json" "$incoming/plugins/virtual-plugin.json"
as_root install -m 0755 "$source_dir/plugins/asicrs-plugin" "$incoming/plugins/asicrs-plugin"
as_root install -m 0644 "$source_dir/plugins/asicrs-config.yaml" "$incoming/plugins/asicrs-config.yaml"
if [[ "$TEST_MODE" != "1" ]]; then
  as_root chown -R root:root "$incoming"
fi
if [[ -d "$PROGRAM_DIR" ]]; then
  as_root mv "$PROGRAM_DIR" "$previous"
  previous_saved=1
fi
as_root mv "$incoming" "$PROGRAM_DIR"
program_replaced=1

if [[ -f "$UNIT_PATH" ]]; then
  as_root cp -p "$UNIT_PATH" "$unit_backup"
fi
unit_replaced=1
as_root install -m 0644 "$PROGRAM_DIR/fleet-node.service" "$UNIT_PATH"
as_root "$SYSTEMCTL" daemon-reload

if [[ "$fresh_install" == "1" ]]; then
  if as_root "$SYSTEMCTL" is-enabled --quiet fleet-node.service; then
    as_root "$SYSTEMCTL" disable fleet-node.service
  fi
else
  # Type=notify keeps this call blocked until Fleet Node finishes local
  # initialization and reports READY=1, so the previous payload remains
  # available to the EXIT trap until the candidate is actually runnable.
  if [[ "$service_stopped" == "1" ]]; then
    as_root "$SYSTEMCTL" start fleet-node.service
  fi
fi

install_complete=1
echo "Fleet Node $VERSION installed."
echo "Configuration: $CONFIG_DIR"
echo "Protected state: $STATE_DIR"
echo
echo "Enroll:"
echo "  sudo -u fleetnode $PROGRAM_DIR/fleetnode --state-dir $STATE_DIR enroll --server-url=https://YOUR-FLEET-SERVER"
echo "Then enable and start the service:"
echo "  sudo systemctl enable --now fleet-node.service"
echo "Inspect:"
echo "  systemctl status fleet-node.service"
echo "  journalctl -u fleet-node.service"
