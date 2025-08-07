#!/usr/bin/env bash

# Wait until influxdb is healthy"
until curl -s http://localhost:8181/health | grep -q "OK" ; do
  echo "Waiting for InfluxDB to be healthy..."
  sleep 5
done
echo "InfluxDB is healthy."

TAG_KEY="device_id"

default_measurements=(
    "power_w"
    "hashrate_mhs"
    "temperature_c"
    "efficiency_jth"
)

measurements=("${default_measurements[@]}")

# Check if token is already set in /var/lib/influxdb3/shared/.env
ENV_FILE=/var/lib/influxdb3/start/.env

# Generate a new token
influxdb3 create token --admin > /tmp/influxdb3_token.txt
if [ $? -ne 0 ]; then
    echo "Failed to create InfluxDB token."
    exit 1
fi

if grep -q "409" /tmp/influxdb3_token.txt; then
    echo "Token already exists, reusing the existing token."
    exit 0
fi

echo "New token created successfully."

# Read the token from the file
# Extracting token form line "Token: <token_value>"
TOKEN=apiv3$(sed -n 's/^.*[t,T]oken:.*apiv3//p' /tmp/influxdb3_token.txt)

# Check if the token is empty
if [ -z "$TOKEN" ]; then
    echo "Failed to extract token from /tmp/influxdb3_token.txt."
    exit 1
fi 

# Write the token to the .env file
if grep -q '^INFLUXDB3_AUTH_TOKEN=' "$ENV_FILE"; then
    # replace existing token
    sed -i "s/^INFLUXDB3_AUTH_TOKEN=.*/INFLUXDB3_AUTH_TOKEN=$TOKEN/" "$ENV_FILE"
else
    # append new token
    echo "INFLUXDB3_AUTH_TOKEN=$TOKEN" >> "$ENV_FILE"
fi
echo "Token written to $ENV_FILE."

db_exists() {
    influxdb3 show databases --token $TOKEN | grep -q "$INFLUXDB3_DATABASE_NAME" 
}

table_exists() {
    influxdb3 query --token $TOKEN --database "$INFLUXDB3_DATABASE_NAME" "SHOW TABLES" | grep -q "$1"
}

create_table() {
    # Echo script to create table with tags
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
    influxdb3 create database --token $TOKEN "$INFLUXDB3_DATABASE_NAME"
fi

for table in "${measurements[@]}"; do
    if table_exists "$table"; then
        echo "Table $table already exists."
    else
        echo "Creating table $table..."
        create_table "$table"
    fi
done
