#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
poc_dir="$(cd "${script_dir}/.." && pwd)"
env_file="${HA_POC_ENV:-${poc_dir}/.env}"
if [[ "$#" -gt 0 && -f "${1}" ]]; then
  env_file="$1"
  shift
fi

usage() {
  cat <<'EOF'
Usage:
  ./scripts/demo.sh [env-file] snapshot
  ./scripts/demo.sh [env-file] loop [seconds]
  ./scripts/demo.sh [env-file] app-failover [--yes]
  ./scripts/demo.sh [env-file] db-failover [--yes]
  ./scripts/demo.sh [env-file] talk-track

Commands:
  snapshot      Show a presenter-friendly one-screen state summary.
  loop          Refresh the summary until Ctrl-C. Default interval: 2s.
  app-failover  Stop the local active fake Fleet app, then time VIP takeover.
  db-failover   Stop the local Patroni primary, then time DB promotion.
  talk-track    Print a short narration and terminal layout for the demo.
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    die "${cmd} is required"
  fi
}

cmd="${1:-snapshot}"
case "${cmd}" in
  -h | --help | help)
    usage
    exit 0
    ;;
esac

if [[ ! -f "${env_file}" ]]; then
  die "env file not found: ${env_file}"
fi

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

: "${HA_POC_PORT_MODE:=standard}"
case "${HA_POC_PORT_MODE}" in
  coexist)
    : "${HA_ETCD_CLIENT_PORT:=12379}"
    : "${HA_ETCD_PEER_PORT:=12380}"
    : "${HA_POSTGRES_PORT:=15432}"
    : "${HA_PATRONI_PORT:=18008}"
    : "${HA_FAKE_FLEET_PORT:=14080}"
    ;;
  standard)
    : "${HA_ETCD_CLIENT_PORT:=2379}"
    : "${HA_ETCD_PEER_PORT:=2380}"
    : "${HA_POSTGRES_PORT:=5432}"
    : "${HA_PATRONI_PORT:=8008}"
    : "${HA_FAKE_FLEET_PORT:=4080}"
    ;;
  *)
    die "HA_POC_PORT_MODE must be coexist or standard; got ${HA_POC_PORT_MODE}"
    ;;
esac

require_cmd curl
require_cmd jq

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  bold=$'\033[1m'
  dim=$'\033[2m'
  green=$'\033[32m'
  yellow=$'\033[33m'
  red=$'\033[31m'
  blue=$'\033[34m'
  reset=$'\033[0m'
else
  bold=""
  dim=""
  green=""
  yellow=""
  red=""
  blue=""
  reset=""
fi

http_code=""
http_body=""

http_get() {
  local url="$1"
  local timeout="${2:-1}"
  local tmp
  local curl_args=(-sS --max-time "${timeout}" -w '%{http_code}' -o)
  tmp="$(mktemp)"
  if [[ -n "${HA_POC_STATUS_TOKEN:-}" ]]; then
    curl_args+=("${tmp}" -H "Authorization: Bearer ${HA_POC_STATUS_TOKEN}" "${url}")
  else
    curl_args+=("${tmp}" "${url}")
  fi
  http_code="$(curl "${curl_args[@]}" 2>/dev/null || true)"
  http_body="$(cat "${tmp}")"
  rm -f "${tmp}"
  if [[ ! "${http_code}" =~ ^[0-9][0-9][0-9]$ ]]; then
    http_code="000"
  fi
}

json_value() {
  local expr="$1"
  jq -r "${expr}" 2>/dev/null <<<"${http_body}" || true
}

short_holder() {
  local holder="$1"
  if [[ "${#holder}" -le 24 ]]; then
    printf '%s' "${holder}"
  else
    printf '%s...' "${holder:0:21}"
  fi
}

now_ms() {
  date +%s%3N
}

compose() {
  docker compose --env-file "${env_file}" -f "${poc_dir}/docker-compose.fleet.yaml" "$@"
}

