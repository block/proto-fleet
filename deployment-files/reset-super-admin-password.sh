#!/usr/bin/env bash
set -euo pipefail

DEPLOYMENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HA_CONFIG_DIR="${PROTO_FLEET_HA_CONFIG_DIR:-/etc/proto-fleet/ha}"
HA_NODE_ENV="$HA_CONFIG_DIR/node.env"
HA_ACTIVE_INSTALL="$HA_CONFIG_DIR/active-install"
HA_COMMAND="$DEPLOYMENT_DIR/ha/fleet-ha"
ENV_FILE="$DEPLOYMENT_DIR/.env"
COMPOSE_FILE="$DEPLOYMENT_DIR/docker-compose.yaml"
COMPOSE_PROJECT_HELPER="$DEPLOYMENT_DIR/scripts/compose-project.sh"

case "$#" in
    0) ;;
    1)
        if [ "$1" != "--password-stdin" ]; then
            echo "Error: unsupported password recovery option; only --password-stdin is allowed." >&2
            exit 1
        fi
        ;;
    *)
        echo "Error: password recovery accepts at most one option: --password-stdin." >&2
        exit 1
        ;;
esac

HA_ACTIVE=false
if [ -e "$HA_ACTIVE_INSTALL" ] || [ -L "$HA_ACTIVE_INSTALL" ]; then
    if [ -L "$HA_ACTIVE_INSTALL" ] || [ ! -f "$HA_ACTIVE_INSTALL" ] || [ ! -r "$HA_ACTIVE_INSTALL" ]; then
        echo "Error: HA installation marker must be a readable regular file: $HA_ACTIVE_INSTALL" >&2
        exit 1
    fi
    if [ "$(cat "$HA_ACTIVE_INSTALL")" != "$DEPLOYMENT_DIR" ]; then
        echo "Error: HA installation marker does not identify this deployment: $HA_ACTIVE_INSTALL" >&2
        exit 1
    fi
    HA_ACTIVE=true
fi

STANDALONE_ACTIVE=false
if [ -f "$ENV_FILE" ] && [ -r "$ENV_FILE" ]; then
    STANDALONE_ACTIVE=true
fi

if [ "$HA_ACTIVE" = true ] && [ "$STANDALONE_ACTIVE" = true ]; then
    echo "Error: both HA and standalone installation state are active; resolve the ambiguous topology before recovery." >&2
    exit 1
fi

if [ "$HA_ACTIVE" = true ]; then
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
    echo "Error: no active HA installation marker found and $ENV_FILE is not a readable regular file." >&2
    exit 1
fi
if [ ! -f "$COMPOSE_FILE" ] || [ ! -r "$COMPOSE_FILE" ]; then
    echo "Error: $COMPOSE_FILE must be a readable regular file." >&2
    exit 1
fi
if [ ! -f "$COMPOSE_PROJECT_HELPER" ] || [ ! -r "$COMPOSE_PROJECT_HELPER" ]; then
    echo "Error: $COMPOSE_PROJECT_HELPER must be a readable regular file." >&2
    exit 1
fi

PROJECT_ROOT="$DEPLOYMENT_DIR"
source "$COMPOSE_PROJECT_HELPER"
FLEET_COMPOSE_PROJECT_NAME=$(resolve_persisted_compose_project_name) || exit 1

if [ "${COMPOSE_PROJECT_NAME+x}" = "x" ] \
    && [ "$COMPOSE_PROJECT_NAME" != "$FLEET_COMPOSE_PROJECT_NAME" ]; then
    echo "Error: caller COMPOSE_PROJECT_NAME conflicts with the installed deployment; unset it before recovery." >&2
    exit 1
fi
for override in DB_DSN DB_NAME DB_USERNAME DB_PASSWORD DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS DOCKER_TLS_VERIFY DOCKER_CERT_PATH; do
    if [ "${!override+x}" = "x" ]; then
        echo "Error: caller $override is not allowed during password recovery; unset it and use the persisted deployment configuration." >&2
        exit 1
    fi
done
unset COMPOSE_PROJECT_NAME override

cd "$DEPLOYMENT_DIR"
exec docker --host unix:///var/run/docker.sock compose \
    --project-name "$FLEET_COMPOSE_PROJECT_NAME" \
    --project-directory "$DEPLOYMENT_DIR" \
    --env-file "$ENV_FILE" \
    -f "$COMPOSE_FILE" \
    run --rm --no-deps -T fleet-api \
    /app/fleetd admin reset-password "$@"
