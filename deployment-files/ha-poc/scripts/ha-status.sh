#!/usr/bin/env bash
set -euo pipefail

env_file="${1:-.env}"
if [[ ! -f "${env_file}" ]]; then
  echo "env file not found: ${env_file}" >&2
  exit 2
fi

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

auth_header=()
if [[ -n "${HA_POC_STATUS_TOKEN:-}" ]]; then
  auth_header=(-H "Authorization: Bearer ${HA_POC_STATUS_TOKEN}")
fi

check_url() {
  local label="$1"
  local url="$2"
  echo "== ${label}: ${url}"
  if curl -fsS --max-time 2 "${auth_header[@]}" "${url}"; then
    echo
  else
    echo "unreachable"
  fi
}

check_url "fleet-a active" "http://${HA_FLEET_A_IP}:4080/health/active"
check_url "fleet-b active" "http://${HA_FLEET_B_IP}:4080/health/active"

if [[ -n "${HA_VIP:-}" ]]; then
  check_url "vip active" "http://${HA_VIP}:4080/health/active"
fi

check_url "fleet-a patroni" "http://${HA_FLEET_A_IP}:8008/patroni"
check_url "fleet-b patroni" "http://${HA_FLEET_B_IP}:8008/patroni"
check_url "fleet-a cluster" "http://${HA_FLEET_A_IP}:8008/cluster"
