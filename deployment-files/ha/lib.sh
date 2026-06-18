#!/usr/bin/env bash

ha_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_root="$(cd "$ha_dir/.." && pwd)"
ha_env_file="$ha_dir/ha.env"
ha_state_dir="$ha_dir/state"

log() {
  echo "ha: $*"
}

warn() {
  echo "ha warning: $*" >&2
}

die() {
  echo "ha error: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

load_env() {
  if [ -f "$project_root/.env" ]; then
    set -a
    # shellcheck disable=SC1091
    . "$project_root/.env"
    set +a
  fi

  if [ -f "$ha_env_file" ]; then
    set -a
    # shellcheck disable=SC1091
    . "$ha_env_file"
    set +a
  fi
}

docker_compose() {
  docker compose "$@"
}

compose_with_env() {
  load_env
  docker_compose "$@"
}

first_host_ip() {
  hostname -I 2>/dev/null | awk '{print $1}'
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "sha256sum or shasum is required"
  fi
}

sha256_stream() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 | awk '{print $1}'
  else
    die "sha256sum or shasum is required"
  fi
}

write_ha_env() {
  mkdir -p "$ha_dir"
  umask 077
  cat > "$ha_env_file" <<EOF
HA_ROLE=${HA_ROLE}
HA_CLUSTER=${HA_CLUSTER:-fleet-ha}
HA_NODE_NAME=${HA_NODE_NAME:-}
HA_NODE_HOST=${HA_NODE_HOST}
HA_MONITOR_HOST=${HA_MONITOR_HOST:-}
HA_MONITOR_URL=${HA_MONITOR_URL:-}
HA_AUTH_METHOD=${HA_AUTH_METHOD:-trust}
HA_SSL_SELF_SIGNED=${HA_SSL_SELF_SIGNED:-false}
PGPORT=${PGPORT:-5432}
EOF
}
