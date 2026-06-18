#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

load_env

PGDATA="${PGDATA:-/home/postgres/pgdata/data}"
DB_NAME="${DB_NAME:-fleet}"
DB_USERNAME="${DB_USERNAME:-fleet}"
LAST_PRIMARY_FILE="$ha_state_dir/last-confirmed-primary"

mkdir -p "$ha_state_dir"

fleet_running() {
  compose_with_env -f "$project_root/docker-compose.ha.yaml" ps --status running -q fleet-api | grep -q .
}

start_fleet() {
  "$script_dir/fleet-control.sh" start
}

stop_fleet() {
  "$script_dir/fleet-control.sh" stop
}

local_db_primary() {
  docker exec -u postgres fleet-ha-db \
    psql -U "$DB_USERNAME" -d "$DB_NAME" -tAc "select not pg_is_in_recovery();" 2>/dev/null \
    | tr -d '[:space:]' \
    | grep -qi '^t'
}

monitor_confirms_primary() {
  local state
  state=$(docker exec -u postgres fleet-ha-db pg_autoctl show state --pgdata "$PGDATA" 2>/dev/null || true)
  [ -n "$state" ] || return 1

  printf '%s\n' "$state" \
    | grep -F "${HA_NODE_NAME:-$HA_NODE_HOST}" \
    | grep -Eiq 'primary|single'
}

last_confirmed_this_host() {
  [ -f "$LAST_PRIMARY_FILE" ] || return 1
  [ "$(cat "$LAST_PRIMARY_FILE")" = "${HA_NODE_HOST:-}" ]
}

if local_db_primary; then
  if monitor_confirms_primary; then
    printf '%s' "$HA_NODE_HOST" > "$LAST_PRIMARY_FILE"
    start_fleet
    exit 0
  fi

  if fleet_running && last_confirmed_this_host; then
    log "monitor unavailable or inconclusive; keeping Fleet running on last confirmed primary"
    exit 0
  fi

  log "local DB is primary but monitor did not confirm; leaving Fleet stopped"
  stop_fleet
  exit 0
fi

log "local DB is not primary; stopping Fleet"
stop_fleet
