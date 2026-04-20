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

  local blocked_paths=("/" "/home" "/usr" "/etc" "/var" "/opt" "/root" "/tmp" "/bin" "/sbin" "/lib" "/sys" "/dev" "/proc" "/boot" "/run" "/mnt" "/srv" "/media")
  for blocked in "${blocked_paths[@]}"; do
    if [[ "$resolved" == "$blocked" ]]; then
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

get_default_install_dir() {
  local os_type
  os_type=$(uname -s)

  if [[ "$os_type" == "Darwin" ]]; then
    echo "$HOME/Applications/ProtoFleet"
  else
    echo "$HOME/proto-fleet"
  fi
}

# =====================================================================
# Deployment Path Detection
# =====================================================================

find_previous_install_dir() {
  local container_id
  container_id=$(docker ps -a \
    --filter "name=${DEPLOYMENT_DIR}-fleet-api" \
    --filter "name=${DEPLOYMENT_DIR}_fleet-api" \
    --format "{{.ID}}" 2>/dev/null | head -n 1 || true)

  if [[ -z "$container_id" ]]; then
    return 1
  fi

  local mount_path
  mount_path=$(docker inspect --format \
    '{{range .Mounts}}{{if eq .Destination "/var/lib/fleet/start"}}{{.Source}}{{end}}{{end}}' \
    "$container_id" 2>/dev/null || true)

  if [[ -z "$mount_path" ]]; then
    return 1
  fi

  local install_dir
  install_dir=$(echo "$mount_path" | sed "s|/${DEPLOYMENT_DIR}.*$||" || true)
  echo "$install_dir"
  return 0
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
    local detected
    detected=$(find_previous_install_dir || echo "")
    if [[ -n "$detected" ]]; then
      local candidate="${detected}/${DEPLOYMENT_DIR}"
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
  read -rp "Proceed with uninstall? (y/N): " confirm
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
