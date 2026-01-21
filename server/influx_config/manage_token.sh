#!/usr/bin/env bash
#
# manage_token.sh - InfluxDB token management
#
# Waits for InfluxDB to be healthy, creates an admin token (or reuses existing),
# and writes it to the .env file for downstream scripts.
#

set -e

ENV_FILE=/var/lib/influxdb3/start/.env

# Wait until influxdb is healthy
until curl -s http://localhost:8181/health | grep -q "OK" ; do
  echo "Waiting for InfluxDB to be healthy..."
  sleep 5
done
echo "InfluxDB is healthy."

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
# Extracting token from line "Token: <token_value>"
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
