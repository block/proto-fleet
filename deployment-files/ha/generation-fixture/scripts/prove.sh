#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${FIXTURE_DIR}/../../.." && pwd)"
COMPOSE_FILE="${FIXTURE_DIR}/compose.yaml"
PROJECT_NAME="${HA_GENERATION_PROJECT_NAME:-proto-fleet-ha07-$$}"
NETWORK_NAME="${PROJECT_NAME}-network"
WAIT_SECONDS="${HA_GENERATION_WAIT_SECONDS:-180}"
PROBE_TIMEOUT_SECONDS="${HA_GENERATION_PROBE_TIMEOUT_SECONDS:-5}"
CLEAN_PROMOTIONS="${HA_GENERATION_CLEAN_PROMOTIONS:-2}"
KEEP_STACK="${HA_GENERATION_KEEP_STACK:-0}"
POSTGRES_PASSWORD="ha07-postgres"

for timeout_value in "${WAIT_SECONDS}" "${PROBE_TIMEOUT_SECONDS}"; do
  if [[ ! "${timeout_value}" =~ ^[1-9][0-9]*$ ]]; then
    echo "HA generation timeout values must be positive integers" >&2
    exit 2
  fi
done

case "${PROJECT_NAME}" in
  proto-fleet-ha07-*) ;;
  *)
    echo "HA_GENERATION_PROJECT_NAME must start with proto-fleet-ha07-" >&2
    exit 2
    ;;
esac

compose() {
  HA_GENERATION_PROJECT_NAME="${PROJECT_NAME}" \
    docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" "$@"
}

run_with_timeout() {
  local timeout_seconds="$1"
  shift
  python3 -c '
import subprocess
import sys

try:
    result = subprocess.run(sys.argv[2:], timeout=float(sys.argv[1]), check=False)
except subprocess.TimeoutExpired:
    raise SystemExit(124)
raise SystemExit(result.returncode)
' "${timeout_seconds}" "$@"
}

compose_timed() {
  local timeout_seconds="$1"
  shift
  run_with_timeout \
    "${timeout_seconds}" \
    env "HA_GENERATION_PROJECT_NAME=${PROJECT_NAME}" \
    docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" "$@"
}

probe_timeout_for_deadline() {
  local deadline="$1"
  local remaining=$((deadline - SECONDS))

  if ((remaining <= 0)); then
    return 1
  fi
  if ((remaining > PROBE_TIMEOUT_SECONDS)); then
    remaining="${PROBE_TIMEOUT_SECONDS}"
  fi
  printf '%s\n' "${remaining}"
}

log() {
  printf '[ha-07] %s\n' "$*" >&2
}

json_field() {
  local document="$1"
  local path="$2"
  printf '%s' "${document}" | python3 -c '
import json
import sys

value = json.load(sys.stdin)
for component in sys.argv[1].split("."):
    value = value[component]
print(value)
' "${path}"
}

other_patroni() {
  case "$1" in
    patroni-a) printf 'patroni-b\n' ;;
    patroni-b) printf 'patroni-a\n' ;;
    *)
      echo "unexpected Patroni member: $1" >&2
      return 1
      ;;
  esac
}

observe_from() {
  local service="$1"
  local timeout_seconds="${2:-${PROBE_TIMEOUT_SECONDS}}"
  local statement_timeout_ms=$((timeout_seconds * 1000))
  compose_timed \
    "${timeout_seconds}" \
    exec -T \
    -e "PGCONNECT_TIMEOUT=${timeout_seconds}" \
    -e "PGOPTIONS=-c statement_timeout=${statement_timeout_ms}" \
    "${service}" \
    /opt/ha07/observe_generation.py --timeout-seconds "${timeout_seconds}"
}

