#!/usr/bin/env bash
#
# init_database.sh - InfluxDB database and table setup
#
# Creates the database and default measurement tables if they don't exist.
# Requires TOKEN to be set (either from environment or .env file).
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

TAG_KEY="device_id"

default_measurements=(
    "power_w"
    "hashrate_mhs"
    "temperature_c"
    "efficiency_jth"
    "device_metrics"
)

measurements=("${default_measurements[@]}")

db_exists() {
    influxdb3 show databases --token "$TOKEN" | grep -q "$INFLUXDB3_DATABASE_NAME"
}

table_exists() {
    influxdb3 query --token "$TOKEN" --database "$INFLUXDB3_DATABASE_NAME" "SHOW TABLES" | grep -q "$1"
}

create_table() {
    echo "Creating table $1 with tags $TAG_KEY in database $INFLUXDB3_DATABASE_NAME..."
    cmd=(
      influxdb3 create table
      --token "$TOKEN"
      --database "$INFLUXDB3_DATABASE_NAME"
      --tags "$TAG_KEY"
      -- "$1"
    )
    echo running "${cmd[@]}"
    "${cmd[@]}"
}

if db_exists; then
    echo "Database $INFLUXDB3_DATABASE_NAME already exists."
else
    echo "Creating database $INFLUXDB3_DATABASE_NAME..."
    influxdb3 create database --token "$TOKEN" "$INFLUXDB3_DATABASE_NAME"
fi

for table in "${measurements[@]}"; do
    if table_exists "$table"; then
        echo "Table $table already exists."
    else
        echo "Creating table $table..."
        create_table "$table"
    fi
done

echo "Database initialization complete."
