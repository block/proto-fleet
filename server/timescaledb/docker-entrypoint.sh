#!/usr/bin/env bash
set -Eeo pipefail

# Minimal entrypoint for PostgreSQL + TimescaleDB.
# Handles cluster initialization on first run, then execs postgres.
# Inspired by the official postgres Docker entrypoint but stripped to essentials.

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

        initdb --username="${POSTGRES_USER:-postgres}" --pwfile=<(echo "${POSTGRES_PASSWORD:-}") \
            --auth-host=md5 --auth-local=trust

        # Configure pg_hba.conf: allow connections from any host with password
        {
            echo "# Allow connections from anywhere (Docker networking)"
            echo "host all all 0.0.0.0/0 md5"
            echo "host all all ::/0 md5"
        } >> "$PGDATA/pg_hba.conf"

        # Configure postgresql.conf
        {
            echo ""
            echo "# TimescaleDB"
            echo "shared_preload_libraries = 'timescaledb'"
            echo "listen_addresses = '*'"
        } >> "$PGDATA/postgresql.conf"

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
