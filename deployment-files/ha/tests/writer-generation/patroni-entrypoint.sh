#!/usr/bin/env bash
set -euo pipefail

required=(
  HA_WRITER_SCOPE
  PATRONI_NAME
  PATRONI_REPLICATION_PASSWORD
  PATRONI_REPLICATION_USERNAME
  PATRONI_REWIND_PASSWORD
  PATRONI_REWIND_USERNAME
  PATRONI_SUPERUSER_PASSWORD
)

for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 2
  fi
done

export PGDATA="${PGDATA:-/home/postgres/pgdata/data}"
mkdir -p "${PGDATA}" /var/run/postgresql

if [[ "$(id -u)" -eq 0 ]]; then
  chown -R postgres:postgres "${PGDATA}" /var/run/postgresql
  chmod 700 "${PGDATA}"
fi

envsubst < /etc/patroni/patroni.template.yml > /tmp/patroni.yml

if [[ "$(id -u)" -eq 0 ]]; then
  exec gosu postgres patroni /tmp/patroni.yml
fi

exec patroni /tmp/patroni.yml
