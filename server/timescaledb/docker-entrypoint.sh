#!/usr/bin/env bash
set -Eeo pipefail

# Minimal entrypoint for PostgreSQL + TimescaleDB.
# Handles cluster initialization on first run, then execs postgres.
# Inspired by the official postgres Docker entrypoint but stripped to essentials.

# Run timescaledb-tune to optimize postgresql.conf for container resources.
# Detects container memory/CPU limits via cgroups and passes them to the tuner.
# Skipped if NO_TS_TUNE is set. Override detection with TS_TUNE_MEMORY / TS_TUNE_NUM_CPUS.
run_timescaledb_tune() {
    if [ -n "${NO_TS_TUNE:-}" ]; then
        return
    fi

    if ! command -v timescaledb-tune &> /dev/null; then
        return
    fi

    local tune_memory="${TS_TUNE_MEMORY:-}"
    local tune_cpus="${TS_TUNE_NUM_CPUS:-}"

    # Detect memory from cgroups if not explicitly set
    if [ -z "$tune_memory" ]; then
        local cgroup_mem=""
        local cgroup_detected=false

        if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
            # cgroups v2
            cgroup_mem=$(cat /sys/fs/cgroup/memory.max 2>/dev/null || true)
            [ "$cgroup_mem" = "max" ] && cgroup_mem=""
            [ -n "$cgroup_mem" ] && cgroup_detected=true
        elif [ -f /sys/fs/cgroup/memory/memory.limit_in_bytes ]; then
            # cgroups v1
            cgroup_mem=$(cat /sys/fs/cgroup/memory/memory.limit_in_bytes 2>/dev/null || true)
            [ -n "$cgroup_mem" ] && cgroup_detected=true
        fi

        if [ "$cgroup_detected" = true ] && [ "$cgroup_mem" != "18446744073709551615" ]; then
            local free_kb
            free_kb=$(grep MemTotal: /proc/meminfo | awk '{print $2}')
            local free_bytes=$(( free_kb * 1024 ))
            if [ "$cgroup_mem" -le "$free_bytes" ] 2>/dev/null; then
                tune_memory="$(awk "BEGIN {printf \"%d\", $cgroup_mem / 1024 / 1024}")MB"
            fi
        fi
    fi

    # Detect CPUs from cgroups if not explicitly set
    if [ -z "$tune_cpus" ]; then
        local cpu_quota=""
        local cpu_period=""

        if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
            # cgroups v2
            cpu_quota=$(cat /sys/fs/cgroup/cpu.max 2>/dev/null | awk '{print $1}' || true)
            [ "$cpu_quota" = "max" ] && cpu_quota=""
            [ -n "$cpu_quota" ] && cpu_period=$(cat /sys/fs/cgroup/cpu.max 2>/dev/null | awk '{print $2}' || true)
        elif [ -f /sys/fs/cgroup/cpu/cpu.cfs_quota_us ]; then
            # cgroups v1
            cpu_quota=$(cat /sys/fs/cgroup/cpu/cpu.cfs_quota_us 2>/dev/null || true)
            [ "$cpu_quota" = "-1" ] && cpu_quota=""
            [ -n "$cpu_quota" ] && cpu_period=$(cat /sys/fs/cgroup/cpu/cpu.cfs_period_us 2>/dev/null || true)
        fi

        if [ -n "$cpu_quota" ] && [ "${cpu_period:-}" = "100000" ]; then
            tune_cpus=$(( (cpu_quota + 99999) / 100000 ))
        fi
    fi

    local tune_flags=""
    [ -n "$tune_memory" ] && tune_flags="$tune_flags --memory=$tune_memory"
    [ -n "$tune_cpus" ] && tune_flags="$tune_flags --cpus=$tune_cpus"
    [ -n "${TS_TUNE_MAX_CONNS:-}" ] && tune_flags="$tune_flags --max-conns=$TS_TUNE_MAX_CONNS"
    [ -n "${TS_TUNE_MAX_BG_WORKERS:-}" ] && tune_flags="$tune_flags --max-bg-workers=$TS_TUNE_MAX_BG_WORKERS"

    echo "TimescaleDB: Running timescaledb-tune"
    # shellcheck disable=SC2086
    timescaledb-tune --quiet --yes \
        --conf-path="$PGDATA/postgresql.conf" \
        --pg-version="${PG_MAJOR:-18}" \
        $tune_flags || true
}

# If first arg looks like a flag, prepend "postgres"
if [ "${1:0:1}" = '-' ]; then
    set -- postgres "$@"
fi

# If we're running postgres, handle initialization
if [ "$1" = 'postgres' ]; then
    # Run as postgres user via gosu if currently root
    if [ "$(id -u)" = '0' ]; then
        # Fix ownership on the data directory (handles volume UID mismatches)
        mkdir -p "$PGDATA"
        chown -R postgres:postgres "$PGDATA" 2>/dev/null || true
        chmod 700 "$PGDATA"

        chown -R postgres:postgres /var/run/postgresql 2>/dev/null || true

        # Re-exec as postgres user
        exec gosu postgres "$0" "$@"
    fi

    # Initialize database if PGDATA is empty
    if [ -z "$(ls -A "$PGDATA" 2>/dev/null)" ]; then
        echo "TimescaleDB: Initializing database in $PGDATA"

        pwfile="$(mktemp)"
        printf '%s' "${POSTGRES_PASSWORD:-}" >"$pwfile"
        initdb --username="${POSTGRES_USER:-postgres}" --pwfile="$pwfile" \
            --auth-host=scram-sha-256 --auth-local=trust
        rm -f "$pwfile"

        # Configure pg_hba.conf: allow connections from any host with password
        {
            echo "# Allow connections from anywhere (Docker networking)"
            echo "host all all 0.0.0.0/0 scram-sha-256"
            echo "host all all ::/0 scram-sha-256"
        } >> "$PGDATA/pg_hba.conf"

        # Configure postgresql.conf
        {
            echo ""
            echo "# TimescaleDB"
            echo "shared_preload_libraries = 'timescaledb'"
            echo "listen_addresses = '*'"
        } >> "$PGDATA/postgresql.conf"

        # Auto-tune PostgreSQL config based on container resources
        run_timescaledb_tune

        # Start a temporary server to create the initial database
        pg_ctl -D "$PGDATA" -o "-c listen_addresses='' -p 5432" -w start

        # Create the requested database if it doesn't already exist.
        # initdb only creates "postgres", "template0", and "template1".
        if [ -n "${POSTGRES_DB:-}" ] && [ "$POSTGRES_DB" != "postgres" ]; then
            psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER:-postgres}" --dbname postgres --no-password <<-EOSQL
                CREATE DATABASE "${POSTGRES_DB}";
EOSQL
        fi

        # Run any init scripts mounted in /docker-entrypoint-initdb.d/
        if [ -d /docker-entrypoint-initdb.d/ ]; then
            for f in /docker-entrypoint-initdb.d/*; do
                [ -e "$f" ] || continue
                case "$f" in
                    *.sh)
                        echo "Running init script: $f"
                        . "$f"
                        ;;
                    *.sql)
                        echo "Running init SQL: $f"
                        psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER:-postgres}" \
                            --dbname "${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" --no-password -f "$f"
                        ;;
                    *)
                        echo "Ignoring: $f (not .sh or .sql)"
                        ;;
                esac
            done
        fi

        pg_ctl -D "$PGDATA" -m fast -w stop
        echo "TimescaleDB: Database initialization complete."
    fi
fi

exec "$@"
