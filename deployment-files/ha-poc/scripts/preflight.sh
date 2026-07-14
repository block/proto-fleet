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

: "${HA_ETCD_CLIENT_PORT:=2379}"
: "${HA_ETCD_PEER_PORT:=2380}"
: "${HA_POSTGRES_PORT:=5432}"
: "${HA_PATRONI_PORT:=8008}"
: "${HA_FAKE_FLEET_PORT:=4080}"

require() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 2
  fi
}

for name in HA_NODE_NAME HA_NODE_IP HA_FLEET_A_IP HA_FLEET_B_IP HA_WITNESS_IP; do
  require "${name}"
done

if ! command -v nc >/dev/null 2>&1; then
  echo "nc is required for TCP preflight checks" >&2
  exit 2
fi

check_tcp() {
  local host="$1"
  local port="$2"
  if nc -z -w 2 "${host}" "${port}" >/dev/null 2>&1; then
    echo "ok tcp ${host}:${port}"
  else
    echo "warn tcp ${host}:${port} is not reachable yet"
  fi
}

echo "host: ${HA_NODE_NAME} (${HA_NODE_IP})"
echo "checking etcd peer/client ports"
for host in "${HA_FLEET_A_IP}" "${HA_FLEET_B_IP}" "${HA_WITNESS_IP}"; do
  check_tcp "${host}" "${HA_ETCD_CLIENT_PORT}"
  check_tcp "${host}" "${HA_ETCD_PEER_PORT}"
done

echo "checking Fleet app host ports"
for host in "${HA_FLEET_A_IP}" "${HA_FLEET_B_IP}"; do
  check_tcp "${host}" "${HA_POSTGRES_PORT}"
  check_tcp "${host}" "${HA_PATRONI_PORT}"
  check_tcp "${host}" "${HA_FAKE_FLEET_PORT}"
done

if [[ -n "${HA_VIP_INTERFACE:-}" ]]; then
  if ip addr show dev "${HA_VIP_INTERFACE}" >/dev/null 2>&1; then
    echo "ok interface ${HA_VIP_INTERFACE} exists"
  else
    echo "warn interface ${HA_VIP_INTERFACE} not found on this host"
  fi
fi

if [[ -n "${HA_VIP:-}" ]]; then
  if ip addr | grep -qE "[[:space:]]${HA_VIP}(/|[[:space:]])"; then
    echo "warn VIP ${HA_VIP} is already assigned on this host"
  else
    echo "ok VIP ${HA_VIP} is not assigned on this host"
  fi
fi
