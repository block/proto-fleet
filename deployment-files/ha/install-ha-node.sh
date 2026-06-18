#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

HA_ROLE=""
HA_CLUSTER="fleet-ha"
HA_NODE_NAME=""
HA_NODE_HOST=""
HA_MONITOR_HOST=""
HA_MONITOR_URL=""
HA_JOIN_PRIMARY_HOST=""
HA_INITIAL_PRIMARY=false
EXPECTED_CONFIG_FINGERPRINT=""
PGPORT="${PGPORT:-5432}"
HA_AUTH_METHOD="${HA_AUTH_METHOD:-trust}"
HA_SSL_SELF_SIGNED="${HA_SSL_SELF_SIGNED:-false}"

usage() {
  cat <<'EOF'
Usage:
  ha/install-ha-node.sh --role monitor --node-host HOST [--cluster NAME]
  ha/install-ha-node.sh --role data --node-host HOST --monitor-host HOST [--node-name NAME] [--initial-primary]
  ha/install-ha-node.sh --role data --node-host HOST --monitor-host HOST --join-primary-host HOST --expected-config-fingerprint HASH
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --role) HA_ROLE="${2:-}"; shift 2 ;;
    --cluster) HA_CLUSTER="${2:-}"; shift 2 ;;
    --node-name) HA_NODE_NAME="${2:-}"; shift 2 ;;
    --node-host) HA_NODE_HOST="${2:-}"; shift 2 ;;
    --monitor-host) HA_MONITOR_HOST="${2:-}"; shift 2 ;;
    --monitor-url) HA_MONITOR_URL="${2:-}"; shift 2 ;;
    --join-primary-host) HA_JOIN_PRIMARY_HOST="${2:-}"; shift 2 ;;
    --initial-primary) HA_INITIAL_PRIMARY=true; shift ;;
    --expected-config-fingerprint) EXPECTED_CONFIG_FINGERPRINT="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[ "$HA_ROLE" = "monitor" ] || [ "$HA_ROLE" = "data" ] || die "--role must be monitor or data"
[ -n "$HA_NODE_HOST" ] || die "--node-host is required"

if [ -z "$HA_NODE_NAME" ]; then
  HA_NODE_NAME="${HA_NODE_HOST%%.*}"
fi

if [ "$HA_ROLE" = "data" ]; then
  [ -n "$HA_MONITOR_HOST" ] || [ -n "$HA_MONITOR_URL" ] || die "--monitor-host or --monitor-url is required for data nodes"
  if [ -z "$HA_MONITOR_URL" ]; then
    HA_MONITOR_URL="postgres://autoctl_node@${HA_MONITOR_HOST}:${PGPORT}/pg_auto_failover"
  fi
  if [ -n "$HA_JOIN_PRIMARY_HOST" ] && [ -z "$EXPECTED_CONFIG_FINGERPRINT" ]; then
    die "--expected-config-fingerprint is required when joining a standby data node"
  fi
fi

"$script_dir/preflight.sh" --role "$HA_ROLE"

if [ "$HA_ROLE" = "data" ]; then
  current_fingerprint=$("$script_dir/config-fingerprint.sh")
  if [ -n "$EXPECTED_CONFIG_FINGERPRINT" ] && [ "$current_fingerprint" != "$EXPECTED_CONFIG_FINGERPRINT" ]; then
    die "config fingerprint mismatch: expected $EXPECTED_CONFIG_FINGERPRINT, got $current_fingerprint"
  fi
  log "config fingerprint: $current_fingerprint"
fi

image_tar="$project_root/images/timescaledb-ha.tar.gz"
if [ -f "$image_tar" ]; then
  log "loading HA TimescaleDB image from $image_tar"
  gzip -dc "$image_tar" | docker load
elif ! docker image inspect proto-fleet-timescaledb-ha:latest >/dev/null 2>&1; then
  die "proto-fleet-timescaledb-ha:latest is not loaded and $image_tar is missing"
fi

write_ha_env

case "$HA_ROLE" in
  monitor)
    log "starting HA monitor container"
    compose_with_env -f "$project_root/docker-compose.ha-monitor.yaml" up -d
    ;;
  data)
    log "starting HA data container"
    compose_with_env -f "$project_root/docker-compose.ha-data.yaml" up -d --wait --wait-timeout "${FLEET_HA_DB_WAIT_TIMEOUT:-180}"
    "$script_dir/install-systemd-follower.sh"
    ;;
esac

log "HA $HA_ROLE install completed"
