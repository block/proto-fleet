#!/bin/sh
# Start token management script, it will wait for InfluxDB to be healthy and then create or reuse a token.
/var/lib/influxdb3/start/manage_token.sh &
# Start InfluxDB server with increased query file limit for 5-day queries
# Default to 1000 for 5-day dashboard queries, but allow override via environment variable
# Note: increasing this value will increase the memory and cpu usage of the InfluxDB server.
QUERY_FILE_LIMIT=${INFLUXDB_QUERY_FILE_LIMIT:-1000}
influxdb3 serve --query-file-limit "$QUERY_FILE_LIMIT"
