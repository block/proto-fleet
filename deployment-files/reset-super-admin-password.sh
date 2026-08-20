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
DOCKER_DAEMON_HELPER="$DEPLOYMENT_DIR/scripts/docker-daemon.sh"

require_readable_file() {
    if [ ! -f "$1" ] || [ ! -r "$1" ]; then
        echo "Error: ${2:-$1 must be a readable regular file.}" >&2
        exit 1
    fi
}

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

# Checked before topology detection so the HA delegation is covered too. An
# interactive terminal would block without a prompt and echo the typed
# password in cleartext.
if [ "${1:-}" = "--password-stdin" ] && [ -t 0 ]; then
    echo "Error: --password-stdin requires piped input, for example: printf '%s\\n' \"\$NEW_PASSWORD\" | $0 --password-stdin" >&2
    exit 1
fi

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
    require_readable_file "$HA_NODE_ENV" \
        "HA recovery requires read access to $HA_NODE_ENV; rerun as the HA deployment user (normally with sudo)."
    if [ ! -x "$HA_COMMAND" ]; then
        echo "Error: HA recovery requires executable $HA_COMMAND." >&2
        exit 1
    fi
    exec "$HA_COMMAND" reset-password "$@"
fi

require_readable_file "$ENV_FILE" \
    "no active HA installation marker found and $ENV_FILE is not a readable regular file."
require_readable_file "$COMPOSE_FILE"
require_readable_file "$COMPOSE_PROJECT_HELPER"
require_readable_file "$DOCKER_DAEMON_HELPER"

PROJECT_ROOT="$DEPLOYMENT_DIR"
source "$COMPOSE_PROJECT_HELPER"
source "$DOCKER_DAEMON_HELPER"
FLEET_COMPOSE_PROJECT_NAME=$(resolve_persisted_compose_project_name) || exit 1

if [ "${COMPOSE_PROJECT_NAME+x}" = "x" ] \
    && [ "$COMPOSE_PROJECT_NAME" != "$FLEET_COMPOSE_PROJECT_NAME" ]; then
    echo "Error: caller COMPOSE_PROJECT_NAME conflicts with the installed deployment; unset it before recovery." >&2
    exit 1
fi
for override in DB_DSN DB_NAME DB_USERNAME DB_PASSWORD; do
    if [ "${!override+x}" = "x" ]; then
        echo "Error: caller $override is not allowed during password recovery; unset it and use the persisted deployment configuration." >&2
        exit 1
    fi
done
unset COMPOSE_PROJECT_NAME override

# Pin the Docker endpoint selection for the whole run. The current context is
# mutable global state (docker context use), so without pinning the daemon
# identity check and the Compose invocation could observe different daemons.
# Pinning the context name (not the endpoint) preserves any TLS material the
# context carries.
if [ -z "${DOCKER_HOST:-}" ] && [ -z "${DOCKER_CONTEXT:-}" ]; then
    DOCKER_CONTEXT=$(docker context show) || {
        echo "Error: could not determine the current Docker context." >&2
        exit 1
    }
    export DOCKER_CONTEXT
fi

if ! require_persisted_docker_daemon; then
    exit 1
fi

cd "$DEPLOYMENT_DIR"
docker_command=(
    docker compose
    --project-name "$FLEET_COMPOSE_PROJECT_NAME"
    --project-directory "$DEPLOYMENT_DIR"
    --env-file "$ENV_FILE"
    -f "$COMPOSE_FILE"
    run --rm --no-deps -T fleet-api
    /app/fleetd admin reset-password --password-stdin
)

if [ "${1:-}" = "--password-stdin" ]; then
    exec "${docker_command[@]}"
fi

if ! command -v openssl >/dev/null 2>&1; then
    echo "Error: generated password recovery requires openssl." >&2
    exit 1
fi
temporary_password=$(openssl rand -base64 24 | tr '+/' '-_' | tr -d '\r\n') || {
    echo "Error: could not generate a temporary password." >&2
    exit 1
}
if [ "${#temporary_password}" -ne 32 ]; then
    echo "Error: generated temporary password had an unexpected length." >&2
    exit 1
fi
if ! printf '%s\n' "$temporary_password" | "${docker_command[@]}"; then
    exit 1
fi
printf 'Temporary password: %s\n' "$temporary_password"
