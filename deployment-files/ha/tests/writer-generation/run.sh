#!/usr/bin/env bash
set -euo pipefail

fixture_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${fixture_dir}/../../../.." && pwd -P)"
compose_file="${fixture_dir}/compose.yaml"
project_name="proto-fleet-ha-writer-$RANDOM-$$"

export COMPOSE_PROJECT_NAME="${project_name}"
export HA_WRITER_BASE_IMAGE="proto-fleet-timescaledb:ha-writer-test"
export HA_WRITER_PATRONI_IMAGE="${project_name}-patroni:local"

compose() {
  docker compose --project-directory "${fixture_dir}" -f "${compose_file}" "$@"
}

cleanup() {
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
}

handle_interrupt() {
  exit 130
}

handle_terminate() {
  exit 143
}

trap cleanup EXIT
trap handle_interrupt INT
trap handle_terminate TERM

observe() {
  local minimum="${1:-}"
  local -a environment=(
    HA_WRITER_FIXTURE=1
  )
  if [[ -n "${minimum}" ]]; then
    environment+=("HA_WRITER_MIN_GENERATION=${minimum}")
  fi
  compose exec -T patroni-a env "${environment[@]}" \
    /opt/ha/ha-observer.test -test.v -test.run '^TestWriterObserverFixture$'
}

extract_field() {
  local field="$1"
  sed -n "s/.*${field}=\\([^[:space:]]*\\).*/\\1/p" | tail -n 1
}

docker build \
  --tag "${HA_WRITER_BASE_IMAGE}" \
  --file "${repo_root}/server/timescaledb/Dockerfile" \
  "${repo_root}/server/timescaledb"

compose config >/dev/null
compose build patroni-a
compose up --detach --wait --wait-timeout 180

first_output="$(observe)"
printf '%s\n' "${first_output}"
first_generation="$(printf '%s\n' "${first_output}" | extract_field writer_generation)"
first_leader="$(printf '%s\n' "${first_output}" | extract_field leader)"
if [[ -z "${first_generation}" || -z "${first_leader}" ]]; then
  echo "failed to parse the initial production observer result" >&2
  exit 1
fi

if [[ "${first_leader}" == "patroni-a" ]]; then
  candidate="patroni-b"
else
  candidate="patroni-a"
fi
payload="$(printf '{"leader":"%s","candidate":"%s"}' "${first_leader}" "${candidate}")"
compose exec -T patroni-a curl --fail --silent --show-error \
  --connect-timeout 5 \
  --max-time 15 \
  --request POST \
  --header 'Content-Type: application/json' \
  --data "${payload}" \
  "http://${first_leader}:8008/switchover" >/dev/null

for attempt in {1..30}; do
  if promoted_output="$(observe "${first_generation}" 2>&1)"; then
    printf '%s\n' "${promoted_output}"
    promoted_generation="$(
      printf '%s\n' "${promoted_output}" | extract_field writer_generation
    )"
    if [[ -n "${promoted_generation}" ]]; then
      echo "writer generation advanced: ${first_generation} -> ${promoted_generation}"
      exit 0
    fi
  fi
  sleep 1
done

compose logs patroni-a patroni-b >&2
echo "production observer did not validate a higher generation after switchover" >&2
exit 1