print_header() {
  printf '%sProto Fleet HA POC%s  %s%s%s\n' "${bold}" "${reset}" "${dim}" "$(date -u '+%Y-%m-%d %H:%M:%SZ')" "${reset}"
  printf 'fleet-a=%s  fleet-b=%s  witness=%s  VIP=%s  http=%s\n' \
    "${HA_FLEET_A_IP}" "${HA_FLEET_B_IP}" "${HA_WITNESS_IP}" "${HA_VIP:-unset}" "${HA_FAKE_FLEET_PORT}"
  printf '%sModel:%s Patroni picks the writable DB. Fleet DB lease picks the active app. keepalived moves the VIP to that active app.\n\n' \
    "${blue}" "${reset}"
}

print_app_row() {
  local label="$1"
  local ip="$2"
  local code state state_color host holder epoch renew heartbeat detail

  http_get "http://${ip}:${HA_FAKE_FLEET_PORT}/health/active"
  code="${http_code}"
  host="$(json_value '.host_id // "-"')"
  holder="$(json_value '.holder_id // "-"')"
  epoch="$(json_value '.lease_epoch // "-"')"
  renew="$(json_value '.last_renew_at // "-"')"
  heartbeat="$(json_value '.last_heartbeat_at // "-"')"

  if [[ "${code}" == "200" ]]; then
    state="ACTIVE"
    state_color="${green}"
  elif [[ "${code}" == "503" ]]; then
    state="passive"
    state_color="${yellow}"
  else
    state="down"
    state_color="${red}"
    host="-"
    holder="-"
    epoch="-"
    renew="-"
    heartbeat="-"
  fi

  detail="holder=$(short_holder "${holder}") renew=${renew} heartbeat=${heartbeat}"
  printf '%-12s %s%-8s%s %-8s %-6s %s\n' "${label}" "${state_color}" "${state}" "${reset}" "${host}" "${epoch}" "${detail}"
}

print_vip_row() {
  local host holder epoch detail
  if [[ -z "${HA_VIP:-}" ]]; then
    printf '%-12s %s%-8s%s %-8s %-6s %s\n' "VIP" "${red}" "unset" "${reset}" "-" "-" "-"
    return
  fi

  http_get "http://${HA_VIP}:${HA_FAKE_FLEET_PORT}/health/active" 2
  if [[ "${http_code}" != "200" ]]; then
    printf '%-12s %s%-8s%s %-8s %-6s %s\n' "VIP" "${red}" "down" "${reset}" "-" "-" "http_code=${http_code}"
    return
  fi

  host="$(json_value '.host_id // "-"')"
  holder="$(json_value '.holder_id // "-"')"
  epoch="$(json_value '.lease_epoch // "-"')"
  detail="routes_to=${host} holder=$(short_holder "${holder}")"
  printf '%-12s %s%-8s%s %-8s %-6s %s\n' "VIP" "${green}" "OK" "${reset}" "${host}" "${epoch}" "${detail}"
}

print_patroni_row() {
  local label="$1"
  local ip="$2"
  local role state timeline detail color

  http_get "http://${ip}:${HA_PATRONI_PORT}/patroni" 2
  if [[ "${http_code}" != "200" ]]; then
    printf '%-12s %s%-8s%s %-10s %-8s %s\n' "${label}" "${red}" "down" "${reset}" "-" "-" "http_code=${http_code}"
    return
  fi

  role="$(json_value '.role // "-"')"
  state="$(json_value '.state // "-"')"
  timeline="$(json_value '.timeline // "-"')"
  if [[ "${role}" == "primary" ]]; then
    color="${green}"
  elif [[ "${role}" == "replica" ]]; then
    color="${yellow}"
  else
    color="${red}"
  fi
  detail="state=${state} timeline=${timeline}"
  if [[ "${role}" == "replica" ]]; then
    detail="${detail} replication=$(json_value '.replication_state // "unknown"')"
  fi
  printf '%-12s %s%-8s%s %-10s %-8s %s\n' "${label}" "${color}" "${role}" "${reset}" "${state}" "tl=${timeline}" "${detail}"
}

