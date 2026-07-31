#!/usr/bin/env bash
set -euo pipefail

# Patroni invokes this once after it initializes the first PostgreSQL primary.
# It creates the least-privileged Fleet login, Fleet database, and extensions
# that require bootstrap privileges. Normal application migrations still run
# later through Fleet.

connection_url="${1:?Patroni did not provide a bootstrap connection URL}"
fleet_password_file="${2:-/run/proto-fleet-ha/fleet-db-password}"
fleet_password="$(<"$fleet_password_file")"

# Strict synchronous mode has no standby during first bootstrap. Keep only
# these local role/database setup statements from waiting for one.
export PGOPTIONS="-c synchronous_commit=local"

# Feed the generated hex password through stdin and let libpq read Patroni's
# connection URL from the environment so neither secret appears in psql's
# process arguments. format(%L) performs PostgreSQL-literal escaping before
# \gexec runs the role statement.
PGDATABASE="$connection_url" psql --set=ON_ERROR_STOP=1 <<SQL
\set fleet_password '$fleet_password'
SELECT format(
    'CREATE ROLE fleet LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION',
    :'fleet_password'
)
\gexec

-- Make Fleet the database owner so its later migrations do not need the
-- PostgreSQL superuser.
CREATE DATABASE fleet OWNER fleet;

-- Extensions are installed while the bootstrap connection is privileged. The
-- Fleet role stays non-superuser after bootstrap.
\connect -reuse-previous=on fleet
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
SQL
