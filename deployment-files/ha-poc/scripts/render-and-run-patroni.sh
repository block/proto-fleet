#!/usr/bin/env bash
set -euo pipefail

export HA_ETCD_CLIENT_PORT="${HA_ETCD_CLIENT_PORT:-2379}"
export HA_POSTGRES_PORT="${HA_POSTGRES_PORT:-5432}"
export HA_PATRONI_PORT="${HA_PATRONI_PORT:-8008}"

required=(
  HA_CLUSTER_NAME
  HA_NODE_NAME
  HA_NODE_IP
  HA_FLEET_A_IP
  HA_FLEET_B_IP
  HA_WITNESS_IP
  HA_POSTGRES_SUPERUSER_PASSWORD
  HA_REPLICATION_USER
  HA_REPLICATION_PASSWORD
  HA_REWIND_USER
  HA_REWIND_PASSWORD
)

for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 2
  fi
done

envsubst < /etc/patroni/patroni.template.yml > /tmp/patroni.yml
exec patroni /tmp/patroni.yml