snapshot() {
  print_header
  printf '%sApplication lease and VIP%s\n' "${bold}" "${reset}"
  printf '%-12s %-8s %-8s %-6s %s\n' "endpoint" "state" "host" "epoch" "details"
  print_app_row "fleet-a" "${HA_FLEET_A_IP}"
  print_app_row "fleet-b" "${HA_FLEET_B_IP}"
  print_vip_row
  printf '\n%sPostgres / Patroni%s\n' "${bold}" "${reset}"
  printf '%-12s %-8s %-10s %-8s %s\n' "endpoint" "role" "state" "timeline" "details"
  print_patroni_row "fleet-a" "${HA_FLEET_A_IP}"
  print_patroni_row "fleet-b" "${HA_FLEET_B_IP}"
  printf '\n%sAudience check:%s one ACTIVE app, VIP routes to it, and Patroni has one primary plus one replica.\n' "${blue}" "${reset}"
}

loop_snapshot() {
  local interval="${1:-2}"
  while true; do
    clear
    snapshot
    printf '\nRefreshing every %ss. Press Ctrl-C to stop.\n' "${interval}"
    sleep "${interval}"
  done
}

confirm() {
  local yes="$1"
  local prompt="$2"
  if [[ "${yes}" == "yes" ]]; then
    return 0
  fi
  printf '%s Type "yes" to continue: ' "${prompt}"
  local answer
  read -r answer
  [[ "${answer}" == "yes" ]] || die "cancelled"
}

active_vip_host() {
  http_get "http://${HA_VIP}:${HA_FAKE_FLEET_PORT}/health/active" 2
  [[ "${http_code}" == "200" ]] || return 1
  json_value '.host_id // empty'
}

primary_host() {
  http_get "http://${HA_FLEET_A_IP}:${HA_PATRONI_PORT}/patroni" 1
  if [[ "${http_code}" == "200" && "$(json_value '.role // empty')" == "primary" ]]; then
    echo "fleet-a"
    return 0
  fi
  http_get "http://${HA_FLEET_B_IP}:${HA_PATRONI_PORT}/patroni" 1
  if [[ "${http_code}" == "200" && "$(json_value '.role // empty')" == "primary" ]]; then
    echo "fleet-b"
    return 0
  fi
  return 1
}

require_fleet_host() {
  case "${HA_NODE_NAME:-}" in
    fleet-a | fleet-b) ;;
    *) die "run this on fleet-a or fleet-b, not ${HA_NODE_NAME:-unknown}" ;;
  esac
}

demo_app_failover() {
  local yes="${1:-no}"
  require_fleet_host
  require_cmd docker

  local before_host
  before_host="$(active_vip_host || true)"
  [[ -n "${before_host}" ]] || die "VIP active endpoint is not healthy before failover"
  if [[ "${before_host}" != "${HA_NODE_NAME}" ]]; then
    die "run app-failover on the active host (${before_host}); this host is ${HA_NODE_NAME}"
  fi

  snapshot
  printf '\nThis demo stops fake-fleet on %s. The peer should take the DB lease and the VIP.\n' "${HA_NODE_NAME}"
  printf 'Restore afterwards on this host with: ./scripts/pi-poc.sh restore\n'
  confirm "${yes}" "Stop local fake-fleet now?"

  local start_ms now after_host epoch elapsed_ms deadline
  start_ms="$(now_ms)"
  compose stop fake-fleet >/dev/null
  deadline=$((start_ms + 60000))

  printf 'Waiting for VIP to move away from %s...\n' "${before_host}"
  while true; do
    after_host="$(active_vip_host || true)"
    if [[ -n "${after_host}" && "${after_host}" != "${before_host}" ]]; then
      now="$(now_ms)"
      elapsed_ms=$((now - start_ms))
      epoch="$(json_value '.lease_epoch // "unknown"')"
      printf '\n%sApp failover complete%s\n' "${green}" "${reset}"
      printf 'before=%s after=%s lease_epoch=%s failover_ms=%s\n\n' "${before_host}" "${after_host}" "${epoch}" "${elapsed_ms}"
      snapshot
      return 0
    fi
    now="$(now_ms)"
    if ((now > deadline)); then
      die "timed out waiting for app failover"
    fi
    sleep 0.2
  done
}

