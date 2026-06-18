#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

role=""

usage() {
  cat <<'EOF'
Usage: ha/preflight.sh --role monitor|data
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --role)
      role="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ "$role" = "monitor" ] || [ "$role" = "data" ] || die "--role must be monitor or data"

require_command docker
docker compose version >/dev/null 2>&1 || die "docker compose is required"

os_type=$(uname -s)
case "$os_type" in
  Linux|Darwin) ;;
  *) die "HA installs are supported on Linux and macOS only" ;;
esac

case "$(uname -m)" in
  aarch64|arm64|x86_64|amd64) ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ "$os_type" = "Linux" ]; then
  page_size=$(getconf PAGESIZE)
  if [ "$page_size" != "4096" ]; then
    die "system page size is $page_size; HA requires 4096-byte pages"
  fi
fi

if [ "$os_type" = "Darwin" ]; then
  warn "macOS HA installs are for local lab testing only"
  warn "Docker Desktop host networking must be enabled for --network host"
  warn "systemd is unavailable on macOS; Fleet follower automation must run manually or on the Linux data node"
fi

if ! docker info >/dev/null 2>&1; then
  die "docker daemon is not reachable"
fi

if [ ! -f "$project_root/docker-compose.ha-${role}.yaml" ]; then
  die "missing docker-compose.ha-${role}.yaml"
fi

if [ "$role" = "data" ]; then
  [ -f "$project_root/docker-compose.ha.yaml" ] || die "missing docker-compose.ha.yaml"
  [ -f "$project_root/.env" ] || die "missing .env; HA data nodes validate existing config and do not create secrets"
  chmod 600 "$project_root/.env" 2>/dev/null || true
fi

log "preflight passed for $role"