wait_for_observation() {
  local minimum_generation="${1:-0}"
  local expected_writer="${2:-}"
  local deadline=$((SECONDS + WAIT_SECONDS))
  local service observation generation writer probe_timeout last_error=""
  local services=(patroni-a patroni-b)

  if [[ -n "${expected_writer}" ]]; then
    services=("${expected_writer}")
  fi

  while ((SECONDS < deadline)); do
    for service in "${services[@]}"; do
      if ! probe_timeout="$(probe_timeout_for_deadline "${deadline}")"; then
        break 2
      fi
      if ! compose_timed \
        "${probe_timeout}" ps --status running -q "${service}" | grep -q .; then
        continue
      fi
      if ! probe_timeout="$(probe_timeout_for_deadline "${deadline}")"; then
        break 2
      fi
      if ! observation="$(observe_from "${service}" "${probe_timeout}" 2>&1)"; then
        last_error="${service}: ${observation}"
        continue
      fi
      generation="$(json_field "${observation}" writer_generation)"
      writer="$(json_field "${observation}" writer.name)"
      if ((generation <= minimum_generation)); then
        continue
      fi
      if [[ -n "${expected_writer}" && "${writer}" != "${expected_writer}" ]]; then
        continue
      fi
      printf '%s\n' "${observation}"
      return 0
    done
    sleep 1
  done

  log "timed out waiting for writer generation > ${minimum_generation}"
  if [[ -n "${last_error}" ]]; then
    log "last observer rejection: ${last_error}"
  fi
  compose_timed "${PROBE_TIMEOUT_SECONDS}" ps >&2 || true
  compose_timed \
    "${PROBE_TIMEOUT_SECONDS}" logs --tail=80 patroni-a patroni-b >&2 || true
  return 1
}

member_role() {
  local service="$1"
  local timeout_seconds="${2:-${PROBE_TIMEOUT_SECONDS}}"
  compose_timed "${timeout_seconds}" exec -T "${service}" \
    curl \
      --fail \
      --silent \
      --show-error \
      --connect-timeout "${timeout_seconds}" \
      --max-time "${timeout_seconds}" \
      http://127.0.0.1:8008/patroni \
    | python3 -c 'import json, sys; print(json.load(sys.stdin)["role"])'
}

wait_for_role() {
  local service="$1"
  local expected_role="$2"
  local deadline=$((SECONDS + WAIT_SECONDS))
  local role probe_timeout

  while ((SECONDS < deadline)); do
    if ! probe_timeout="$(probe_timeout_for_deadline "${deadline}")"; then
      break
    fi
    if role="$(member_role "${service}" "${probe_timeout}" 2>/dev/null)" \
      && [[ "${role}" == "${expected_role}" ]]; then
      return 0
    fi
    sleep 1
  done

  log "timed out waiting for ${service} to report role ${expected_role}"
  compose_timed "${PROBE_TIMEOUT_SECONDS}" logs --tail=80 "${service}" >&2 || true
  return 1
}

sql() {
  local service="$1"
  local query="$2"
  local timeout_seconds="${3:-${PROBE_TIMEOUT_SECONDS}}"
  local statement_timeout_ms=$((timeout_seconds * 1000))
  compose_timed \
    "${timeout_seconds}" \
    exec -T \
    -e "PGPASSWORD=${POSTGRES_PASSWORD}" \
    -e "PGCONNECT_TIMEOUT=${timeout_seconds}" \
    -e "PGOPTIONS=-c statement_timeout=${statement_timeout_ms}" \
    "${service}" \
    psql -h 127.0.0.1 -U postgres -d postgres -Atq -v ON_ERROR_STOP=1 -c "${query}"
}

wait_for_sql_value() {
  local service="$1"
  local query="$2"
  local expected="$3"
  local deadline=$((SECONDS + WAIT_SECONDS))
  local value probe_timeout

  while ((SECONDS < deadline)); do
    if ! probe_timeout="$(probe_timeout_for_deadline "${deadline}")"; then
      break
    fi
    if value="$(sql "${service}" "${query}" "${probe_timeout}" 2>/dev/null)" \
      && [[ "${value}" == "${expected}" ]]; then
      return 0
    fi
    sleep 1
  done

  log "timed out waiting for SQL value ${expected} on ${service}"
  return 1
}

