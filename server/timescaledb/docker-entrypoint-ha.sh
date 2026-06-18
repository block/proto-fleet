#!/usr/bin/env bash
set -Eeo pipefail

PGDATA="${PGDATA:-/home/postgres/pgdata/data}"
PGPORT="${PGPORT:-5432}"
HA_AUTH_METHOD="${HA_AUTH_METHOD:-trust}"
HA_SSL_SELF_SIGNED="${HA_SSL_SELF_SIGNED:-false}"
POSTGRES_DB="${POSTGRES_DB:-fleet}"
POSTGRES_USER="${POSTGRES_USER:-fleet}"

log() {
    echo "Proto Fleet HA DB: $*"
}

die() {
    echo "Proto Fleet HA DB error: $*" >&2
    exit 1
}

require_env() {
    local name="$1"
    if [ -z "${!name:-}" ]; then
        die "$name is required"
    fi
}

append_once() {
    local file="$1"
    local line="$2"
    grep -qxF "$line" "$file" 2>/dev/null || echo "$line" >> "$file"
}

ensure_shared_preload_libraries() {
    local file="$1"
    local desired="shared_preload_libraries = 'timescaledb,pgautofailover'"

    if grep -q '^shared_preload_libraries' "$file" 2>/dev/null; then
        sed -i "s/^shared_preload_libraries.*/$desired/" "$file"
    else
        echo "$desired" >> "$file"
    fi
}

tune_postgres() {
    [ -f "$PGDATA/postgresql.conf" ] || return 0

    ensure_shared_preload_libraries "$PGDATA/postgresql.conf"
    append_once "$PGDATA/postgresql.conf" "listen_addresses = '*'"

    if [ -n "${NO_TS_TUNE:-}" ] || ! command -v timescaledb-tune >/dev/null 2>&1; then
        return 0
    fi

    local tune_flags=""
    [ -n "${TS_TUNE_MEMORY:-}" ] && tune_flags="$tune_flags --memory=$TS_TUNE_MEMORY"
    [ -n "${TS_TUNE_NUM_CPUS:-}" ] && tune_flags="$tune_flags --cpus=$TS_TUNE_NUM_CPUS"
    [ -n "${TS_TUNE_MAX_CONNS:-}" ] && tune_flags="$tune_flags --max-conns=$TS_TUNE_MAX_CONNS"
    [ -n "${TS_TUNE_MAX_BG_WORKERS:-}" ] && tune_flags="$tune_flags --max-bg-workers=$TS_TUNE_MAX_BG_WORKERS"

    log "running timescaledb-tune"
    # shellcheck disable=SC2086
    timescaledb-tune --quiet --yes \
        --conf-path="$PGDATA/postgresql.conf" \
        --pg-version="${PG_MAJOR:-18}" \
        $tune_flags || true
}

ssl_flags=()
if [ "$HA_SSL_SELF_SIGNED" = "true" ]; then
    ssl_flags=(--ssl-self-signed)
elif [ "$HA_SSL_SELF_SIGNED" = "false" ]; then
    ssl_flags=(--no-ssl)
else
    die "HA_SSL_SELF_SIGNED must be true or false"
fi

if [ "$(id -u)" = "0" ]; then
    pgdata_parent="$(dirname "$PGDATA")"
    mkdir -p "$PGDATA" "$pgdata_parent/backup" /var/run/postgresql
    chown -R postgres:postgres "$pgdata_parent" /var/run/postgresql 2>/dev/null || true
    chmod 700 "$PGDATA"
    exec gosu postgres "$0" "$@"
fi

mkdir -p "$PGDATA"

if [ -z "${HA_ROLE:-}" ] && [ "$#" -gt 0 ]; then
    if [ "$1" != "pg_autoctl" ] || [ "${2:-}" != "run" ]; then
        exec "$@"
    fi
fi

case "${HA_ROLE:-}" in
    monitor)
        require_env HA_NODE_HOST
        if [ ! -f "$PGDATA/pg_autoctl.cfg" ]; then
            log "initializing pg_auto_failover monitor at $HA_NODE_HOST:$PGPORT"
            pg_autoctl create monitor \
                --pgdata "$PGDATA" \
                --pgport "$PGPORT" \
                --hostname "$HA_NODE_HOST" \
                --auth "$HA_AUTH_METHOD" \
                "${ssl_flags[@]}"
        fi
        exec pg_autoctl run --pgdata "$PGDATA"
        ;;

    data)
        require_env HA_NODE_NAME
        require_env HA_NODE_HOST
        require_env HA_MONITOR_URL
        if [ ! -f "$PGDATA/pg_autoctl.cfg" ]; then
            log "initializing pg_auto_failover data node $HA_NODE_NAME at $HA_NODE_HOST:$PGPORT"
            pg_autoctl create postgres \
                --pgdata "$PGDATA" \
                --pgport "$PGPORT" \
                --listen "*" \
                --hostname "$HA_NODE_HOST" \
                --name "$HA_NODE_NAME" \
                --username "$POSTGRES_USER" \
                --dbname "$POSTGRES_DB" \
                --monitor "$HA_MONITOR_URL" \
                --auth "$HA_AUTH_METHOD" \
                --pg-hba-lan \
                "${ssl_flags[@]}"
            tune_postgres
        fi
        exec pg_autoctl run --pgdata "$PGDATA"
        ;;

    "")
        die "HA_ROLE is required and must be monitor or data"
        ;;

    *)
        die "unknown HA_ROLE: $HA_ROLE"
        ;;
esac
