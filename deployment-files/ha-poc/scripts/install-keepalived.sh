#!/usr/bin/env bash
set -euo pipefail

env_file="${1:-${HA_POC_ENV:-.env}}"
if [[ ! -f "${env_file}" ]]; then
  echo "env file not found: ${env_file}" >&2
  exit 2
fi

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

required=(
  HA_NODE_IP
  HA_VIP_CIDR
  HA_VIP_INTERFACE
  HA_VIP_ROUTER_ID
  HA_VIP_AUTH_PASS
  HA_KEEPALIVED_PRIORITY
  HA_KEEPALIVED_PEER_IP
)

for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 2
  fi
done

if ! command -v envsubst >/dev/null 2>&1; then
  echo "envsubst is required. Install gettext-base first." >&2
  exit 2
fi

if ! command -v keepalived >/dev/null 2>&1; then
  echo "keepalived is required. Install it first, for example: sudo apt-get install -y keepalived" >&2
  exit 2
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl is required to install keepalived as a host service" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
poc_dir="$(cd "${script_dir}/.." && pwd)"

install -m 0755 "${script_dir}/keepalived-check-active.sh" /usr/local/bin/ha-poc-check-active
install -d /etc/keepalived
if [[ -f /etc/keepalived/keepalived.conf ]]; then
  cp /etc/keepalived/keepalived.conf "/etc/keepalived/keepalived.conf.backup.$(date +%Y%m%d%H%M%S)"
fi
envsubst < "${poc_dir}/keepalived.conf.template" > /etc/keepalived/keepalived.conf

systemctl enable --now keepalived
systemctl restart keepalived
systemctl status --no-pager keepalived
