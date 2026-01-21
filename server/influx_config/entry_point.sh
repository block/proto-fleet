#!/usr/bin/env bash
#
# entry_point.sh - InfluxDB container entrypoint
#
# Runs initialization scripts in background while starting the InfluxDB server.
#

SCRIPT_DIR=/var/lib/influxdb3/start

# Run initialization scripts in background (they wait for InfluxDB to be healthy)
(
    echo "Running manage_token.sh..." && "$SCRIPT_DIR/manage_token.sh" || { echo "manage_token.sh failed"; exit 1; }
    echo "Running init_database.sh..." && "$SCRIPT_DIR/init_database.sh" || { echo "init_database.sh failed"; exit 1; }
    echo "Running init_last_cache.sh..." && "$SCRIPT_DIR/init_last_cache.sh" || { echo "init_last_cache.sh failed"; exit 1; }
) &

# Start InfluxDB server with increased query file limit for 5-day queries
# Default to 1000 for 5-day dashboard queries, but allow override via environment variable
# Note: increasing this value will increase the memory and cpu usage of the InfluxDB server.
QUERY_FILE_LIMIT=${INFLUXDB_QUERY_FILE_LIMIT:-1000}
influxdb3 serve --query-file-limit "$QUERY_FILE_LIMIT"
