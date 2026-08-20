#!/bin/bash

# Shared Docker daemon identity handling for the standalone deployment runner
# and recovery commands. Callers set PROJECT_ROOT before sourcing this file.

DOCKER_DAEMON_STATE_FILE="$PROJECT_ROOT/.docker-daemon-id"
DOCKER_DAEMON_STATE_PREFIX="proto-fleet-docker-daemon-v1:"

current_docker_daemon_id() {
    local daemon_id
    if ! daemon_id=$(docker info --format '{{.ID}}' 2>/dev/null); then
        echo "Error: could not inspect the selected Docker daemon." >&2
        return 1
    fi
    case "$daemon_id" in
        ''|*$'\n'*|*$'\r'*)
            echo "Error: the selected Docker daemon returned an invalid identity." >&2
            return 1
            ;;
    esac
    printf '%s' "$daemon_id"
}

read_persisted_docker_daemon_id() {
    local contents daemon_id
    if [ -L "$DOCKER_DAEMON_STATE_FILE" ] \
        || [ ! -f "$DOCKER_DAEMON_STATE_FILE" ] \
        || [ ! -r "$DOCKER_DAEMON_STATE_FILE" ]; then
        echo "Error: Docker daemon state must be a readable regular file: $DOCKER_DAEMON_STATE_FILE" >&2
        return 1
    fi
    contents=$(cat "$DOCKER_DAEMON_STATE_FILE") || return 1
    case "$contents" in
        "$DOCKER_DAEMON_STATE_PREFIX"*) daemon_id="${contents#"$DOCKER_DAEMON_STATE_PREFIX"}" ;;
        *)
            echo "Error: Docker daemon state has an unsupported format: $DOCKER_DAEMON_STATE_FILE" >&2
            return 1
            ;;
    esac
    case "$daemon_id" in
        ''|*$'\n'*|*$'\r'*)
            echo "Error: Docker daemon state contains an invalid identity: $DOCKER_DAEMON_STATE_FILE" >&2
            return 1
            ;;
    esac
    printf '%s' "$daemon_id"
}

verify_persisted_docker_daemon() {
    local expected_id current_id
    if [ ! -e "$DOCKER_DAEMON_STATE_FILE" ] && [ ! -L "$DOCKER_DAEMON_STATE_FILE" ]; then
        return 0
    fi
    expected_id=$(read_persisted_docker_daemon_id) || return 1
    current_id=$(current_docker_daemon_id) || return 1
    if [ "$current_id" != "$expected_id" ]; then
        echo "Error: the selected Docker daemon does not own this Proto Fleet installation." >&2
        echo "Select the Docker context or DOCKER_HOST used to install Fleet and retry." >&2
        echo "If the daemon was deliberately replaced, remove $DOCKER_DAEMON_STATE_FILE and rerun the installer to pin the new daemon." >&2
        return 1
    fi
}

# Strict variant for recovery flows: a missing state file is an error, not a
# fresh install.
require_persisted_docker_daemon() {
    if [ ! -e "$DOCKER_DAEMON_STATE_FILE" ] && [ ! -L "$DOCKER_DAEMON_STATE_FILE" ]; then
        echo "Error: Docker daemon state is missing; rerun the installer before password recovery." >&2
        return 1
    fi
    verify_persisted_docker_daemon
}

persist_current_docker_daemon() {
    local current_id temporary_state owner_group
    if [ -e "$DOCKER_DAEMON_STATE_FILE" ] || [ -L "$DOCKER_DAEMON_STATE_FILE" ]; then
        verify_persisted_docker_daemon
        return
    fi
    current_id=$(current_docker_daemon_id) || return 1

    temporary_state=$(umask 077; mktemp "$DOCKER_DAEMON_STATE_FILE.tmp.XXXXXX") || return 1
    if ! printf '%s%s\n' "$DOCKER_DAEMON_STATE_PREFIX" "$current_id" > "$temporary_state" \
        || ! chmod 600 "$temporary_state"; then
        rm -f "$temporary_state"
        return 1
    fi

    # A root-owned updater preserves the deployment administrator's access to
    # recovery state, matching the ownership of the persisted .env file.
    if [ "$(id -u)" -eq 0 ] && [ -n "${ENV_FILE:-}" ] \
        && [ ! -L "$ENV_FILE" ] && [ -f "$ENV_FILE" ]; then
        owner_group=$(stat -c '%u:%g' "$ENV_FILE" 2>/dev/null) \
            || owner_group=$(stat -f '%u:%g' "$ENV_FILE" 2>/dev/null) \
            || {
                rm -f "$temporary_state"
                return 1
            }
        if ! chown "$owner_group" "$temporary_state"; then
            rm -f "$temporary_state"
            return 1
        fi
    fi

    # -T (rename over a directory target as a file) is GNU-only.
    local mv_args=(-f)
    if [ "$(uname -s)" = "Linux" ]; then
        mv_args=(-fT)
    fi
    if ! mv "${mv_args[@]}" -- "$temporary_state" "$DOCKER_DAEMON_STATE_FILE"; then
        rm -f "$temporary_state"
        return 1
    fi
    [ ! -L "$DOCKER_DAEMON_STATE_FILE" ] && [ -f "$DOCKER_DAEMON_STATE_FILE" ]
}
