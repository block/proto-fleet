#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

json=false
if [ "${1:-}" = "--json" ]; then
  json=true
fi

load_env

PGDATA="${PGDATA:-/home/postgres/pgdata/data}"
DB_NAME="${DB_NAME:-fleet}"
DB_USERNAME="${DB_USERNAME:-fleet}"
host_ip="$(first_host_ip)"
api_url="http://${host_ip:-127.0.0.1}:4000"

db_role="unknown"
if docker ps --format '{{.Names}}' | grep -qx 'fleet-ha-db'; then
  if docker exec -u postgres fleet-ha-db psql -U "$DB_USERNAME" -d "$DB_NAME" -tAc "select pg_is_in_recovery();" 2>/dev/null | tr -d '[:space:]' | grep -qi '^f'; then
    db_role="primary"
  elif docker exec -u postgres fleet-ha-db psql -U "$DB_USERNAME" -d "$DB_NAME" -tAc "select pg_is_in_recovery();" 2>/dev/null | tr -d '[:space:]' | grep -qi '^t'; then
    db_role="standby"
  fi
fi

monitor_reachable="false"
cluster_state=""
if docker ps --format '{{.Names}}' | grep -qx 'fleet-ha-db'; then
  if cluster_state=$(docker exec -u postgres fleet-ha-db pg_autoctl show state --pgdata "$PGDATA" 2>/dev/null); then
    monitor_reachable="true"
  fi
elif docker ps --format '{{.Names}}' | grep -qx 'fleet-ha-monitor'; then
  if cluster_state=$(docker exec -u postgres fleet-ha-monitor pg_autoctl show state --pgdata "$PGDATA" 2>/dev/null); then
    monitor_reachable="true"
  fi
fi

fleet_state="stopped"
if compose_with_env -f "$project_root/docker-compose.ha.yaml" ps --status running -q fleet-api 2>/dev/null | grep -q .; then
  fleet_state="running"
fi

fleet_health="missing"
fleet_container_id=$(compose_with_env -f "$project_root/docker-compose.ha.yaml" ps -q fleet-api 2>/dev/null || true)
if [ -n "$fleet_container_id" ]; then
  fleet_health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$fleet_container_id" 2>/dev/null || printf 'unknown')
fi

fingerprint="unavailable"
if [ -f "$project_root/.env" ]; then
  fingerprint=$("$script_dir/config-fingerprint.sh" 2>/dev/null || printf 'unavailable')
fi

last_confirmed_primary=""
if [ -f "$ha_state_dir/last-confirmed-primary" ]; then
  last_confirmed_primary=$(cat "$ha_state_dir/last-confirmed-primary")
fi

if [ "$json" = "true" ]; then
  cat <<EOF
{"node_host":"${HA_NODE_HOST:-}","db_role":"$db_role","monitor_reachable":$monitor_reachable,"fleet_state":"$fleet_state","fleet_health":"$fleet_health","active_api_url":"$api_url","config_fingerprint":"$fingerprint","last_confirmed_primary":"$last_confirmed_primary"}
EOF
  exit 0
fi

cat <<EOF
Node host:              ${HA_NODE_HOST:-unknown}
Local DB role:          $db_role
Monitor reachable:      $monitor_reachable
Fleet state:            $fleet_state
Fleet health:           $fleet_health
Active API URL:         $api_url
Config fingerprint:     $fingerprint
Last confirmed primary: ${last_confirmed_primary:-none}

Cluster state:
${cluster_state:-unavailable}
EOF
