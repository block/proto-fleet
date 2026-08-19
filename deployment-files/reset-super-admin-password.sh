#!/usr/bin/env bash
set -euo pipefail

DEPLOYMENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HA_CONFIG_DIR="${PROTO_FLEET_HA_CONFIG_DIR:-/etc/proto-fleet/ha}"
HA_NODE_ENV="$HA_CONFIG_DIR/node.env"
HA_COMMAND="$DEPLOYMENT_DIR/ha/fleet-ha"
ENV_FILE="$DEPLOYMENT_DIR/.env"
COMPOSE_FILE="$DEPLOYMENT_DIR/docker-compose.yaml"

if [ -d "$HA_CONFIG_DIR" ]; then
    if [ ! -f "$HA_NODE_ENV" ] || [ ! -r "$HA_NODE_ENV" ]; then
        echo "Error: HA recovery requires read access to $HA_NODE_ENV; rerun as the HA deployment user (normally with sudo)." >&2
        exit 1
    fi
    if [ ! -x "$HA_COMMAND" ]; then
        echo "Error: HA recovery requires executable $HA_COMMAND." >&2
        exit 1
    fi
    exec "$HA_COMMAND" reset-password "$@"
fi

if [ ! -f "$ENV_FILE" ] || [ ! -r "$ENV_FILE" ]; then
    echo "Error: no installed HA profile found and $ENV_FILE is not a readable regular file." >&2
    exit 1
fi
if [ ! -f "$COMPOSE_FILE" ] || [ ! -r "$COMPOSE_FILE" ]; then
    echo "Error: $COMPOSE_FILE must be a readable regular file." >&2
    exit 1
fi

cd "$DEPLOYMENT_DIR"
exec docker compose \
    --project-directory "$DEPLOYMENT_DIR" \
    --env-file "$ENV_FILE" \
    -f "$COMPOSE_FILE" \
    run --rm -T fleet-api \
    /app/fleetd admin reset-password "$@"