wait_for_lease_renewal() {
  local initial_observation="$1"
  local writer generation lease_id minimum_ttl deadline
  local observation current_writer current_generation current_lease_id current_ttl
  local probe_timeout last_error=""

  writer="$(json_field "${initial_observation}" writer.name)"
  generation="$(json_field "${initial_observation}" writer_generation)"
  lease_id="$(json_field "${initial_observation}" dcs.leader_lease_id)"
  minimum_ttl="$(json_field "${initial_observation}" dcs.leader_lease_ttl)"
  deadline=$((SECONDS + WAIT_SECONDS))

  while ((SECONDS < deadline)); do
    if ! probe_timeout="$(probe_timeout_for_deadline "${deadline}")"; then
      break
    fi
    if ! observation="$(observe_from "${writer}" "${probe_timeout}" 2>&1)"; then
      last_error="${observation}"
      sleep 1
      continue
    fi

    current_writer="$(json_field "${observation}" writer.name)"
    current_generation="$(json_field "${observation}" writer_generation)"
    current_lease_id="$(json_field "${observation}" dcs.leader_lease_id)"
    current_ttl="$(json_field "${observation}" dcs.leader_lease_ttl)"
    if [[ "${current_writer}" != "${writer}" || "${current_generation}" != "${generation}" ]]; then
      log "writer or generation changed while waiting for lease renewal"
      return 1
    fi
    if [[ "${current_lease_id}" != "${lease_id}" ]]; then
      log "leader lease changed while waiting for renewal"
      return 1
    fi
    if ((current_ttl > minimum_ttl)); then
      log \
        "leader generation ${generation} stayed stable while lease ${lease_id}" \
        "TTL refreshed ${minimum_ttl} -> ${current_ttl}"
      printf '%s\n' "${observation}"
      return 0
    fi
    if ((current_ttl < minimum_ttl)); then
      minimum_ttl="${current_ttl}"
    fi
    sleep 1
  done

  log \
    "timed out waiting for ${writer} generation ${generation} lease ${lease_id}" \
    "TTL to refresh above ${minimum_ttl}"
  if [[ -n "${last_error}" ]]; then
    log "last observer rejection: ${last_error}"
  fi
  return 1
}

restore_as_replica() {
  local former_primary="$1"
  compose start "${former_primary}" >/dev/null
  wait_for_role "${former_primary}" replica
}

clean_promotion() {
  local observation="$1"
  local previous_generation primary replica next_observation next_generation
  previous_generation="$(json_field "${observation}" writer_generation)"
  primary="$(json_field "${observation}" writer.name)"
  replica="$(other_patroni "${primary}")"

  log "stopping ${primary} to promote ${replica}"
  compose stop "${primary}" >/dev/null
  next_observation="$(wait_for_observation "${previous_generation}" "${replica}")"
  next_generation="$(json_field "${next_observation}" writer_generation)"
  log "writer generation advanced ${previous_generation} -> ${next_generation}"
  restore_as_replica "${primary}"
  printf '%s\n' "${next_observation}"
}

prove_async_write_loss() {
  local observation="$1"
  local previous_generation primary replica next_observation next_generation marker_count
  previous_generation="$(json_field "${observation}" writer_generation)"
  primary="$(json_field "${observation}" writer.name)"
  replica="$(other_patroni "${primary}")"

  log "preparing an acknowledged write that cannot reach ${replica}"
  sql "${primary}" \
    "CREATE TABLE IF NOT EXISTS ha07_async_loss_probe (marker text PRIMARY KEY); TRUNCATE ha07_async_loss_probe; SELECT pg_switch_wal();" \
    >/dev/null
  wait_for_sql_value \
    "${replica}" \
    "SELECT (to_regclass('public.ha07_async_loss_probe') IS NOT NULL)::text;" \
    "true"

  compose stop "${replica}" >/dev/null
  sql "${primary}" "INSERT INTO ha07_async_loss_probe(marker) VALUES ('acknowledged-lost-write');" >/dev/null
  wait_for_sql_value \
    "${primary}" \
    "SELECT count(*)::text FROM ha07_async_loss_probe WHERE marker = 'acknowledged-lost-write';" \
    "1"

  log "stopping ${primary}; ${replica} must promote without the acknowledged row"
  compose stop "${primary}" >/dev/null
  compose start "${replica}" >/dev/null
  next_observation="$(wait_for_observation "${previous_generation}" "${replica}")"
  next_generation="$(json_field "${next_observation}" writer_generation)"
  marker_count="$(sql "${replica}" \
    "SELECT count(*)::text FROM ha07_async_loss_probe WHERE marker = 'acknowledged-lost-write';")"
  if [[ "${marker_count}" != "0" ]]; then
    log "the acknowledged marker unexpectedly replicated; write-loss proof is invalid"
    return 1
  fi
  log "write was lost while generation advanced ${previous_generation} -> ${next_generation}"

  restore_as_replica "${primary}"
  wait_for_sql_value \
    "${primary}" \
    "SELECT count(*)::text FROM ha07_async_loss_probe WHERE marker = 'acknowledged-lost-write';" \
    "0"
  printf '%s\n' "${next_observation}"
}