demo_db_failover() {
  local yes="${1:-no}"
  require_fleet_host
  require_cmd docker

  http_get "http://${HA_NODE_IP}:${HA_PATRONI_PORT}/patroni" 2
  [[ "${http_code}" == "200" ]] || die "local Patroni API is not healthy at ${HA_NODE_IP}:${HA_PATRONI_PORT}"
  local local_role
  local_role="$(json_value '.role // empty')"
  if [[ "${local_role}" != "primary" ]]; then
    local current_primary
    current_primary="$(primary_host || true)"
    die "run db-failover on the current DB primary (${current_primary:-unknown}); this host is ${HA_NODE_NAME} role=${local_role}"
  fi

  snapshot
  printf '\nThis demo stops Patroni/Postgres on %s. The replica should promote, and the app should reconnect through the multi-host DSN.\n' "${HA_NODE_NAME}"
  printf 'Restore afterwards on this host with: ./scripts/pi-poc.sh restore\n'
  confirm "${yes}" "Stop local Patroni primary now?"

  local before_primary start_ms now after_primary elapsed_ms deadline
  before_primary="${HA_NODE_NAME}"
  start_ms="$(now_ms)"
  compose stop patroni >/dev/null
  deadline=$((start_ms + 90000))

  printf 'Waiting for Patroni to promote the peer...\n'
  while true; do
    after_primary="$(primary_host || true)"
    if [[ -n "${after_primary}" && "${after_primary}" != "${before_primary}" ]]; then
      now="$(now_ms)"
      elapsed_ms=$((now - start_ms))
      printf '\n%sDB failover complete%s\n' "${green}" "${reset}"
      printf 'before_primary=%s after_primary=%s failover_ms=%s\n\n' "${before_primary}" "${after_primary}" "${elapsed_ms}"
      snapshot
      return 0
    fi
    now="$(now_ms)"
    if ((now > deadline)); then
      die "timed out waiting for DB failover"
    fi
    sleep 0.5
  done
}

talk_track() {
  cat <<EOF
Terminal layout:
  1. On either Fleet host:
       cd ~/proto-fleet-ha-poc/deployment-files/ha-poc
       ./scripts/pi-poc.sh demo loop 2

  2. On the current active app host:
       ./scripts/pi-poc.sh demo app-failover

  3. After app failover, restore the stopped app:
       ./scripts/pi-poc.sh restore

  4. On the current DB primary host:
       ./scripts/pi-poc.sh demo db-failover

  5. After DB failover, restore the stopped DB:
       ./scripts/pi-poc.sh restore

Narration:
  - Patroni owns database primary election.
  - Fleet owns a database-backed active lease.
  - keepalived owns the LAN VIP and follows /health/active.
  - The key invariant is exactly one ACTIVE app, and the VIP points at it.
  - App failover proves the peer takes the lease and VIP.
  - DB failover proves Patroni promotes the replica and the app reconnects to the writable DB.
EOF
}

shift || true

case "${cmd}" in
  snapshot) snapshot ;;
  loop) loop_snapshot "$@" ;;
  app-failover)
    if [[ "${1:-}" == "--yes" ]]; then
      demo_app_failover yes
    else
      demo_app_failover no
    fi
    ;;
  db-failover)
    if [[ "${1:-}" == "--yes" ]]; then
      demo_db_failover yes
    else
      demo_db_failover no
    fi
    ;;
  talk-track) talk_track ;;
  -h | --help | help) usage ;;
  *) usage; exit 2 ;;
esac
