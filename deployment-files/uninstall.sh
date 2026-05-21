#!/usr/bin/env bash
set -euo pipefail

# Proto Fleet Uninstaller
# Removes Proto Fleet containers, images, systemd units, and deployment files.
# Always performs a clean uninstall (including data volumes).

DEPLOYMENT_DIR="deployment"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Defaults
DEPLOYMENT_PATH=""
INSTALL_ROOT=""
DRY_RUN=false

# =====================================================================
# Output Helpers
# =====================================================================

print_step()    { echo -e "\n\033[36m$1\033[0m"; }
print_success() { echo -e "\033[32m[OK] $1\033[0m"; }
print_warn()    { echo -e "\033[33m[WARN] $1\033[0m"; }
print_error()   { echo -e "\033[31m[ERROR] $1\033[0m"; }

# =====================================================================
# Usage
# =====================================================================

usage() {
  cat <<EOF
Usage: $0 [OPTIONS]

Proto Fleet Uninstaller

Options:
  --deployment-path PATH Explicit path to deployment or install root directory
  --dry-run              Show actions without executing
  -h, --help             Show this help message

This uninstaller always performs a clean uninstall:
  - removes containers, images, and volumes
  - removes systemd user units
  - deletes the deployment directory
EOF
  exit 0
}

# =====================================================================
# Argument Parsing
# =====================================================================

# Capture the original argv before parsing so the sudo re-run hint below
# can preserve any flags the user passed (--deployment-path, --dry-run).
# `set -u` -safe — empty `"$@"` produces an empty array.
ORIGINAL_ARGV=("$@")

