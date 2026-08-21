#!/usr/bin/env bash
set -euo pipefail

# Patroni invokes this once after it initializes the first PostgreSQL primary.
# It creates the least-privileged Fleet login, Fleet database, and extensions
# that require bootstrap privileges. Normal application migrations still run
# later through Fleet.

connection_url="${1:?Patroni did not provide a bootstrap connection URL}"
fleet_password_file="${2:-/run/proto-fleet-ha/fleet-db-password}"
fleet_password="$(<"$fleet_password_file")"
grafana_password_file="${3:-/run/proto-fleet-ha/grafana-db-password}"
grafana_password="$(<"$grafana_password_file")"

# Patroni passes a password-free libpq connection string and supplies any
# bootstrap credential through PGPASSFILE. Pass that string as psql's database
# target; PGDATABASE would treat the whole string as a literal database name.
# The Fleet password still travels over stdin, and format(%L) performs
# PostgreSQL-literal escaping before \gexec runs the role statement.
if [[ ! "$fleet_password" =~ ^[0-9a-f]{64}$ ]]; then
    echo "fleet database password must be 64 lowercase hexadecimal characters" >&2
    exit 1
fi
if [[ ! "$grafana_password" =~ ^[0-9a-f]{64}$ ]]; then
    echo "grafana database password must be 64 lowercase hexadecimal characters" >&2
    exit 1
fi

psql --dbname="$connection_url" --set=ON_ERROR_STOP=1 <<SQL
\set fleet_password '$fleet_password'
\set grafana_password '$grafana_password'
SELECT format(
    'CREATE ROLE fleet LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION',
    :'fleet_password'
)
\gexec
SELECT format(
    'CREATE ROLE grafana_ha_ro LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT',
    :'grafana_password'
)
\gexec

-- Make Fleet the database owner so its later migrations do not need the
-- PostgreSQL superuser.
CREATE DATABASE fleet OWNER fleet;
GRANT CONNECT ON DATABASE fleet TO grafana_ha_ro;

-- Extensions are installed while the bootstrap connection is privileged. The
-- Fleet role stays non-superuser after bootstrap.
\connect -reuse-previous=on fleet
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Grafana evaluates file-provisioned rules with a read-only login. A Fleet
-- migration grants SELECT only on the metric table and active-org view after
-- those objects exist; do not grant access to every application table here.
GRANT USAGE ON SCHEMA public TO grafana_ha_ro;
SQL
