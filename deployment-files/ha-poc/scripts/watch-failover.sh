#!/usr/bin/env bash
set -euo pipefail

env_file="${1:-.env}"
url_override="${2:-}"
if [[ ! -f "${env_file}" ]]; then
  echo "env file not found: ${env_file}" >&2
  exit 2
fi

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

: "${HA_FAKE_FLEET_PORT:=4080}"

target_url="${url_override:-http://${HA_VIP}:${HA_FAKE_FLEET_PORT}/health/active}"

holder_id() {
  curl -fsS --max-time 1 "${target_url}" 2>/dev/null \
    | sed -n 's/.*"holder_id":"\([^"]*\)".*/\1/p'
}

lease_epoch() {
  curl -fsS --max-time 1 "${target_url}" 2>/dev/null \
    | sed -n 's/.*"lease_epoch":\([0-9][0-9]*\).*/\1/p'
}

before="$(holder_id || true)"
if [[ -z "${before}" ]]; then
  echo "active endpoint is not healthy before failover: ${target_url}" >&2
  exit 1
fi
before_epoch="$(lease_epoch || true)"

echo "watching ${target_url}"
echo "current holder: ${before} epoch=${before_epoch:-unknown}"
echo "trigger the failure in another terminal now"

start_ms=""
while true; do
  current="$(holder_id || true)"
  if [[ -z "${current}" ]]; then
    if [[ -z "${start_ms}" ]]; then
      start_ms="$(date +%s%3N)"
      echo "active endpoint went unhealthy; timing recovery"
    fi
    sleep 0.2
    continue
  fi

  if [[ -n "${start_ms}" ]]; then
    end_ms="$(date +%s%3N)"
    elapsed_ms=$((end_ms - start_ms))
    current_epoch="$(lease_epoch || true)"
    echo "new holder: ${current}"
    echo "new epoch: ${current_epoch:-unknown}"
    if [[ "${current}" == "${before}" ]]; then
      echo "holder unchanged; endpoint recovered without app lease takeover"
    fi
    echo "failover_ms: ${elapsed_ms}"
    exit 0
  fi
  sleep 0.2
done
