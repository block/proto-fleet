#!/usr/bin/env bash
set -euo pipefail

# Container entrypoint for a Patroni database member.
#
# Compose starts this script as root only because host-owned 0600 secrets must
# be copied into a private runtime directory. The final process is replaced
# with Patroni running as `postgres`; no long-lived database process runs as
# root.

# These values determine both the node's advertised identity and the rendered
# DCS topology. Fail before touching runtime state if Compose omitted one.
required_env=(
    HA_NODE_NAME
    HA_NODE_IP
    HA_DB_A_IP
    HA_DB_B_IP
    HA_DCS_C_IP
    HA_SECRETS_SOURCE
)

for name in "${required_env[@]}"; do
    if [[ -z "${!name:-}" ]]; then
        echo "missing required environment variable: ${name}" >&2
        exit 2
    fi
done

run_dir=/run/proto-fleet-ha
source_dir="${HA_SECRETS_SOURCE}"

# Certificates and passwords are mounted from the host as individual read-only
# files. Keep the lists explicit so adding a new credential requires a visible
# review of what the database container can access.
required_files=(
    service-ca.crt
    patroni-rest.crt
    patroni-rest.key
    postgres.crt
    postgres.key
)
password_files=(
    patroni-etcd-password
    patroni-rest-password
    postgres-superuser-password
    postgres-replication-password
    fleet-db-password
)
required_files+=("${password_files[@]}")

# Copy secrets rather than changing ownership of bind-mounted host files. The
# copies are readable only by postgres and disappear with the container.
install -d -m 0700 -o postgres -g postgres "$run_dir"
for file in "${required_files[@]}"; do
    if [[ ! -f "${source_dir}/${file}" ]]; then
        echo "missing HA secret: ${source_dir}/${file}" >&2
        exit 2
    fi
    install -m 0600 -o postgres -g postgres "${source_dir}/${file}" "${run_dir}/${file}"
done
for file in "${password_files[@]}"; do
    grep -q '[^[:space:]]' "${run_dir}/${file}" || {
        echo "HA secret must not be empty: ${file}" >&2
        exit 2
    }
done

# Render passwords as YAML scalars inside the private runtime directory. The
# generated config contains secrets and therefore remains mode 0600.
render-patroni-config \
    /etc/patroni/patroni.yml.tmpl \
    "$run_dir" \
    "${run_dir}/patroni.yml"
chown postgres:postgres "${run_dir}/patroni.yml"
chmod 0600 "${run_dir}/patroni.yml"

# Patroni expects PostgreSQL's data and socket directories to exist with the
# final process user's ownership before it initializes or joins the cluster.
export PGDATA="${PGDATA:-/home/postgres/pgdata/data}"
install -d -m 0700 -o postgres -g postgres "$PGDATA"
install -d -m 2775 -o postgres -g postgres /var/run/postgresql

# exec preserves Patroni's signals and exit status as the container's own.
exec gosu postgres patroni "${run_dir}/patroni.yml"