prove_partition_and_rejoin() {
  local observation="$1"
  local previous_generation primary replica container_id next_observation next_generation
  previous_generation="$(json_field "${observation}" writer_generation)"
  primary="$(json_field "${observation}" writer.name)"
  replica="$(other_patroni "${primary}")"
  container_id="$(compose ps -q "${primary}")"

  log "disconnecting ${primary} from ${NETWORK_NAME}"
  docker network disconnect --force "${NETWORK_NAME}" "${container_id}"
  next_observation="$(wait_for_observation "${previous_generation}" "${replica}")"
  next_generation="$(json_field "${next_observation}" writer_generation)"
  log "partition promoted ${replica}; generation advanced ${previous_generation} -> ${next_generation}"

  docker network connect --alias "${primary}" "${NETWORK_NAME}" "${container_id}"
  wait_for_role "${primary}" replica

  local stable_observation stable_generation stable_writer
  stable_observation="$(observe_from "${replica}")"
  stable_generation="$(json_field "${stable_observation}" writer_generation)"
  stable_writer="$(json_field "${stable_observation}" writer.name)"
  if [[ "${stable_generation}" != "${next_generation}" || "${stable_writer}" != "${replica}" ]]; then
    log "old primary rejoin changed the authoritative writer term"
    return 1
  fi
  printf '%s\n' "${stable_observation}"
}

cleanup() {
  local exit_code=$?
  trap - EXIT

  if [[ "${KEEP_STACK}" == "1" && "${exit_code}" -ne 0 ]]; then
    log "retaining ${PROJECT_NAME} for inspection"
  else
    if ! compose_timed \
      "${WAIT_SECONDS}" down --volumes --remove-orphans >/dev/null; then
      log "failed to tear down disposable fixture ${PROJECT_NAME}"
      if [[ "${exit_code}" -eq 0 ]]; then
        exit_code=1
      fi
    fi
  fi
  exit "${exit_code}"
}

run_proof() {
  local observation initial_generation iteration

  trap cleanup EXIT

  log "building the repository TimescaleDB base"
  docker build -t proto-fleet-timescaledb:ha07 "${REPO_ROOT}/server/timescaledb"
  compose build patroni-a

  log "starting the disposable two-Postgres, three-etcd fixture"
  compose up -d --wait --wait-timeout "${WAIT_SECONDS}"
  observation="$(wait_for_observation)"
  initial_generation="$(json_field "${observation}" writer_generation)"
  log "initial writer $(json_field "${observation}" writer.name) uses generation ${initial_generation}"

  observation="$(wait_for_lease_renewal "${observation}")"

  for ((iteration = 1; iteration <= CLEAN_PROMOTIONS; iteration++)); do
    log "clean promotion ${iteration}/${CLEAN_PROMOTIONS}"
    observation="$(clean_promotion "${observation}")"
  done

  observation="$(prove_async_write_loss "${observation}")"
  observation="$(prove_partition_and_rejoin "${observation}")"

  log "HA-07 PASS"
  printf '%s\n' "${observation}"
}

case "${1:-run}" in
  run)
    run_proof
    ;;
  unit)
    python3 -m unittest "${FIXTURE_DIR}/tests/test_observe_generation.py"
    ;;
  config)
    compose config
    ;;
  *)
    echo "usage: $0 [run|unit|config]" >&2
    exit 2
    ;;
esac
