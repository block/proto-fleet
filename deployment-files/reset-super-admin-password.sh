#!/usr/bin/env bash
set -euo pipefail

DEPLOYMENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$DEPLOYMENT_DIR/.env"
COMPOSE_FILE="$DEPLOYMENT_DIR/docker-compose.yaml"

if [ ! -f "$ENV_FILE" ] || [ ! -r "$ENV_FILE" ]; then
    echo "Error: $ENV_FILE must be a readable regular file." >&2
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
