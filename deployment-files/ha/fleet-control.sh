#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

usage() {
  cat <<'EOF'
Usage: ha/fleet-control.sh start|stop|restart|status
EOF
}

action="${1:-}"
[ -n "$action" ] || { usage; exit 1; }

compose_args=(-f "$project_root/docker-compose.ha.yaml")

case "$action" in
  start)
    compose_with_env "${compose_args[@]}" up -d --wait --wait-timeout "${FLEET_HA_WAIT_TIMEOUT:-120}" fleet-api fleet-client
    ;;
  stop)
    compose_with_env "${compose_args[@]}" stop fleet-api fleet-client
    ;;
  restart)
    compose_with_env "${compose_args[@]}" restart fleet-api fleet-client
    ;;
  status)
    compose_with_env "${compose_args[@]}" ps fleet-api fleet-client
    ;;
  *)
    usage
    exit 1
    ;;
esac
