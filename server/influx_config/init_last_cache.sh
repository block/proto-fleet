#!/usr/bin/env bash
#
# init_last_cache.sh - InfluxDB Last Value Cache configuration
#
# Creates a Last Value Cache for device_metrics table to enable fast latest-value queries.
# The LVC caches all value columns automatically (no explicit column list), so schema
# changes to device_metrics are automatically reflected in the cache.
#

set -e

ENV_FILE=/var/lib/influxdb3/start/.env

# Source the .env file to get TOKEN if not already set
if [ -z "$TOKEN" ] && [ -f "$ENV_FILE" ]; then
    source "$ENV_FILE"
    TOKEN="$INFLUXDB3_AUTH_TOKEN"
fi

if [ -z "$TOKEN" ]; then
    echo "ERROR: TOKEN not set. Run manage_token.sh first."
    exit 1
fi

LVC_NAME="device_metrics_latest"
LVC_TABLE="device_metrics"
LVC_KEY_COLUMNS="device_id"
LVC_TTL="10mins"
LVC_COUNT="60"

lvc_exists() {
    influxdb3 query --token "$TOKEN" --database "$INFLUXDB3_DATABASE_NAME" \
        "SELECT name FROM system.last_caches WHERE name = '$LVC_NAME'" 2>/dev/null | grep -q "$LVC_NAME"
}

if lvc_exists; then
    echo "Last Value Cache $LVC_NAME already exists."
else
    echo "Creating Last Value Cache $LVC_NAME for table $LVC_TABLE..."
    influxdb3 create last_cache \
        --token "$TOKEN" \
        --database "$INFLUXDB3_DATABASE_NAME" \
        --table "$LVC_TABLE" \
        --key-columns "$LVC_KEY_COLUMNS" \
        --ttl "$LVC_TTL" \
        --count "$LVC_COUNT" \
        "$LVC_NAME"
    echo "Last Value Cache $LVC_NAME created successfully."
fi

echo "LVC setup complete."
