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
LAST_FLEET_RESTART_FILE="$ha_state_dir/last-fleet-restart-at"
LAST_APP_FAILOVER_FILE="$ha_state_dir/last-app-failover-at"
FLEET_HA_HEALTH_URL="${FLEET_HA_HEALTH_URL:-http://127.0.0.1:4000/health}"
FLEET_HA_HEALTH_TIMEOUT="${FLEET_HA_HEALTH_TIMEOUT:-3}"
FLEET_HA_RESTART_WAIT_SECONDS="${FLEET_HA_RESTART_WAIT_SECONDS:-20}"
FLEET_HA_APP_FAILOVER_COOLDOWN_SECONDS="${FLEET_HA_APP_FAILOVER_COOLDOWN_SECONDS:-300}"
FLEET_HA_APP_FAILOVER_ENABLED="${FLEET_HA_APP_FAILOVER_ENABLED:-true}"

mkdir -p "$ha_state_dir"

fleet_running() {
  compose_with_env -f "$project_root/docker-compose.ha.yaml" ps --status running -q fleet-api | grep -q .
}

fleet_api_container_id() {
  compose_with_env -f "$project_root/docker-compose.ha.yaml" ps -q fleet-api 2>/dev/null || true
}

fleet_health_status() {
  local container_id status
  container_id=$(fleet_api_container_id)
  if [ -z "$container_id" ]; then
    printf 'missing'
    return 0
  fi

  status=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null || true)
  printf '%s' "${status:-unknown}"
}

http_health_probe() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsS --max-time "$FLEET_HA_HEALTH_TIMEOUT" "$FLEET_HA_HEALTH_URL" >/dev/null
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -q --timeout="$FLEET_HA_HEALTH_TIMEOUT" --tries=1 -O /dev/null "$FLEET_HA_HEALTH_URL"
    return
  fi

  return 1
}

fleet_healthy() {
  local status
  status=$(fleet_health_status)
  case "$status" in
    healthy)
      return 0
      ;;
    running)
      http_health_probe
      return
      ;;
    *)
      return 1
      ;;
  esac
}

start_fleet() {
  "$script_dir/fleet-control.sh" start
}

stop_fleet() {
  "$script_dir/fleet-control.sh" stop
}

restart_fleet() {
  "$script_dir/fleet-control.sh" restart
}

now_seconds() {
  date +%s
}

marker_within_cooldown() {
  local marker_file="$1"
  local cooldown_seconds="$2"
  local now last

  [ -f "$marker_file" ] || return 1
  now=$(now_seconds)
  last=$(cat "$marker_file" 2>/dev/null || printf '0')
  [ "$last" -gt 0 ] 2>/dev/null || return 1
  [ $((now - last)) -lt "$cooldown_seconds" ]
}

wait_for_fleet_health() {
  local deadline
  deadline=$(( $(now_seconds) + FLEET_HA_RESTART_WAIT_SECONDS ))

  while [ "$(now_seconds)" -le "$deadline" ]; do
    if fleet_healthy; then
      return 0
    fi
    sleep 2
  done

  return 1
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

app_failover_enabled() {
  case "$FLEET_HA_APP_FAILOVER_ENABLED" in
    true|TRUE|1|yes|YES)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

trigger_app_failover() {
  if ! app_failover_enabled; then
    log "Fleet is unhealthy after restart; app-triggered failover is disabled"
    return 1
  fi

  if marker_within_cooldown "$LAST_APP_FAILOVER_FILE" "$FLEET_HA_APP_FAILOVER_COOLDOWN_SECONDS"; then
    log "Fleet is unhealthy after restart; app-triggered failover is in cooldown"
    return 1
  fi

  if ! monitor_confirms_primary; then
    log "Fleet is unhealthy after restart, but monitor no longer confirms this node as primary; not stopping DB"
    return 1
  fi

  log "Fleet is unhealthy after restart; stopping local Fleet and DB to trigger pg_auto_failover promotion"
  stop_fleet || true
  if docker stop -t "${FLEET_HA_DB_STOP_TIMEOUT:-10}" fleet-ha-db; then
    now_seconds > "$LAST_APP_FAILOVER_FILE"
    rm -f "$LAST_FLEET_RESTART_FILE"
  else
    log "failed to stop local DB container; app-triggered failover was not started"
    return 1
  fi
}

watchdog_fleet() {
  local status

  if ! fleet_running; then
    log "local DB is primary; starting Fleet"
    if ! start_fleet; then
      log "Fleet start did not complete cleanly; will re-check on the next follower run"
    fi
    return 0
  fi

  if fleet_healthy; then
    rm -f "$LAST_FLEET_RESTART_FILE"
    return 0
  fi

  status=$(fleet_health_status)
  if [ "$status" = "starting" ]; then
    log "Fleet healthcheck is still starting"
    return 0
  fi

  if [ -f "$LAST_FLEET_RESTART_FILE" ]; then
    log "Fleet is still unhealthy after a watchdog restart"
    trigger_app_failover
    return 0
  fi

  log "Fleet health is $status; restarting Fleet before failover"
  now_seconds > "$LAST_FLEET_RESTART_FILE"
  restart_fleet || true

  if wait_for_fleet_health; then
    log "Fleet recovered after watchdog restart"
    rm -f "$LAST_FLEET_RESTART_FILE"
    return 0
  fi

  trigger_app_failover
}

if local_db_primary; then
  if monitor_confirms_primary; then
    printf '%s' "$HA_NODE_HOST" > "$LAST_PRIMARY_FILE"
    watchdog_fleet
    exit 0
  fi

  if fleet_running && last_confirmed_this_host; then
    log "monitor unavailable or inconclusive; keeping Fleet running on last confirmed primary"
    watchdog_fleet
    exit 0
  fi

  log "local DB is primary but monitor did not confirm; leaving Fleet stopped"
  rm -f "$LAST_FLEET_RESTART_FILE"
  stop_fleet
  exit 0
fi

log "local DB is not primary; stopping Fleet"
rm -f "$LAST_FLEET_RESTART_FILE"
stop_fleet