# Shell-escape ORIGINAL_ARGV for embedding in a copy-pasteable sudo command.
# `printf ' %q'` cycles the format spec over every arg, so a single call
# shell-escapes the whole array. Emits nothing when the array is empty so
# the caller can append the result directly to a base command string.
quoted_rerun_argv() {
  (( ${#ORIGINAL_ARGV[@]} )) && printf ' %q' "${ORIGINAL_ARGV[@]}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --deployment-path)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        print_error "--deployment-path requires a PATH argument."
        usage
      fi
      DEPLOYMENT_PATH="$2"; shift 2 ;;
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help) usage ;;
    *)
      print_error "Unknown option: $1"
      usage
      ;;
  esac
done

# =====================================================================
# Safety Checks
# =====================================================================

canonicalize_existing_dir() {
  local path="$1"

  if [[ ! -d "$path" ]]; then
    return 1
  fi

  (cd "$path" 2>/dev/null && pwd -P)
}

assert_safe_removal_path() {
  local path="$1"

  if [[ -z "$path" ]]; then
    print_error "Empty path provided for removal."
    exit 1
  fi

  local resolved
  if ! resolved="$(canonicalize_existing_dir "$path")"; then
    print_error "Refusing to remove unresolved path: $path"
    exit 1
  fi
  resolved="${resolved%/}"

  # Two-tier blocklist:
  #   - exact_blocked  — paths whose SUBPATHS can legitimately host installs
  #                       (e.g., /home/<user>/proto-fleet, /opt/proto-fleet).
  #                       Only the top-level dir itself is refused.
  #   - subtree_blocked — system paths where no install should ever live;
  #                        rm -rf anywhere under here is dangerous.
  # Without the subtree arm, a misrouted PREVIOUS_INSTALL_DIR resolving
  # to e.g. /etc/foo or /sys/x would slip past and let rm -rf damage
  # system-owned space.
  local exact_blocked=("/" "/home" "/Users" "/root" "/opt" "/usr" "/var" "/tmp" "/srv" "/mnt" "/media")
  local subtree_blocked=("/etc" "/bin" "/sbin" "/lib" "/lib64" "/sys" "/dev" "/proc" "/boot" "/run")
  for blocked in "${exact_blocked[@]}"; do
    if [[ "$resolved" == "$blocked" ]]; then
      print_error "Refusing to remove dangerous path: $resolved"
      exit 1
    fi
  done
  for blocked in "${subtree_blocked[@]}"; do
    if [[ "$resolved" == "$blocked" || "$resolved" == "$blocked"/* ]]; then
      print_error "Refusing to remove dangerous path: $resolved"
      exit 1
    fi
  done

  local home_resolved
  home_resolved="$(canonicalize_existing_dir "$HOME")"
  if [[ "$resolved" == "$home_resolved" ]]; then
    print_error "Refusing to remove home directory: $resolved"
    exit 1
  fi
}

validate_deployment_path() {
  local path="$1"
  [[ -f "$path/docker-compose.yaml" ]] &&
  [[ -d "$path/server" ]] &&
  [[ -d "$path/client" ]] &&
  grep -q "fleet-api" "$path/docker-compose.yaml" 2>/dev/null
}

# When invoked under sudo on Linux, prefer the invoking user's home over
# /root — fleet is normally installed under the user account, and falling
# back to /root/proto-fleet would miss the user's on-disk install.
get_default_install_dir() {
  local os_type
  os_type=$(uname -s)

  if [[ "$os_type" == "Darwin" ]]; then
    echo "$HOME/Applications/ProtoFleet"
    return
  fi

  if [[ "$(id -u)" -eq 0 ]] && [[ -n "${SUDO_USER:-}" ]] && [[ "$SUDO_USER" != "root" ]]; then
    # `|| true` neutralizes set -e / pipefail so a missing getent or failed
    # NSS lookup falls through to $HOME instead of aborting.
    local sudo_home
    sudo_home=$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6 || true)
    if [[ -n "$sudo_home" ]]; then
      echo "$sudo_home/proto-fleet"
      return
    fi
    # SUDO_USER is set but resolution failed (deleted account, NSS hiccup,
    # missing getent). Surface this on stderr so the operator knows the
    # default below is /root, not their home — otherwise the silent
    # degradation looks like the SUDO_USER branch never ran. Direct >&2
    # because this function's stdout is captured by the `$(...)` caller.
    echo "[WARN] SUDO_USER='$SUDO_USER' set but home lookup returned empty;" >&2
    echo "[WARN]   default install dir will fall back to \$HOME ($HOME/proto-fleet)." >&2
  fi

  echo "$HOME/proto-fleet"
}

# =====================================================================
# Deployment Path Detection
#
# Keep the two helpers below in sync with install.sh — install.sh is a
# piped bootstrap (`curl | bash`) and can't source a shared lib, so the
# detection logic intentionally lives in both scripts.
# =====================================================================

# Probe docker for an existing fleet-api container and return the install
# directory inferred from its bind mount. Echoes the path on success;
# returns 1 on miss.
#
# Takes a privilege-wrapper argv (empty for unprivileged, or `sudo -n` for
# elevated). All docker calls AND the marker validation are run through the
# wrapper at the same privilege level — without that, sudo-detected installs
# at root-only paths (e.g. /root/proto-fleet) would pass the docker probe
# but silently fail the unprivileged `test -f` check and look absent.
probe_install_dir_with() {
  local privilege=("$@")
  # `${arr[@]+"${arr[@]}"}` is the set-u-safe expansion for arrays that
  # may be empty; bare `"${arr[@]}"` errors on empty arrays in bash 3.2.
  local container_id
  container_id=$(${privilege[@]+"${privilege[@]}"} docker ps -a --filter "name=${DEPLOYMENT_DIR}-fleet-api" --filter "name=${DEPLOYMENT_DIR}_fleet-api" --format "{{.ID}}" 2>/dev/null | head -n 1 || true)
  [[ -z "$container_id" ]] && return 1

  local mount_path
  mount_path=$(${privilege[@]+"${privilege[@]}"} docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/fleet/start"}}{{.Source}}{{end}}{{end}}' "$container_id" 2>/dev/null || true)
  [[ -z "$mount_path" ]] && return 1

  # Strip the trailing /deployment/<...> segment with parameter expansion.
  # `${var%/deployment/*}` matches the shortest trailing match, so
  # /home/alice/deployment/proto-fleet/deployment/... resolves to
  # /home/alice/deployment/proto-fleet (not /home/alice). Fall back to
  # the bare /deployment suffix when the mount source ends exactly there.
  local install_dir="${mount_path%/${DEPLOYMENT_DIR}/*}"
  if [[ "$install_dir" == "$mount_path" ]]; then
    install_dir="${mount_path%/${DEPLOYMENT_DIR}}"
  fi
  if [[ "$install_dir" == "$mount_path" ]]; then
    return 1
  fi
  # Reject install_dir="/" — a mount source like /deployment/<x> would
  # otherwise propose deleting /deployment as the install root. No
  # supported install layout ever puts ProtoFleet directly at /.
  [[ -z "$install_dir" || "$install_dir" == "/" ]] && return 1

  # Marker check at the same privilege level. Empty stderr on non-zero
  # exit means the marker is genuinely missing; non-empty stderr means
  # sudo refused (e.g., sudoers permits docker but not test) — in that
  # case accept conservatively since docker already confirmed the
  # container.
  local marker="${install_dir%/}/${DEPLOYMENT_DIR}/docker-compose.yaml"
  if [[ "${#privilege[@]}" -eq 0 ]]; then
    [[ -f "$marker" ]] || return 1
  else
    local test_err
    if ! test_err=$(${privilege[@]+"${privilege[@]}"} test -f "$marker" 2>&1); then
      [[ -z "$test_err" ]] && return 1
    fi
  fi
  echo "$install_dir"
}

# Determines the installation directory by detecting previous installations.
# Writes results to globals (rather than stdout) so the sudo signal isn't
# lost across a subshell:
#   PREVIOUS_INSTALL_DIR          — install dir, or empty if none detected
#   PREVIOUS_INSTALL_NEEDS_SUDO   — 1 if the install was only visible via sudo
#   PREVIOUS_INSTALL_SUDO_BLOCKED — 1 if sudo would prompt and we couldn't
#                                   probe the root daemon at all
detect_previous_install() {
  PREVIOUS_INSTALL_DIR=""
  PREVIOUS_INSTALL_NEEDS_SUDO=0
  PREVIOUS_INSTALL_SUDO_BLOCKED=0

  local install_dir
  if install_dir=$(probe_install_dir_with); then
    PREVIOUS_INSTALL_DIR="$install_dir"
    return 0
  fi

  if [[ "$(id -u)" -eq 0 ]]; then
    if install_dir=$(probe_install_dir_with sudo -n); then
      PREVIOUS_INSTALL_DIR="$install_dir"
      PREVIOUS_INSTALL_NEEDS_SUDO=1
      return 0
    fi
    return 1
  fi

  # No sudo binary -> nothing to probe; skip to avoid leaking
  # "sudo: command not found" to user stderr on minimal hosts.
  command -v sudo >/dev/null 2>&1 || return 1

  # Probe sudo's view of docker directly and inspect stderr to distinguish
  # "sudo refused (needs password)" from "sudo ran but found no install".
  # `2>&1 >/dev/null` captures stderr only (redirect order matters).
  local sudo_probe_err
  sudo_probe_err=$(sudo -n docker version --format 'x' 2>&1 >/dev/null || true)
  # Anchor each pattern on `sudo:` so docker stderr that happens to mention
  # "password" / "terminal" / "tty" can't false-positive into SUDO_BLOCKED.
  case "$sudo_probe_err" in
    *"sudo: a password is required"*|*"sudo: a terminal is required"*|*"sudo:"*"may not run"*|*"sudo: no tty present"*|*"is not in the sudoers file"*)
      PREVIOUS_INSTALL_SUDO_BLOCKED=1
      return 1
      ;;
  esac

  if install_dir=$(probe_install_dir_with sudo -n); then
    PREVIOUS_INSTALL_DIR="$install_dir"
    PREVIOUS_INSTALL_NEEDS_SUDO=1
    return 0
  fi
  return 1
}

resolve_deployment_path() {
  local resolved=""

  # 1) User-provided path
  if [[ -n "$DEPLOYMENT_PATH" ]]; then
    local normalized="${DEPLOYMENT_PATH%/}"
    if [[ "$normalized" == *"/deployment" ]]; then
      resolved="$normalized"
    elif validate_deployment_path "$normalized/${DEPLOYMENT_DIR}"; then
      resolved="$normalized/${DEPLOYMENT_DIR}"
    elif validate_deployment_path "$normalized"; then
      resolved="$normalized"
    fi

    if [[ -z "$resolved" ]]; then
      print_error "Provided --deployment-path is invalid: $DEPLOYMENT_PATH"
      print_error "Expected a directory containing docker-compose.yaml, server/, and client/"
      exit 1
    fi
  fi

  # 2) Auto-detect via Docker container mounts
  if [[ -z "$resolved" ]]; then
    detect_previous_install || true
    if [[ -n "${PREVIOUS_INSTALL_DIR:-}" ]]; then
      # If the install was only visible via sudo and we aren't running as
      # root, the uninstaller will fail downstream (it needs to bring the
      # docker-compose stack down and delete root-owned files). Fail loud
      # now with the same guidance install.sh uses.
      if [[ "${PREVIOUS_INSTALL_NEEDS_SUDO:-0}" == "1" ]] && [[ "$(id -u)" -ne 0 ]]; then
        print_error "Existing fleet containers were detected, but only via sudo."
        print_error "They are managed by the root Docker daemon, and this script is running as $(id -un)."
        # Use the pipe form rather than `sudo bash $0` — when the user
        # invoked us via `bash <(curl ...)` (per README.md), $0 is a
        # transient /dev/fd/* descriptor that sudo cannot reopen, so the
        # naive suggestion fails immediately.
        print_error "Re-run the uninstaller as root (flags preserved):"
        echo ""
        echo "    curl -fsSL https://fleet.proto.xyz/uninstall.sh | sudo bash -s --$(quoted_rerun_argv)"
        echo ""
        exit 1
      fi
      local candidate="${PREVIOUS_INSTALL_DIR}/${DEPLOYMENT_DIR}"
      if validate_deployment_path "$candidate"; then
        resolved="$candidate"
      fi
    fi
  fi

  # 3) Check if we're running from within the deployment directory
  if [[ -z "$resolved" ]] && validate_deployment_path "$SCRIPT_DIR"; then
    resolved="$SCRIPT_DIR"
  fi

  # 4) Fallback to default location
  if [[ -z "$resolved" ]]; then
    local default_path
    default_path="$(get_default_install_dir)/${DEPLOYMENT_DIR}"
    if validate_deployment_path "$default_path"; then
      resolved="$default_path"
    fi
  fi

  if [[ -z "$resolved" ]]; then
    print_error "Could not locate a valid Proto Fleet deployment."
    print_error "Provide --deployment-path or ensure Proto Fleet is installed."
    # When sudo would have prompted for a password, we couldn't probe the
    # root daemon — a root-managed install could exist that we never saw.
    if [[ "${PREVIOUS_INSTALL_SUDO_BLOCKED:-0}" == "1" ]]; then
      print_error "(sudo required a password, so the root Docker daemon was not probed."
      print_error " If a root-managed install might exist, re-run as root:"
      print_error "   curl -fsSL https://fleet.proto.xyz/uninstall.sh | sudo bash -s --$(quoted_rerun_argv))"
    fi
    exit 1
  fi

  if ! DEPLOYMENT_PATH="$(canonicalize_existing_dir "$resolved")"; then
    print_error "Could not resolve deployment path: $resolved"
    exit 1
  fi

  # Derive install root (parent of deployment/)
  if [[ "$(basename "$DEPLOYMENT_PATH")" == "$DEPLOYMENT_DIR" ]]; then
    INSTALL_ROOT="$(dirname "$DEPLOYMENT_PATH")"
  else
    INSTALL_ROOT="$DEPLOYMENT_PATH"
  fi
}

# =====================================================================
# Action Helpers
# =====================================================================

run_action() {
  local description="$1"
  shift

  if $DRY_RUN; then
    echo "[DRY-RUN] $description"
    return
  fi

  print_step "$description"
  "$@"
}

docker_compose_cmd() {
  if command -v docker &>/dev/null && docker compose version &>/dev/null 2>&1; then
    docker compose "$@"
  elif command -v docker-compose &>/dev/null; then
    docker-compose "$@"
  else
    print_warn "Neither 'docker compose' nor 'docker-compose' found."
    return 1
  fi
}

# =====================================================================
# Uninstall Steps
# =====================================================================

teardown_docker_stack() {
  if [[ ! -f "$DEPLOYMENT_PATH/docker-compose.yaml" ]]; then
    print_warn "docker-compose.yaml not found, skipping Docker teardown."
    return
  fi

  cd "$DEPLOYMENT_PATH"
  docker_compose_cmd -f docker-compose.yaml down --rmi all --volumes --remove-orphans 2>/dev/null || true
  print_success "Containers, images, and volumes removed."
}

remove_systemd_units() {
  if ! command -v systemctl &>/dev/null; then
    print_success "systemctl not found, skipping systemd cleanup."
    return
  fi

  local units
  units="$(systemctl --user list-unit-files --type=service --no-legend 2>/dev/null \
    | awk '{print $1}' \
    | grep -E '^(protofleet|proto-fleet|fleet).*\.service$' || true)"

  if [[ -n "$units" ]]; then
    while IFS= read -r unit; do
      [[ -z "$unit" ]] && continue
      systemctl --user disable --now "$unit" 2>/dev/null || true
    done <<< "$units"
  fi

  systemctl --user daemon-reload 2>/dev/null || true
  systemctl --user reset-failed 2>/dev/null || true

  rm -f ~/.config/systemd/user/protofleet*.service \
        ~/.config/systemd/user/proto-fleet*.service \
        ~/.config/systemd/user/fleet*.service 2>/dev/null || true

  print_success "Systemd user units cleaned up."
}

remove_deployment_files() {
  assert_safe_removal_path "$DEPLOYMENT_PATH"

  if [[ -d "$DEPLOYMENT_PATH" ]]; then
    rm -rf "$DEPLOYMENT_PATH"
    print_success "Deployment directory removed: $DEPLOYMENT_PATH"
  else
    print_warn "Deployment directory not found: $DEPLOYMENT_PATH"
  fi

  # Remove install root if it's now empty
  if [[ -n "$INSTALL_ROOT" ]] && [[ -d "$INSTALL_ROOT" ]]; then
    assert_safe_removal_path "$INSTALL_ROOT"
    rmdir "$INSTALL_ROOT" 2>/dev/null || true
  fi

  # Clean up temp tarballs
  rm -f /tmp/proto-fleet-*.tar.gz 2>/dev/null || true
}

# =====================================================================
# Main
# =====================================================================

resolve_deployment_path

assert_safe_removal_path "$DEPLOYMENT_PATH"
assert_safe_removal_path "$INSTALL_ROOT"

if ! validate_deployment_path "$DEPLOYMENT_PATH"; then
  print_error "Path does not appear to be a valid Proto Fleet deployment: $DEPLOYMENT_PATH"
  exit 1
fi

echo ""
echo "Proto Fleet Uninstaller"
echo "  Deployment path: $DEPLOYMENT_PATH"
echo "  Install root:    $INSTALL_ROOT"
echo ""
echo "WARNING: This will permanently remove Proto Fleet from this machine, including:"
echo "  - Docker containers (Proto Fleet services)"
echo "  - Docker images used by Proto Fleet"
echo "  - Docker volumes (ALL Proto Fleet data)"
echo "  - systemd user unit files matching protofleet/proto-fleet/fleet*.service"
echo "  - Deployment directory: $DEPLOYMENT_PATH"
echo ""

if ! $DRY_RUN; then
  # Read from /dev/tty so the prompt works under `curl ... | sudo bash -s --`
  # (the sudo re-run form suggested above). Without this, stdin is the curl
  # pipe and the confirm read silently hits EOF.
  read -rp "Proceed with uninstall? (y/N): " confirm < /dev/tty
  if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
    echo "Uninstall canceled."
    exit 0
  fi
fi

run_action "Tearing down Proto Fleet Docker stack (containers/images/volumes)..." teardown_docker_stack
run_action "Removing Proto Fleet systemd units..." remove_systemd_units
run_action "Removing Proto Fleet deployment files..." remove_deployment_files

echo ""
if $DRY_RUN; then
  echo "Dry-run complete. No changes were made."
else
  echo "Uninstall complete."
  echo "  Deployment removed: Yes"
  echo "  Volumes removed: Yes"
fi
