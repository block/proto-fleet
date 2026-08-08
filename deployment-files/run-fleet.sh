#!/bin/bash

# ============================================================================
# Proto Fleet Installation and Setup Script
# ============================================================================

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_FLEET_ORIGINAL_ARGS=("$@")
FLEET_COMPOSE_PROJECT_NAME=""
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
COMPOSE_ALERTS_FILE="$PROJECT_ROOT/docker-compose.alerts.yaml"
COMPOSE_SYSTEM_MONITORING_FILE="$PROJECT_ROOT/docker-compose.system-monitoring.yaml"
COMPOSE_TRACING_FILE="$PROJECT_ROOT/docker-compose.tracing.yaml"
COMPOSE_UPDATER_FILE="$PROJECT_ROOT/docker-compose.updater.yaml"
ENV_FILE="$PROJECT_ROOT/.env"
VERSION_FILE="$PROJECT_ROOT/version.txt"
RELEASE_MANIFEST_FILE="$PROJECT_ROOT/deployment-manifest.sha256"
TSDB_IMAGE="$PROJECT_ROOT/images/timescaledb.tar.gz"
PREFLIGHT_MARKER="$PROJECT_ROOT/.update-preflight-complete"
NGINX_CONFIG_TEMP=""
UPDATER_SOCKET_PATH="/run/proto-fleet-updater/updater.sock"
HOST_UPDATER_ENV_PATH="/etc/proto-fleet/updater.env"
DIRECT_UPDATER_PRIVILEGE=()
DIRECT_UPDATER_WAS_ACTIVE=false
DIRECT_UPDATER_SERVICE_PRESENT=false
DIRECT_UPDATER_OWNERSHIP_VERIFIED=false
DIRECT_UPDATER_ENV_ROLLBACK_PENDING=false
DEPLOYMENT_MUTATION_STARTED=false

# Preserve process-level Compose overrides before the script initializes its
# runtime flags with the same names. Plain shell assignment keeps an inherited
# variable's export bit, so inspecting those names later would otherwise see
# the script default instead of the invoking environment.
OVERLAY_FLAG_KEYS=(ENABLE_BETA_ALERTS ENABLE_SYSTEM_MONITORING ENABLE_TRACING ENABLE_ONE_CLICK_UPDATES)
for overlay_key in "${OVERLAY_FLAG_KEYS[@]}"; do
    printf -v "INVOKING_${overlay_key}_SET" '%s' "${!overlay_key+x}"
    printf -v "INVOKING_${overlay_key}_VALUE" '%s' "${!overlay_key-}"
done
unset overlay_key

ENABLE_BETA_ALERTS=false
ENABLE_SYSTEM_MONITORING=false
ENABLE_TRACING=false
ENABLE_ONE_CLICK_UPDATES=false
ONE_CLICK_UPDATES_OVERRIDE=""
NON_INTERACTIVE=false
PREFLIGHT_ONLY=false
SKIP_BUILD=false
PREFLIGHT_FINGERPRINT=""
RELEASE_IMAGE_TAG=""
FLEET_API_IMAGE=""
FLEET_CLIENT_IMAGE=""
TIMESCALEDB_IMAGE=""
TIMESCALEDB_HA_IMAGE=""
# Every repository whose tags release retention may protect or remove.
PROTO_FLEET_IMAGE_REPOSITORIES=(
    proto-fleet-api
    proto-fleet-client
    proto-fleet-timescaledb
    proto-fleet-timescaledb-ha
)
PREVIOUS_RELEASE_IMAGE_TAGS=()
RELEASE_IMAGE_CLEANUP_SAFE=true
DD_HOSTNAME_DEFAULTED=false

parse_direct_updater_install_root() {
    local contents="$1"
    local key_re='^[[:space:]]*PROTO_FLEET_INSTALL_ROOT[[:space:]]*='
    local assignment_re='^PROTO_FLEET_INSTALL_ROOT="(([^"\\]|\\["\\])*)"$'
    local line encoded decoded char
    local found=0

    while IFS= read -r line || [ -n "$line" ]; do
        [[ "$line" =~ $key_re ]] || continue
        [ "$found" -eq 0 ] || return 1
        [[ "$line" =~ $assignment_re ]] || return 1
        encoded="${BASH_REMATCH[1]}"
        decoded=""
        while [ -n "$encoded" ]; do
            char="${encoded:0:1}"
            encoded="${encoded:1}"
            if [ "$char" = '\' ]; then
                [ -n "$encoded" ] || return 1
                char="${encoded:0:1}"
                encoded="${encoded:1}"
                [ "$char" = '\' ] || [ "$char" = '"' ] || return 1
            fi
            decoded="${decoded}${char}"
        done
        [ -n "$decoded" ] || return 1
        found=1
    done <<< "$contents"

    [ "$found" -eq 1 ] || return 1
    printf '%s\n' "$decoded"
}

verify_direct_updater_ownership() {
    local contents configured_root configured_resolved expected_resolved

    [ "$DIRECT_UPDATER_OWNERSHIP_VERIFIED" != "true" ] || return 0
    if ! contents="$(${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        cat -- "$HOST_UPDATER_ENV_PATH" 2>/dev/null)" \
      || ! configured_root="$(parse_direct_updater_install_root "$contents")"; then
        echo "Error: could not read a valid updater install root from $HOST_UPDATER_ENV_PATH; refusing to control the global updater service." >&2
        return 1
    fi
    if ! configured_resolved="$(cd "$configured_root" 2>/dev/null && pwd -P)"; then
        echo "Error: the configured updater install root cannot be resolved: $configured_root" >&2
        return 1
    fi
    if ! expected_resolved="$(cd "$PROJECT_ROOT/.." 2>/dev/null && pwd -P)"; then
        echo "Error: the current Proto Fleet install root cannot be resolved from $PROJECT_ROOT." >&2
        return 1
    fi
    if [ "${configured_resolved%/}" != "${expected_resolved%/}" ]; then
        echo "Error: proto-fleet-updater.service belongs to a different Proto Fleet installation; refusing to change it." >&2
        echo "  Updater install root: $configured_resolved" >&2
        echo "  Current install root: $expected_resolved" >&2
        return 1
    fi
    DIRECT_UPDATER_OWNERSHIP_VERIFIED=true
}

deployment_directory_identity() {
    local identity

    # GNU and BSD stat spell the device/inode format differently. Directory
    # identity, rather than a content hash, detects an updater activation that
    # exchanged the complete deployment tree while this shell waited for the
    # daemon to stop.
    identity=$(stat -Lc '%d:%i' -- "$PROJECT_ROOT" 2>/dev/null) \
      || identity=$(stat -f '%d:%i' "$PROJECT_ROOT" 2>/dev/null) \
      || return 1
    [ -n "$identity" ] || return 1
    printf '%s\n' "$identity"
}

resolve_direct_updater_privilege() {
    DIRECT_UPDATER_PRIVILEGE=()
    if [ "$(id -u)" -eq 0 ]; then
        if ! verify_direct_updater_ownership; then
            return 1
        fi
        return 0
    fi
    command -v sudo >/dev/null 2>&1 || {
        echo "Error: sudo is required to manage the host updater during this manual deployment run." >&2
        return 1
    }
    if [ "$NON_INTERACTIVE" = "true" ]; then
        DIRECT_UPDATER_PRIVILEGE=(sudo -n)
    else
        DIRECT_UPDATER_PRIVILEGE=(sudo)
    fi
    verify_direct_updater_ownership
}

restart_direct_run_updater() {
    local enable_at_boot="$1"
    DIRECT_UPDATER_WAS_ACTIVE=false
    if [ "$enable_at_boot" = "true" ] \
      && ! ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl enable proto-fleet-updater.service; then
        echo "Error: could not enable proto-fleet-updater.service after the manual deployment run." >&2
        return 1
    fi
    echo "Restoring the host updater after the manual deployment run..."
    if ! ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl restart proto-fleet-updater.service; then
        echo "Error: could not restart proto-fleet-updater.service after the manual deployment run." >&2
        return 1
    fi
    local ready_deadline=$((SECONDS + 60))
    while [ "$SECONDS" -lt "$ready_deadline" ]; do
        if ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
            curl -fsS --max-time 1 --unix-socket "$UPDATER_SOCKET_PATH" \
            http://localhost/v1/status >/dev/null 2>&1 \
          && ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
            systemctl is-active --quiet proto-fleet-updater.service \
          && ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
            systemctl is-enabled --quiet proto-fleet-updater.service; then
            return 0
        fi
        sleep 1
    done
    echo "Error: proto-fleet-updater.service did not become ready after the manual deployment run." >&2
    return 1
}

disable_direct_run_updater() {
    DIRECT_UPDATER_WAS_ACTIVE=false
    # An unavailable systemd manager cannot have a supervised updater to
    # disable. This preserves the supported WSL fallback to copy-command
    # upgrades when systemd has been turned off.
    if ! systemctl show --property=Version --value >/dev/null 2>&1; then
        return 0
    fi
    echo "Disabling the host updater after the manual deployment run..."
    if ! ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl disable --now proto-fleet-updater.service; then
        echo "Error: could not disable proto-fleet-updater.service after the manual deployment run." >&2
        return 1
    fi
    local active_state unit_file_state
    if ! active_state="$(${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl show --property=ActiveState --value \
        proto-fleet-updater.service 2>/dev/null)" \
      || ! unit_file_state="$(${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl show --property=UnitFileState --value \
        proto-fleet-updater.service 2>/dev/null)"; then
        echo "Error: could not verify that proto-fleet-updater.service is stopped and disabled." >&2
        return 1
    fi
    case "$active_state:$unit_file_state" in
        inactive:disabled|failed:disabled) ;;
        *)
            echo "Error: proto-fleet-updater.service did not reach the stopped and disabled state." >&2
            return 1
            ;;
    esac
    return 0
}

deploy_direct_run_updater_fallback() {
    echo "Warning: the host updater did not become ready; redeploying without its socket overlay." >&2
    # A failed readiness check can leave a partially started updater behind.
    # Stop and disable it before the fallback runner mutates the deployment so
    # it cannot race the second Compose transition.
    if ! disable_direct_run_updater; then
        echo "Error: could not stop the unavailable host updater before deploying the copy-command fallback." >&2
        return 1
    fi
    # The successful deployment already persisted every other overlay choice.
    # Use only the non-interactive updater override here: preflight proof has
    # been consumed, and carrying --skip-build would reject this recovery run.
    if ! /bin/bash "$PROJECT_ROOT/run-fleet.sh" \
        --non-interactive --disable-one-click-updates; then
        echo "Error: could not deploy the copy-command fallback after the host updater failed readiness." >&2
        return 1
    fi
    ENABLE_ONE_CLICK_UPDATES=false
    DIRECT_UPDATER_ENV_ROLLBACK_PENDING=false
    echo "One-click upgrades are unavailable; the in-product copy command remains usable."
}

reconcile_direct_run_updater() {
    local run_status="$1"
    local committed_state="$ENABLE_ONE_CLICK_UPDATES"
    [ "${PROTO_FLEET_UPDATER_MANAGED_RUN:-0}" != "1" ] || return 0
    [ "${PROTO_FLEET_INSTALLER_MANAGED_RUN:-0}" != "1" ] || return 0

    # Before Compose mutation, failure leaves the old topology running and
    # therefore restores its updater state. Once teardown begins, topology is
    # uncertain and the persisted desired state is the only safe authority.
    if [ "$run_status" -ne 0 ] \
      && [ "$DEPLOYMENT_MUTATION_STARTED" != "true" ]; then
        committed_state="$ONE_CLICK_UPDATES_WAS_CONFIGURED"
    fi

    if [ "$committed_state" = "true" ]; then
        if [ "$DIRECT_UPDATER_SERVICE_PRESENT" != "true" ]; then
            echo "Error: one-click updates are enabled, but proto-fleet-updater.service cannot be reconciled." >&2
            return 1
        fi
        resolve_direct_updater_privilege || return 1
        # Enforce the persisted contract even when systemd drifted before this
        # run: enabled configuration always ends with a boot-enabled, ready
        # service rather than relying on its state at admission time.
        if ! restart_direct_run_updater true; then
            # Once teardown has started, Fleet may already be running with the
            # updater socket overlay. Commit the supported copy-command mode
            # rather than returning with a broken privileged endpoint.
            if [ "$DEPLOYMENT_MUTATION_STARTED" = "true" ]; then
                deploy_direct_run_updater_fallback || return 1
                return 0
            fi
            return 1
        fi
        return 0
    fi
    if [ "$DIRECT_UPDATER_SERVICE_PRESENT" = "true" ]; then
        resolve_direct_updater_privilege || return 1
        # Likewise, disabled configuration always removes boot enablement and
        # stops a stale process, even when both desired flags were already false.
        disable_direct_run_updater || return 1
    fi
    return 0
}

run_fleet_exit_cleanup() {
    local run_status=$?
    local status="$run_status"
    local env_rollback_failed=false
    trap - EXIT
    if declare -F cleanup_nginx_config_temp >/dev/null 2>&1; then
        cleanup_nginx_config_temp || status=1
    fi
    if declare -F cleanup_env_rewrite >/dev/null 2>&1; then
        cleanup_env_rewrite || status=1
    fi
    if [ "$run_status" -ne 0 ] \
      && [ "$DEPLOYMENT_MUTATION_STARTED" != "true" ] \
      && [ "$DIRECT_UPDATER_ENV_ROLLBACK_PENDING" = "true" ]; then
        echo "Restoring the previous one-click update setting after the failed deployment run..." >&2
        if atomic_set_env_values \
            ENABLE_ONE_CLICK_UPDATES "$ONE_CLICK_UPDATES_WAS_CONFIGURED"; then
            DIRECT_UPDATER_ENV_ROLLBACK_PENDING=false
        else
            echo "Error: could not restore the previous one-click update setting; leaving the updater stopped." >&2
            env_rollback_failed=true
            status=1
        fi
    elif [ "$run_status" -eq 0 ] \
      || [ "$DEPLOYMENT_MUTATION_STARTED" = "true" ]; then
        DIRECT_UPDATER_ENV_ROLLBACK_PENDING=false
    fi
    if [ "$env_rollback_failed" != "true" ] \
      && ! reconcile_direct_run_updater "$run_status"; then
        status=1
    fi
    exit "$status"
}

# The updater daemon owns serialization for its staged preflight and activation
# children. A human running this script against the active deployment must
# instead stop the daemon before any environment or Compose mutation so the
# deployment directory cannot be renamed out from under the shell.
quiesce_updater_for_direct_run() {
    local one_click_was_configured="$1"
    local load_state active_state identity_before_stop identity_after_stop

    [ "${PROTO_FLEET_UPDATER_MANAGED_RUN:-0}" != "1" ] || return 0
    [ "$(uname -s)" = "Linux" ] || return 0
    # A source checkout has no relationship to the packaged installation's
    # global updater. Inspect it only for a packaged deployment, or when this
    # runner explicitly carries updater state that must be serialized.
    if [ ! -f "$VERSION_FILE" ] \
      && [ "$one_click_was_configured" != "true" ] \
      && [ "$ENABLE_ONE_CLICK_UPDATES" != "true" ]; then
        return 0
    fi
    if ! command -v systemctl >/dev/null 2>&1; then
        if [ "$one_click_was_configured" != "true" ] \
          && [ "$ENABLE_ONE_CLICK_UPDATES" != "true" ]; then
            return 0
        fi
        echo "Error: one-click updates are configured, but systemctl is unavailable to serialize this manual run." >&2
        return 1
    fi
    # A host that previously supported the updater may later lose its systemd
    # manager (notably WSL after an init configuration change). The installer
    # must still be able to persist its explicit fallback to copy-command
    # upgrades. Keep enabled runs fail-closed, and distinguish manager absence
    # from a later failure to inspect the updater unit itself.
    if ! systemctl show --property=Version --value >/dev/null 2>&1; then
        if [ "$ENABLE_ONE_CLICK_UPDATES" != "true" ]; then
            return 0
        fi
        echo "Error: one-click updates are enabled, but the systemd manager is unavailable to serialize this manual run." >&2
        return 1
    fi
    if ! load_state=$(systemctl show --property=LoadState --value \
        proto-fleet-updater.service 2>/dev/null); then
        echo "Error: could not inspect proto-fleet-updater.service before the manual deployment run." >&2
        return 1
    fi
    if ! active_state=$(systemctl show --property=ActiveState --value \
      proto-fleet-updater.service 2>/dev/null); then
        echo "Error: could not inspect proto-fleet-updater.service state before the manual deployment run." >&2
        return 1
    fi
    [ "$load_state" = "not-found" ] || DIRECT_UPDATER_SERVICE_PRESENT=true
    # A loaded updater is a global privileged resource even while inactive or
    # failed. Verify its durable ownership before any branch can return and
    # before this deployment can mount its socket directory into Fleet API.
    if [ "$DIRECT_UPDATER_SERVICE_PRESENT" = "true" ]; then
        resolve_direct_updater_privilege || return 1
    fi
    case "$active_state" in
        inactive|failed)
            if [ "$load_state" = "not-found" ] \
              && [ "$ENABLE_ONE_CLICK_UPDATES" = "true" ]; then
                echo "Error: one-click updates are enabled, but proto-fleet-updater.service is not installed." >&2
                return 1
            fi
            return 0
            ;;
        active|activating|reloading|deactivating) ;;
        *)
            echo "Error: proto-fleet-updater.service has unexpected state '$active_state'." >&2
            return 1
            ;;
    esac

    resolve_direct_updater_privilege || return 1
    if ! identity_before_stop=$(deployment_directory_identity); then
        echo "Error: could not identify the current deployment before stopping the host updater." >&2
        return 1
    fi
    echo "Stopping the host updater for this manual deployment run..."
    if ! ${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl stop proto-fleet-updater.service; then
        echo "Error: could not stop proto-fleet-updater.service before mutating the deployment." >&2
        return 1
    fi
    # Arm restoration as soon as systemd reports a successful stop. If the
    # checked inactive-state query fails, EXIT cleanup still repairs service
    # availability rather than leaving the updater accidentally stopped.
    DIRECT_UPDATER_WAS_ACTIVE=true
    if ! identity_after_stop=$(deployment_directory_identity); then
        echo "Error: could not identify the current deployment after stopping the host updater." >&2
        return 1
    fi
    if [ "$identity_before_stop" != "$identity_after_stop" ]; then
        echo "The host updater activated a new deployment while stopping; restarting with the current release runner..."
        exec /bin/bash "$PROJECT_ROOT/run-fleet.sh" "${RUN_FLEET_ORIGINAL_ARGS[@]}"
        echo "Error: could not restart with the current release runner after the deployment changed." >&2
        return 1
    fi
    if ! active_state=$(${DIRECT_UPDATER_PRIVILEGE[@]+"${DIRECT_UPDATER_PRIVILEGE[@]}"} \
        systemctl show --property=ActiveState --value \
        proto-fleet-updater.service 2>/dev/null); then
        echo "Error: could not verify that proto-fleet-updater.service stopped." >&2
        return 1
    fi
    case "$active_state" in
        inactive|failed) return 0 ;;
        *)
            echo "Error: proto-fleet-updater.service remained active; refusing to mutate the deployment." >&2
            return 1
            ;;
    esac
}

# How long the post-start steps wait for fleet-api to finish its migrations.
# 300 x 2s = 10 minutes: a first boot on SD-card-class hardware (Raspberry Pi)
# runs the full migration set plus image load, which comfortably exceeds the
# old 2-4 minute caps and previously left grafana_ro unprovisioned. On a warm
# database these polls return on the first attempt, so the high cap only costs
# time when migrations are genuinely stuck.
FLEET_API_READY_ATTEMPTS="${FLEET_API_READY_ATTEMPTS:-300}"

env_last_value() {
    local key="$1" snapshot_set snapshot_value
    # Compose interpolation gives the invoking process precedence over .env.
    # Validate that same effective value rather than silently checking the
    # persisted fallback while containers receive an exported override. The
    # overlay flags read their pre-initialization snapshot because the script
    # reuses their names for its runtime defaults.
    case "$key" in
        ENABLE_BETA_ALERTS|ENABLE_SYSTEM_MONITORING|ENABLE_TRACING|ENABLE_ONE_CLICK_UPDATES)
            snapshot_set="INVOKING_${key}_SET"
            snapshot_value="INVOKING_${key}_VALUE"
            if [ "${!snapshot_set}" = "x" ]; then
                printf '%s' "${!snapshot_value}"
                return 0
            fi
            compose_env_last_value "$key"
            return $?
            ;;
    esac
    if [ "${!key+x}" = "x" ]; then
        printf '%s' "${!key}"
        return 0
    fi
    compose_env_last_value "$key"
}

parse_compose_env_value() {
    local value="$1"
    local double_quoted='^"([^"\\]*)"[[:space:]]*(#.*)?$'
    local single_quoted="^'([^']*)'[[:space:]]*(#.*)?$"

    # Fork-free trims: these run per line per key lookup, which adds up on
    # SD-card-class hardware.
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    case "$value" in
        \"*)
            [[ "$value" =~ $double_quoted ]] || return 2
            value="${BASH_REMATCH[1]}"
            # Compose expands $ references and decodes backslash escapes in
            # double-quoted values. Reject those uncommon forms rather than
            # validating a different secret from the one Compose will use.
            [[ "$value" != *'$'* && "$value" != *\\* ]] || return 2
            ;;
        \'*)
            [[ "$value" =~ $single_quoted ]] || return 2
            value="${BASH_REMATCH[1]}"
            # An escaped quote cannot match the restricted expression above;
            # other backslashes remain literal under Compose single quotes.
            ;;
        *)
            # Compose treats # as an inline comment only when whitespace
            # separates it from an unquoted value.
            value="${value%%[[:space:]]#*}"
            value="${value%"${value##*[![:space:]]}"}"
            # Unquoted values undergo Compose interpolation.
            [[ "$value" != *'$'* ]] || return 2
            ;;
    esac
    printf '%s' "$value"
}

# Read one key using the literal subset of Compose's documented dotenv
# delimiters, quoting, and comment rules. Returns 1 when absent and 2 when a
# runner-consumed value is present but cannot be interpreted without risking
# divergence from Compose.
compose_env_last_value() {
    local key="$1" line normalized parsed found=false
    local assignment_re="^${key}[[:space:]]*[:=](.*)$"
    local malformed_re="^${key}([[:space:]]|$)"

    [ -e "$ENV_FILE" ] || return 1
    [ -f "$ENV_FILE" ] && [ -r "$ENV_FILE" ] || return 2
    while IFS= read -r line || [ -n "$line" ]; do
        normalized="${line#"${line%%[![:space:]]*}"}"
        case "$normalized" in
            export[[:space:]]*)
                normalized="${normalized#export}"
                normalized="${normalized#"${normalized%%[![:space:]]*}"}"
                ;;
        esac
        if [[ "$normalized" =~ $assignment_re ]]; then
            parsed=$(parse_compose_env_value "${BASH_REMATCH[1]}") || return 2
            found=true
        elif [[ "$normalized" =~ $malformed_re ]]; then
            return 2
        fi
    done < "$ENV_FILE"

    [ "$found" = "true" ] || return 1
    printf '%s' "$parsed"
}

validate_runner_env_value_syntax() {
    local key status
    for key in "$@"; do
        if compose_env_last_value "$key" >/dev/null; then
            status=0
        else
            status=$?
        fi
        [ "$status" -eq 0 ] && continue
        [ "$status" -eq 1 ] && continue
        echo "Error: $key in $ENV_FILE uses unsupported Compose dotenv syntax; use a literal same-line value (single-quote literal dollar signs)." >&2
        return 1
    done
}

validate_runner_env_values() {
    local keys=(
        COMPOSE_PROJECT_NAME
        ENABLE_BETA_ALERTS
        ENABLE_SYSTEM_MONITORING
        ENABLE_TRACING
        ENABLE_ONE_CLICK_UPDATES
        FLEET_PROFILE
        DB_USERNAME
        DB_PASSWORD
        DB_NAME
        AUTH_CLIENT_SECRET_KEY
        ENCRYPT_SERVICE_MASTER_KEY
        SESSION_COOKIE_SECURE
    )

    [ -e "$ENV_FILE" ] || return 0
    if [ ! -f "$ENV_FILE" ] || [ ! -r "$ENV_FILE" ]; then
        echo "Error: $ENV_FILE must be a readable regular file." >&2
        return 1
    fi
    validate_runner_env_value_syntax "${keys[@]}"
}

env_has_nonempty_value() {
    local value
    value=$(env_last_value "$1") || return 1
    [ -n "$value" ]
}

# .env-scoped counterpart: whether a key still needs persisting, ignoring any process-level override.
dotenv_has_nonempty_value() {
    local value
    value=$(compose_env_last_value "$1") || return 1
    [ -n "$value" ]
}

env_boolean_is_true() {
    local key="$1" value read_status
    value=$(env_last_value "$key")
    read_status=$?
    if [ "$read_status" -ne 0 ]; then
        if [ "$read_status" -eq 1 ]; then
            return 1
        fi
        echo "Error: $key in $ENV_FILE uses unsupported or malformed Compose dotenv syntax." >&2
        exit 1
    fi
    value=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')
    case "$value" in
        true) return 0 ;;
        false|'') return 1 ;;
        *)
            echo "Error: the effective $key value must be true or false (process environment takes precedence over $ENV_FILE)." >&2
            exit 1
            ;;
    esac
}

dotenv_boolean_is_true() {
    local key="$1" value read_status
    value=$(compose_env_last_value "$key")
    read_status=$?
    if [ "$read_status" -ne 0 ]; then
        if [ "$read_status" -eq 1 ]; then
            return 1
        fi
        echo "Error: $key in $ENV_FILE uses unsupported or malformed Compose dotenv syntax." >&2
        exit 1
    fi
    value=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')
    case "$value" in
        true) return 0 ;;
        false|'') return 1 ;;
        *)
            echo "Error: persisted $key in $ENV_FILE must be true or false." >&2
            exit 1
            ;;
    esac
}

resolve_compose_project_name() {
    local project_name persisted_status

    if [ -n "${COMPOSE_PROJECT_NAME:-}" ]; then
        project_name="$COMPOSE_PROJECT_NAME"
    else
        project_name=$(compose_env_last_value COMPOSE_PROJECT_NAME)
        persisted_status=$?
        case "$persisted_status" in
            0)
                if [ -z "$project_name" ]; then
                    project_name=$(basename "$PROJECT_ROOT")
                fi
                ;;
            1)
                # Preserve Compose's historical per-directory project identity
                # when neither the process nor deployment .env selects one.
                project_name=$(basename "$PROJECT_ROOT")
                ;;
            *)
                echo "Error: COMPOSE_PROJECT_NAME in $ENV_FILE uses unsupported or malformed Compose dotenv syntax." >&2
                return 1
                ;;
        esac
    fi

    # The same value is also used to select volumes below, so reject names
    # outside Compose's documented grammar before any destructive operation.
    if [[ ! "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]]; then
        echo "Error: COMPOSE_PROJECT_NAME must start with a lowercase letter or digit and contain only lowercase letters, digits, hyphens, and underscores." >&2
        return 1
    fi

    printf '%s' "$project_name"
}

resolve_release_image_tag() {
    local count tag

    if [ ! -f "$VERSION_FILE" ]; then
        if [ "$PREFLIGHT_ONLY" = "true" ] || [ "$SKIP_BUILD" = "true" ]; then
            echo "Error: updater preflight and activation require packaged release metadata at $VERSION_FILE." >&2
            return 1
        fi
        # Source-tree and local development installs do not carry version.txt.
        # Keep their historical mutable tag; release bundles are pinned below.
        printf 'latest'
        return 0
    fi

    count=$(grep -c '^version:' "$VERSION_FILE" 2>/dev/null || true)
    if [ "$count" -ne 1 ]; then
        echo "Error: $VERSION_FILE must contain exactly one version entry." >&2
        return 1
    fi
    tag=$(sed -n -E 's/^version:[[:space:]]*([^[:space:]]+)[[:space:]]*$/\1/p' "$VERSION_FILE")
    if [[ ! "$tag" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]] || [ "${#tag}" -gt 128 ]; then
        echo "Error: the packaged release version is not a valid Docker image tag." >&2
        return 1
    fi
    if [ "$tag" = "latest" ]; then
        echo "Error: packaged releases must not use the shared latest image tag." >&2
        return 1
    fi
    printf '%s' "$tag"
}

# A hand-edited .env may lack a trailing newline; a bare append would glue the key onto the last line.
append_env_line() {
    if [ -s "$ENV_FILE" ] && [ -n "$(tail -c1 "$ENV_FILE")" ]; then
        echo >> "$ENV_FILE"
    fi
    echo "$1" >> "$ENV_FILE"
}

# Satisfies the tracing overlay's ${DD_HOSTNAME:?}; env_has_nonempty_value already reads the effective Compose value, so only default when that is empty.
ensure_dd_hostname() {
    if env_has_nonempty_value DD_HOSTNAME; then
        return 0
    fi
    local detected
    detected=$(hostname 2>/dev/null || true)
    if [ -z "$detected" ]; then
        echo "Error: could not read this host's hostname; set DD_HOSTNAME in $ENV_FILE and re-run with --enable-tracing." >&2
        exit 1
    fi
    export DD_HOSTNAME="$detected"
    DD_HOSTNAME_DEFAULTED=true
}

usage() {
    cat <<'EOF'
Usage: run-fleet.sh [options]

Options:
  --enable-beta-alerts   Layer in the beta alerts sidecar
                                (Grafana, polling TimescaleDB and running
                                the built-in Alertmanager). Off by
                                default. Can also be enabled by setting
                                ENABLE_BETA_ALERTS=true in the .env file.
  --enable-system-monitoring   Layer in host system monitoring (CPU/RAM/disk
                                alert rules and a slow-query dashboard).
                                Requires --enable-beta-alerts. Off by
                                default. Can also be enabled by setting
                                ENABLE_SYSTEM_MONITORING=true in the .env
                                file.
  --enable-tracing              Layer in request tracing (fleet-api spans
                                forwarded to Datadog APM via an OTel
                                collector sidecar). Requires DD_API_KEY in
                                the .env file. Off by default. Can also be
                                enabled by setting ENABLE_TRACING=true in
                                the .env file.
  --enable-one-click-updates    Connect fleet-api to the host updater Unix
                                socket. The installer sets this only after
                                the systemd updater starts successfully.
  --disable-one-click-updates   Disconnect fleet-api from the host updater.
                                The installer uses this when host bootstrap
                                is unavailable or fails.
  --non-interactive              Reuse complete persisted configuration and
                                fail instead of prompting. Intended for the
                                host updater, not first-time setup.
  --preflight-only               Validate configuration, load release images,
                                and build the new stack without stopping the
                                running deployment.
  --skip-build                   Skip image preparation because a successful
                                preflight already prepared this exact release.
  -h, --help                    Show this help and exit.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --enable-beta-alerts)
            ENABLE_BETA_ALERTS=true
            shift
            ;;
        --enable-system-monitoring)
            ENABLE_SYSTEM_MONITORING=true
            shift
            ;;
        --enable-tracing)
            ENABLE_TRACING=true
            shift
            ;;
        --enable-one-click-updates)
            ONE_CLICK_UPDATES_OVERRIDE=true
            shift
            ;;
        --disable-one-click-updates)
            ONE_CLICK_UPDATES_OVERRIDE=false
            shift
            ;;
        --non-interactive)
            NON_INTERACTIVE=true
            shift
            ;;
        --preflight-only)
            PREFLIGHT_ONLY=true
            shift
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            break
            ;;
        *)
            echo "Error: unknown argument: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

if [ "$PREFLIGHT_ONLY" = "true" ] && [ "$SKIP_BUILD" = "true" ]; then
    echo "Error: --preflight-only and --skip-build cannot be combined." >&2
    exit 1
fi

validate_runner_env_values || exit 1
if [[ ! "$FLEET_API_READY_ATTEMPTS" =~ ^[1-9][0-9]*$ ]]; then
    echo "Error: FLEET_API_READY_ATTEMPTS must be a positive integer." >&2
    exit 1
fi
FLEET_COMPOSE_PROJECT_NAME=$(resolve_compose_project_name) || exit 1
RELEASE_IMAGE_TAG=$(resolve_release_image_tag) || exit 1
FLEET_API_IMAGE="proto-fleet-api:${RELEASE_IMAGE_TAG}"
FLEET_CLIENT_IMAGE="proto-fleet-client:${RELEASE_IMAGE_TAG}"
TIMESCALEDB_IMAGE="proto-fleet-timescaledb:${RELEASE_IMAGE_TAG}"
TIMESCALEDB_HA_IMAGE="proto-fleet-timescaledb-ha:${RELEASE_IMAGE_TAG}"

if [ "$SKIP_BUILD" = "true" ] && [ ! -f "$PREFLIGHT_MARKER" ]; then
    echo "Error: --skip-build requires a successful preflight for this deployment." >&2
    exit 1
fi

# Also honor persisted overlay state. Use the last exact-key assignment, as
# Docker Compose does, and accept quoted/case-insensitive boolean values.
if env_boolean_is_true ENABLE_BETA_ALERTS; then
    ENABLE_BETA_ALERTS=true
fi
if env_boolean_is_true ENABLE_SYSTEM_MONITORING; then
    ENABLE_SYSTEM_MONITORING=true
fi
if env_boolean_is_true ENABLE_TRACING; then
    ENABLE_TRACING=true
fi
ONE_CLICK_UPDATES_WAS_CONFIGURED=false
if dotenv_boolean_is_true ENABLE_ONE_CLICK_UPDATES; then
    ONE_CLICK_UPDATES_WAS_CONFIGURED=true
fi
if env_boolean_is_true ENABLE_ONE_CLICK_UPDATES; then
    ENABLE_ONE_CLICK_UPDATES=true
fi
if [ -n "$ONE_CLICK_UPDATES_OVERRIDE" ]; then
    ENABLE_ONE_CLICK_UPDATES="$ONE_CLICK_UPDATES_OVERRIDE"
fi
if [ "$PREFLIGHT_ONLY" = "true" ] \
  && [ "${PROTO_FLEET_UPDATER_MANAGED_RUN:-0}" != "1" ] \
  && [ "$ONE_CLICK_UPDATES_WAS_CONFIGURED" != "$ENABLE_ONE_CLICK_UPDATES" ]; then
    echo "Error: --preflight-only cannot change one-click update state for the active deployment; run the complete deployment with the same enable/disable option." >&2
    exit 1
fi
# System monitoring rides the alerts stack (the in-process metrics writer,
# Grafana rule evaluation, and webhook delivery are all alerts-gated), so it
# cannot run alone.
if [ "$ENABLE_SYSTEM_MONITORING" = "true" ] && [ "$ENABLE_BETA_ALERTS" != "true" ]; then
    echo "Error: --enable-system-monitoring requires the beta alerts stack." >&2
    echo "       Pass --enable-beta-alerts as well, or set ENABLE_BETA_ALERTS=true in $ENV_FILE." >&2
    exit 1
fi

# Optional settings are interpreted by the runner only while their matching
# overlay is active. Preserve Compose's full dotenv surface for dormant values,
# while continuing to fail closed before any state changes when they are used.
if [ "$ENABLE_BETA_ALERTS" = "true" ]; then
    validate_runner_env_value_syntax \
        GRAFANA_ADMIN_PASSWORD \
        GRAFANA_DB_USERNAME \
        GRAFANA_DB_PASSWORD \
        GRAFANA_SECRET_KEY \
        FLEET_ALERTS_WEBHOOK_TOKEN \
        FLEET_ALERTS_GRAFANA_TOKEN || exit 1
fi

# Validate tracing prerequisites before the overlay is layered: its ${DD_API_KEY:?}
# interpolation would otherwise abort every compose command, even `compose down`.
if [ "$ENABLE_TRACING" = "true" ]; then
    validate_runner_env_value_syntax DD_API_KEY DD_HOSTNAME || exit 1
    if [ ! -f "$COMPOSE_TRACING_FILE" ]; then
        echo "Error: --enable-tracing was passed but $COMPOSE_TRACING_FILE is missing." >&2
        exit 1
    fi
    if ! env_has_nonempty_value DD_API_KEY; then
        echo "Error: --enable-tracing requires DD_API_KEY (a Datadog API key) in $ENV_FILE." >&2
        exit 1
    fi
    # Before layering too: the overlay's ${DD_HOSTNAME:?} would abort the `compose down` the volume-reinit prompt runs.
    ensure_dd_hostname
fi

if [ "$ENABLE_ONE_CLICK_UPDATES" = "true" ] && [ ! -f "$COMPOSE_UPDATER_FILE" ]; then
    echo "Error: one-click updates are enabled but $COMPOSE_UPDATER_FILE is missing." >&2
    exit 1
fi

# Install cleanup before quiescing the updater so every later validation,
# configuration, build, and Compose failure restores the service.
trap run_fleet_exit_cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
if ! quiesce_updater_for_direct_run "$ONE_CLICK_UPDATES_WAS_CONFIGURED"; then
    exit 1
fi

if [ "$PREFLIGHT_ONLY" = "true" ]; then
    # A failed retry must not leave an older success marker reusable.
    if ! rm -f "$PREFLIGHT_MARKER"; then
        echo "Error: could not clear the previous preflight marker at $PREFLIGHT_MARKER." >&2
        exit 1
    fi
fi

refresh_compose_files() {
    COMPOSE_FILES=(-f "$COMPOSE_FILE")
    if [ "$ENABLE_BETA_ALERTS" = "true" ] && [ -f "$COMPOSE_ALERTS_FILE" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_ALERTS_FILE")
    fi
    # After alerts so its grafana mounts shadow the rules tombstone and the
    # dashboards placeholder inside the alerts overlay's provisioning mount.
    if [ "$ENABLE_SYSTEM_MONITORING" = "true" ] && [ -f "$COMPOSE_SYSTEM_MONITORING_FILE" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_SYSTEM_MONITORING_FILE")
    fi
    if [ "$ENABLE_TRACING" = "true" ] && [ -f "$COMPOSE_TRACING_FILE" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_TRACING_FILE")
    fi
    if [ "$ENABLE_ONE_CLICK_UPDATES" = "true" ]; then
        COMPOSE_FILES+=(-f "$COMPOSE_UPDATER_FILE")
    fi
}
refresh_compose_files

# Layered compose interpolation: host profile file first, operator .env
# last so any key set in .env overrides the profile. Passing --env-file
# disables compose's automatic ./.env loading, so .env must be passed
# explicitly whenever it exists.
refresh_compose_env_args() {
    COMPOSE_ENV_ARGS=()
    local profile profile_file
    profile=$(env_last_value FLEET_PROFILE 2>/dev/null || true)
    profile=$(printf '%s' "$profile" | tr '[:upper:]' '[:lower:]')
    if [ -n "$profile" ]; then
        profile_file="$PROJECT_ROOT/profiles/${profile}.env"
        if [[ "$profile" =~ ^[a-z]+$ ]] && [ -f "$profile_file" ]; then
            COMPOSE_ENV_ARGS+=(--env-file "$profile_file")
        else
            echo "Warning: FLEET_PROFILE='$profile' does not match a profile in $PROJECT_ROOT/profiles/; using default configuration." >&2
        fi
    fi
    if [ -f "$ENV_FILE" ]; then
        COMPOSE_ENV_ARGS+=(--env-file "$ENV_FILE")
    fi
}
refresh_compose_env_args

compose() {
    # Resolve this once so Compose operations, volume detection, and printed
    # recovery commands all target the same project. Packaged updater paths
    # keep the implicit `deployment` identity because both their staged and
    # active release directories have that basename.
    docker compose --project-name "$FLEET_COMPOSE_PROJECT_NAME" \
        ${COMPOSE_ENV_ARGS[@]+"${COMPOSE_ENV_ARGS[@]}"} "${COMPOSE_FILES[@]}" "$@"
}

verify_release_image_references() {
    local images required ha_compose="$PROJECT_ROOT/ha/compose.yaml"
    images=$(compose config --images) || {
        echo "Error: could not list Docker Compose image references." >&2
        return 1
    }

    for required in "$FLEET_API_IMAGE" "$FLEET_CLIENT_IMAGE" "$TIMESCALEDB_IMAGE"; do
        if ! printf '%s\n' "$images" | grep -Fxq "$required"; then
            echo "Error: the packaged Compose model is not pinned to required image $required." >&2
            return 1
        fi
    done

    # A release bundle also carries the HA database image in the same archive.
    # Its separate Compose profile must stay pinned even though this runner does
    # not activate that profile itself.
    if [ -f "$VERSION_FILE" ]; then
        if [ ! -f "$ha_compose" ] || ! grep -Fq "image: $TIMESCALEDB_HA_IMAGE" "$ha_compose"; then
            echo "Error: the packaged HA Compose model is not pinned to required image $TIMESCALEDB_HA_IMAGE." >&2
            return 1
        fi
        for required in \
            'proto-fleet-api:latest' \
            'proto-fleet-client:latest' \
            'proto-fleet-timescaledb:latest'; do
            if printf '%s\n' "$images" | grep -Fxq "$required"; then
                echo "Error: release Compose configuration still references shared image $required." >&2
                return 1
            fi
        done
        if grep -Fq 'proto-fleet-timescaledb-ha:latest' "$ha_compose"; then
            echo "Error: release HA Compose configuration still references the shared latest image tag." >&2
            return 1
        fi
    fi
}

validate_timescaledb_archive_tags() {
    local manifest required forbidden
    manifest=$(gunzip -c "$TSDB_IMAGE" | tar -xOf - manifest.json 2>/dev/null) || {
        echo "Error: $TSDB_IMAGE does not contain a readable Docker image manifest." >&2
        return 1
    }

    for required in "$TIMESCALEDB_IMAGE" "$TIMESCALEDB_HA_IMAGE"; do
        if ! printf '%s\n' "$manifest" | grep -Fq "\"$required\""; then
            echo "Error: $TSDB_IMAGE does not contain required image $required." >&2
            return 1
        fi
    done
    if [ "$RELEASE_IMAGE_TAG" != "latest" ]; then
        for forbidden in \
            'proto-fleet-timescaledb:latest' \
            'proto-fleet-timescaledb-ha:latest'; do
            if printf '%s\n' "$manifest" | grep -Fq "\"$forbidden\""; then
                echo "Error: $TSDB_IMAGE contains forbidden shared image $forbidden." >&2
                return 1
            fi
        done
    fi
}

is_managed_release_image_tag() {
    local tag="$1"
    [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9._-]*)?$ ]] ||
        [[ "$tag" =~ ^nightly-[0-9]{8}-[0-9a-f]{12}$ ]]
}

is_proto_fleet_image_reference() {
    local image="$1" repository
    for repository in "${PROTO_FLEET_IMAGE_REPOSITORIES[@]}"; do
        case "$image" in
            "$repository":*) return 0 ;;
        esac
    done
    return 1
}

prune_obsolete_release_images() {
    local repository image tag image_id containers
    local candidates=()
    local protected_tags=(latest "$RELEASE_IMAGE_TAG" "${PREVIOUS_RELEASE_IMAGE_TAGS[@]}")

    # Source-tree runs intentionally share mutable :latest tags and must not
    # participate in packaged-release retention.
    [ "$RELEASE_IMAGE_TAG" != "latest" ] || return 0
    [ "$RELEASE_IMAGE_CLEANUP_SAFE" = "true" ] || return 0

    # Collect the complete candidate set before deleting anything. If Docker
    # cannot enumerate one reserved repository, fail safe and leave every tag
    # untouched rather than making a partial retention decision.
    for repository in "${PROTO_FLEET_IMAGE_REPOSITORIES[@]}"; do
        local listed_images
        if ! listed_images=$(docker image ls --format '{{.Repository}}:{{.Tag}}' "$repository" 2>/dev/null); then
            echo "Warning: could not inspect $repository images; skipping Proto Fleet release image cleanup." >&2
            return 0
        fi
        while IFS= read -r image; do
            [ -n "$image" ] || continue
            [ "${image##*:}" != "<none>" ] || continue
            candidates+=("$image")
        done <<< "$listed_images"
    done

    # A container may reference only three images from a four-image release
    # (the standard and HA database images are mutually exclusive). Protect
    # the whole release tag whenever any running or stopped container uses it.
    for image in "${candidates[@]}"; do
        tag="${image##*:}"
        if [ "$tag" = "latest" ] || [ "$tag" = "$RELEASE_IMAGE_TAG" ] ||
            ! is_managed_release_image_tag "$tag"; then
            continue
        fi
        if ! image_id=$(docker image inspect --format '{{.Id}}' "$image" 2>/dev/null); then
            echo "Warning: could not inspect obsolete Proto Fleet image $image; retaining its release tag." >&2
            protected_tags+=("$tag")
            continue
        fi
        if ! containers=$(docker container ls --all --quiet --filter "ancestor=$image_id" 2>/dev/null); then
            echo "Warning: could not inspect containers using $image; retaining its release tag." >&2
            protected_tags+=("$tag")
            continue
        fi
        if [ -n "$containers" ]; then
            protected_tags+=("$tag")
        fi
    done

    for image in "${candidates[@]}"; do
        tag="${image##*:}"
        is_managed_release_image_tag "$tag" || continue
        local protected=false protected_tag
        for protected_tag in "${protected_tags[@]}"; do
            if [ "$tag" = "$protected_tag" ]; then
                protected=true
                break
            fi
        done
        [ "$protected" = "false" ] || continue

        if ! docker image rm "$image" >/dev/null 2>&1; then
            # Retention is best-effort housekeeping. A cleanup race or daemon
            # error must not turn a verified upgrade into an outage.
            echo "Warning: could not remove obsolete Proto Fleet image $image; continuing." >&2
        fi
    done
}

capture_previous_release_image_tags() {
    local images image tag existing_tag found_target=false
    PREVIOUS_RELEASE_IMAGE_TAGS=()
    RELEASE_IMAGE_CLEANUP_SAFE=true

    [ "$RELEASE_IMAGE_TAG" != "latest" ] || return 0
    if ! images=$(docker container ls --all \
        --filter "label=com.docker.compose.project=$FLEET_COMPOSE_PROJECT_NAME" \
        --format '{{.Image}}' 2>/dev/null); then
        echo "Warning: could not identify the active Proto Fleet release; skipping release image cleanup." >&2
        RELEASE_IMAGE_CLEANUP_SAFE=false
        return 0
    fi
    while IFS= read -r image; do
        is_proto_fleet_image_reference "$image" || continue
        tag="${image##*:}"
        is_managed_release_image_tag "$tag" || continue
        for existing_tag in "${PREVIOUS_RELEASE_IMAGE_TAGS[@]}"; do
            [ "$existing_tag" != "$tag" ] || continue 2
        done
        PREVIOUS_RELEASE_IMAGE_TAGS+=("$tag")
        [ "$tag" != "$RELEASE_IMAGE_TAG" ] || found_target=true
    done <<< "$images"

    if [ "${#PREVIOUS_RELEASE_IMAGE_TAGS[@]}" -eq 0 ]; then
        echo "Warning: no active Proto Fleet release tag was found; skipping release image cleanup." >&2
        RELEASE_IMAGE_CLEANUP_SAFE=false
    elif [ "$found_target" = "true" ]; then
        # On a retry, target containers may already have replaced every source
        # of the previous known-good tag. Even a second, stale project tag does
        # not prove which intervening release is the recovery target.
        echo "Warning: the target Proto Fleet release is already active; skipping release image cleanup because the previous tag cannot be identified safely." >&2
        RELEASE_IMAGE_CLEANUP_SAFE=false
    fi
}

# Resolved once and shared by digest computation and manifest verification.
SHA256_CMD=()

resolve_sha256_cmd() {
    [ "${#SHA256_CMD[@]}" -eq 0 ] || return 0
    if command -v sha256sum >/dev/null 2>&1; then
        SHA256_CMD=(sha256sum)
    elif command -v shasum >/dev/null 2>&1; then
        SHA256_CMD=(shasum -a 256)
    else
        echo "Error: SHA-256 support requires sha256sum or shasum." >&2
        return 1
    fi
}

sha256() {
    local output
    resolve_sha256_cmd || return 1
    output=$("${SHA256_CMD[@]}" "$@") || return 1
    printf '%s\n' "$output" | awk '{print $1}'
}

# Keep the release scope shared by the type and path-set checks. These narrow
# exclusions are operator-owned runtime state; selected files are fingerprinted
# separately by write_preflight_metadata.
find_release_entries() {
    find . \
        ! -path './.env' \
        ! -path './.update-preflight-complete' \
        ! -path './.update-preflight-complete.tmp.*' \
        ! -path './client/nginx.conf' \
        ! -path './ssl/*' \
        ! -path './server/influx_config/.env' \
        ! -path './ha/node.env' \
        "$@"
}

find_immutable_release_entries() {
    find_release_entries ! -path './deployment-manifest.sha256' "$@"
}

validate_generated_runtime_entries() {
    local nginx_config="$PROJECT_ROOT/client/nginx.conf"

    # Generated state stays outside the immutable manifest, but excluding its
    # contents must not also permit a symlink, FIFO, device, or directory at
    # the path the privileged runner replaces.
    if [ -L "$nginx_config" ] || { [ -e "$nginx_config" ] && [ ! -f "$nginx_config" ]; }; then
        echo "Error: generated nginx config $nginx_config must be a regular, non-symlink file when present." >&2
        return 1
    fi
}

verify_release_manifest() {
    local manifest_name="${RELEASE_MANIFEST_FILE##*/}" manifest_paths current_paths unsupported_entries
    if [ ! -f "$RELEASE_MANIFEST_FILE" ]; then
        echo "Error: upgrade preflight requires the immutable release manifest at $RELEASE_MANIFEST_FILE." >&2
        return 1
    fi
    validate_generated_runtime_entries || return 1

    # Validate and extract the GNU sha256sum path column before asking a
    # checksum utility to open anything. Release paths must stay below the
    # deployment root, and the sorted path set must exactly match the current
    # immutable files so added provisioning/config files cannot bypass the
    # manifest by simply being absent from it.
    if ! manifest_paths=$(awk '
        {
            digest = substr($0, 1, 64)
            separator = substr($0, 65, 2)
            path = substr($0, 67)
            if (length(digest) != 64 || digest !~ /^[0-9a-fA-F]+$/ ||
                separator != "  " || path !~ /^\.\// ||
                path ~ /(^|\/)\.\.(\/|$)/) {
                exit 1
            }
            print path
        }
    ' "$RELEASE_MANIFEST_FILE"); then
        echo "Error: immutable release manifest has an invalid entry." >&2
        return 1
    fi

    unsupported_entries=$(cd "$PROJECT_ROOT" && \
        find_release_entries ! -type f ! -type d -print) || return 1
    if [ -n "$unsupported_entries" ]; then
        echo "Error: immutable release contains unsupported non-regular entries:" >&2
        printf '  %s\n' "$unsupported_entries" >&2
        return 1
    fi

    current_paths=$(cd "$PROJECT_ROOT" && \
        find_immutable_release_entries -type f -print | LC_ALL=C sort) || return 1
    if [ "$manifest_paths" != "$current_paths" ]; then
        echo "Error: immutable release file set does not match the packaged manifest." >&2
        return 1
    fi

    resolve_sha256_cmd || return 1
    (cd "$PROJECT_ROOT" && "${SHA256_CMD[@]}" -c "$manifest_name" >/dev/null)
}

prepared_images_fingerprint() {
    local images image image_id metadata=""
    images=$(compose config --images) || {
        echo "Error: could not list the images required by this release." >&2
        return 1
    }
    images=$(printf '%s\n' "$images" | sort -u)
    if [ -z "$images" ]; then
        echo "Error: Docker Compose did not report any release images." >&2
        return 1
    fi

    for image in $images; do
        image_id=$(docker image inspect --format '{{.Id}}' "$image" 2>/dev/null) || {
            echo "Error: prepared image is missing before activation: $image" >&2
            return 1
        }
        [ -n "$image_id" ] || {
            echo "Error: Docker returned an empty image ID for $image." >&2
            return 1
        }
        metadata="${metadata}${image}=${image_id}"$'\n'
    done
    printf '%s' "$metadata" | sha256
}

project_relative_path() {
    case "$1" in
        "$PROJECT_ROOT"/*) printf '%s' "${1#"$PROJECT_ROOT"/}" ;;
        *) printf '%s' "$1" ;;
    esac
}

compose_config_hash() {
    local rendered hash status
    rendered=$(mktemp) || {
        echo "Error: could not create a temporary file for Compose validation." >&2
        return 1
    }
    if ! compose config > "$rendered"; then
        rm -f "$rendered"
        echo "Error: could not render Docker Compose configuration for preflight verification." >&2
        return 1
    fi

    # Compose resolves bind/build paths to the staging directory. The updater
    # renames that directory into place between preflight and activation, so
    # redact only that literal prefix before hashing the normalized model.
    hash=$(awk -v root="$PROJECT_ROOT" '
        {
            line = $0
            normalized = ""
            while ((position = index(line, root)) > 0) {
                normalized = normalized substr(line, 1, position - 1) "<PROJECT_ROOT>"
                line = substr(line, position + length(root))
            }
            print normalized line
        }
    ' "$rendered" | sha256)
    status=$?
    rm -f "$rendered"
    [ "$status" -eq 0 ] || return "$status"
    printf '%s' "$hash"
}

write_preflight_metadata() {
    local config_hash digest file relative index

    for file in "$VERSION_FILE" "$RELEASE_MANIFEST_FILE" "$ENV_FILE" "$TSDB_IMAGE" "$PROJECT_ROOT/run-fleet.sh" "$NGINX_CONF_DIR/nginx.conf"; do
        if [ ! -f "$file" ]; then
            echo "Error: upgrade preflight metadata requires $file." >&2
            return 1
        fi
    done

    config_hash=$(compose_config_hash) || return 1
    printf 'format=1\n'
    digest=$(sha256 "$VERSION_FILE") || return 1
    printf 'version_file_sha256=%s\n' "$digest"
    digest=$(sha256 "$RELEASE_MANIFEST_FILE") || return 1
    printf 'deployment_manifest_sha256=%s\n' "$digest"
    digest=$(sha256 "$PROJECT_ROOT/run-fleet.sh") || return 1
    printf 'runner_sha256=%s\n' "$digest"
    digest=$(sha256 "$NGINX_CONF_DIR/nginx.conf") || return 1
    printf 'nginx_config_sha256=%s\n' "$digest"
    if [ "$PROTOCOL_MODE" = "https" ]; then
        digest=$(sha256 "$SSL_CERT") || return 1
        printf 'ssl_certificate_sha256=%s\n' "$digest"
        digest=$(sha256 "$SSL_KEY") || return 1
        printf 'ssl_private_key_sha256=%s\n' "$digest"
    else
        printf 'ssl_state=disabled\n'
    fi
    printf 'compose_config_sha256=%s\n' "$config_hash"
    printf 'feature_state=alerts:%s,system_monitoring:%s,tracing:%s,protocol:%s\n' \
        "$ENABLE_BETA_ALERTS" "$ENABLE_SYSTEM_MONITORING" "$ENABLE_TRACING" "$PROTOCOL_MODE"

    for ((index = 1; index < ${#COMPOSE_FILES[@]}; index += 2)); do
        file="${COMPOSE_FILES[$index]}"
        relative=$(project_relative_path "$file")
        digest=$(sha256 "$file") || return 1
        printf 'compose_file=%s:%s\n' "$relative" "$digest"
    done

    for ((index = 1; index < ${#COMPOSE_ENV_ARGS[@]}; index += 2)); do
        file="${COMPOSE_ENV_ARGS[$index]}"
        relative=$(project_relative_path "$file")
        digest=$(sha256 "$file") || return 1
        printf 'compose_env_file=%s:%s\n' "$relative" "$digest"
    done

    digest=$(sha256 "$TSDB_IMAGE") || return 1
    printf 'timescaledb_archive_sha256=%s\n' "$digest"
}

preflight_fingerprint() {
    local metadata
    metadata=$(write_preflight_metadata) || return 1
    printf '%s' "$metadata" | sha256
}

record_preflight_marker() {
    local prepared_fingerprint="$1" current_fingerprint image_fingerprint temporary_marker
    if ! verify_release_manifest; then
        echo "Error: release or configuration changed during preflight; immutable release files no longer match." >&2
        return 1
    fi
    current_fingerprint=$(preflight_fingerprint) || return 1
    if [ "$current_fingerprint" != "$prepared_fingerprint" ]; then
        echo "Error: release or configuration changed during preflight; run preflight again." >&2
        return 1
    fi
    image_fingerprint=$(prepared_images_fingerprint) || return 1
    temporary_marker=$(umask 077; mktemp "$PREFLIGHT_MARKER.tmp.XXXXXX") || return 1
    if ! printf 'proto-fleet-preflight-v2:%s:%s\n' "$prepared_fingerprint" "$image_fingerprint" > "$temporary_marker"; then
        rm -f "$temporary_marker"
        return 1
    fi
    if ! chmod 600 "$temporary_marker"; then
        rm -f "$temporary_marker"
        return 1
    fi
    if ! mv -f "$temporary_marker" "$PREFLIGHT_MARKER"; then
        rm -f "$temporary_marker"
        return 1
    fi
}

verify_preflight_marker() {
    local actual expected image_fingerprint
    if [ ! -f "$PREFLIGHT_MARKER" ]; then
        echo "Error: --skip-build requires a successful preflight for this deployment." >&2
        return 1
    fi

    if ! verify_release_manifest || \
        ! expected=$(preflight_fingerprint) || \
        ! image_fingerprint=$(prepared_images_fingerprint); then
        if ! rm -f "$PREFLIGHT_MARKER"; then
            echo "Error: preflight verification failed and the stale marker could not be removed." >&2
        fi
        echo "Error: release, prepared images, or configuration changed after preflight; run preflight again before activation." >&2
        return 1
    fi
    actual=$(cat "$PREFLIGHT_MARKER") || return 1
    if [ "$actual" != "proto-fleet-preflight-v2:$expected:$image_fingerprint" ]; then
        if ! rm -f "$PREFLIGHT_MARKER"; then
            echo "Error: preflight inputs changed and the stale marker could not be removed." >&2
            return 1
        fi
        echo "Error: release or configuration changed after preflight; run preflight again before activation." >&2
        return 1
    fi
}

# Poll psql until the query returns true; caller owns the warning.
wait_for_psql_true() {
    local query="$1" attempt result
    for attempt in $(seq 1 "$FLEET_API_READY_ATTEMPTS"); do
        result=$(compose exec -T timescaledb \
            bash -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -tAc \"$query\"" \
            2>/dev/null | tr -d '[:space:]')
        if [ "$result" = "t" ]; then
            return 0
        fi
        sleep 2
    done
    return 1
}
SSL_DIR="$PROJECT_ROOT/ssl"
SSL_CERT="$SSL_DIR/cert.pem"
SSL_KEY="$SSL_DIR/key.pem"
NGINX_CONF_DIR="$PROJECT_ROOT/client"

# Protocol mode: "https" or "http"
PROTOCOL_MODE="http"

# ----------------------------------------------------------------------------
# Helper Functions
# ----------------------------------------------------------------------------

# Validate if a string is valid Base64 and decodes to 32 bytes
validate_base64_key() {
    local input="$1"

    # Try to decode the Base64 input to a temporary file
    local temp_file=$(mktemp)
    if ! echo "$input" | base64 -d > "$temp_file" 2>/dev/null; then
        rm -f "$temp_file"
        return 1  # Not valid Base64
    fi

    # Check the byte length of the decoded data
    local byte_length=$(wc -c < "$temp_file")
    rm -f "$temp_file"

    if [ "$byte_length" -ne 32 ]; then
        return 2  # Not 32 bytes
    fi

    return 0  # Valid
}

validate_grafana_db_role_names() {
    local grafana_user="$1" db_name="$2" app_user="$3"

    # These values are spliced into SQL as quoted identifiers. Restrict them
    # to PostgreSQL's safe identifier shape and reject roles that can never be
    # used as the dedicated, least-privilege Grafana reader.
    if ! [[ "$grafana_user" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        echo "Error: GRAFANA_DB_USERNAME must be a valid SQL identifier (got: $grafana_user)" >&2
        return 1
    fi
    if ! [[ "$db_name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        echo "Error: DB_NAME must be a valid SQL identifier (got: $db_name)" >&2
        return 1
    fi
    if [ "$grafana_user" = "$app_user" ] || [ "$grafana_user" = "postgres" ]; then
        echo "Error: GRAFANA_DB_USERNAME ('$grafana_user') must not match the application DB role ('$app_user') or the postgres superuser." >&2
        echo "       Pick a dedicated read-only role name (e.g. grafana_ro) and re-run." >&2
        return 1
    fi
    if [[ "$grafana_user" = pg_* ]]; then
        echo "Error: GRAFANA_DB_USERNAME must not use PostgreSQL's reserved pg_ role prefix (got: $grafana_user)" >&2
        return 1
    fi
}

validate_non_interactive_environment() {
    local key auth_secret encryption_key grafana_user db_name app_user invalid=0
    local -a alert_keys=(
        GRAFANA_ADMIN_PASSWORD
        GRAFANA_DB_USERNAME
        GRAFANA_DB_PASSWORD
        FLEET_ALERTS_WEBHOOK_TOKEN
        GRAFANA_SECRET_KEY
        FLEET_ALERTS_GRAFANA_TOKEN
    )

    for key in DB_USERNAME DB_PASSWORD AUTH_CLIENT_SECRET_KEY ENCRYPT_SERVICE_MASTER_KEY; do
        if ! env_has_nonempty_value "$key"; then
            echo "Error: missing or empty required key in $ENV_FILE: $key" >&2
            invalid=1
        fi
    done

    auth_secret=$(env_last_value AUTH_CLIENT_SECRET_KEY 2>/dev/null || true)
    if [ -n "$auth_secret" ] && [ "${#auth_secret}" -lt 32 ]; then
        echo "Error: AUTH_CLIENT_SECRET_KEY in $ENV_FILE must be at least 32 characters." >&2
        invalid=1
    fi

    encryption_key=$(env_last_value ENCRYPT_SERVICE_MASTER_KEY 2>/dev/null || true)
    if [ -n "$encryption_key" ] && ! validate_base64_key "$encryption_key"; then
        echo "Error: ENCRYPT_SERVICE_MASTER_KEY in $ENV_FILE must decode to exactly 32 bytes." >&2
        invalid=1
    fi

    if [ "$ENABLE_BETA_ALERTS" = "true" ]; then
        for key in "${alert_keys[@]}"; do
            if ! env_has_nonempty_value "$key"; then
                echo "Error: alerts are enabled but $key is missing or empty in $ENV_FILE." >&2
                invalid=1
            fi
        done
        grafana_user=$(env_last_value GRAFANA_DB_USERNAME 2>/dev/null || true)
        db_name=$(env_last_value DB_NAME 2>/dev/null || true)
        db_name="${db_name:-fleet}"
        app_user=$(env_last_value DB_USERNAME 2>/dev/null || true)
        if [ -n "$grafana_user" ] && [ -n "$app_user" ] && \
            ! validate_grafana_db_role_names "$grafana_user" "$db_name" "$app_user"; then
            invalid=1
        fi
    fi

    [ "$invalid" -eq 0 ]
}

# Get local network IP addresses (excludes loopback, includes IPv4 and global IPv6)
get_local_ips() {
    if [ "$(uname)" == "Darwin" ]; then
        # macOS - get IPv4 and global IPv6 from all active interfaces, exclude loopback
        ifconfig | grep "inet " | grep -v "127\." | awk '{print $2}' | tr '\n' ' '
        ifconfig | grep "inet6 " | grep -vE "fe[89ab][0-9a-f]:" | grep -v "::1" | awk '{print $2}' | tr '\n' ' '
    else
        # Linux - get IPv4 and global IPv6 from all active interfaces, exclude loopback
        hostname -I 2>/dev/null | tr ' ' '\n' | grep -v "^127\." | tr '\n' ' ' || \
        ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | grep -v "^127\." | tr '\n' ' '
        ip -6 addr show scope global 2>/dev/null | grep -oP '(?<=inet6\s)[0-9a-f:]+' | tr '\n' ' '
    fi
}

# Generate self-signed SSL certificate using OpenSSL
generate_self_signed_cert() {
    echo "Generating self-signed SSL certificate..."
    mkdir -p "$SSL_DIR"

    # Collect all addresses for the certificate
    local san_entries="DNS:localhost,IP:127.0.0.1,IP:::1"

    # Add local hostname
    local hostname=$(hostname)
    if [ -n "$hostname" ]; then
        san_entries="$san_entries,DNS:$hostname,DNS:${hostname}.local"
    fi

    # Add all local network IPs
    local local_ips=$(get_local_ips)
    for ip in $local_ips; do
        san_entries="$san_entries,IP:$ip"
    done

    echo "Certificate will be valid for: $san_entries"

    # Generate self-signed certificate valid for 365 days
    local openssl_output
    openssl_output=$(openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$SSL_KEY" \
        -out "$SSL_CERT" \
        -subj "/C=US/ST=Local/L=Local/O=ProtoFleet/CN=localhost" \
        -addext "subjectAltName=$san_entries" 2>&1)
    local openssl_status=$?

    if [ $openssl_status -ne 0 ]; then
        echo "Error: Failed to generate SSL certificate."
        echo "$openssl_output"
        return 1
    fi

    chmod 600 "$SSL_KEY"
    chmod 644 "$SSL_CERT"
    echo "Self-signed certificate generated successfully."
    echo ""
    echo "NOTE: Browsers will show a security warning for self-signed certificates."
    echo "      You can accept the warning to proceed, or import the certificate"
    echo "      into your browser/OS trust store."
}

cleanup_nginx_config_temp() {
    if [ -n "$NGINX_CONFIG_TEMP" ]; then
        if ! rm -f "$NGINX_CONFIG_TEMP"; then
            echo "Warning: could not remove temporary nginx config $NGINX_CONFIG_TEMP." >&2
            return 1
        fi
        NGINX_CONFIG_TEMP=""
    fi
    return 0
}

abort_nginx_config_write() {
    echo "Error: $1" >&2
    cleanup_nginx_config_temp || true
    return 1
}

# Copy appropriate nginx configuration based on protocol mode. Build the
# replacement beside its destination and rename it over the stable directory
# entry; this prevents a planted symlink or hard link from being truncated.
# Concurrent control of the parent directory is outside this shell-level
# defense and is excluded by the updater's trusted deployment-owner model.
copy_nginx_config() {
    local mode="$1"
    local src_conf="$NGINX_CONF_DIR/nginx.${mode}.conf"
    local dest_conf="$NGINX_CONF_DIR/nginx.conf"

    if [ ! -d "$NGINX_CONF_DIR" ] || [ -L "$NGINX_CONF_DIR" ]; then
        echo "Error: nginx config parent $NGINX_CONF_DIR must be a real directory, not a symlink." >&2
        return 1
    fi
    if [ ! -f "$src_conf" ] || [ -L "$src_conf" ]; then
        echo "Error: nginx config source $src_conf must be a regular, non-symlink file." >&2
        return 1
    fi
    if [ -L "$dest_conf" ] || { [ -e "$dest_conf" ] && [ ! -f "$dest_conf" ]; }; then
        echo "Error: generated nginx config $dest_conf must be a regular, non-symlink file when present." >&2
        return 1
    fi

    NGINX_CONFIG_TEMP=$(umask 077; mktemp "$NGINX_CONF_DIR/.nginx.conf.tmp.XXXXXX") || {
        NGINX_CONFIG_TEMP=""
        echo "Error: could not create a temporary nginx config beside $dest_conf." >&2
        return 1
    }
    if ! cp "$src_conf" "$NGINX_CONFIG_TEMP"; then
        abort_nginx_config_write "could not write the temporary nginx config"
        return 1
    fi
    if ! chmod 644 "$NGINX_CONFIG_TEMP"; then
        abort_nginx_config_write "could not set permissions on the temporary nginx config"
        return 1
    fi
    # Recheck the stable parent and destination immediately before replacement
    # so a planted directory, symlink, or FIFO fails clearly. This narrows the
    # check/write interval but is not a lock against a concurrent parent owner.
    if [ ! -d "$NGINX_CONF_DIR" ] || [ -L "$NGINX_CONF_DIR" ] ||
        [ -L "$dest_conf" ] || { [ -e "$dest_conf" ] && [ ! -f "$dest_conf" ]; }; then
        abort_nginx_config_write "$dest_conf changed to a non-regular path during configuration"
        return 1
    fi
    if ! mv -f "$NGINX_CONFIG_TEMP" "$dest_conf"; then
        abort_nginx_config_write "could not atomically replace $dest_conf"
        return 1
    fi
    NGINX_CONFIG_TEMP=""
}

# Detect if running inside WSL
is_wsl() {
    grep -qiE "(microsoft|wsl)" /proc/version 2>/dev/null
}

# Check and fix WSL networking issues (IPv6/DNS problems)
fix_wsl_networking() {
    echo "Detected WSL environment. Checking network connectivity..."

    # Test if we can reach Docker registry
    if ! curl -s --max-time 5 https://registry-1.docker.io/v2/ >/dev/null 2>&1; then
        echo "Network issue detected. Applying WSL networking fixes..."

        # Fix 1: Configure system to prefer IPv4 over IPv6
        echo "  - Configuring IPv4 preference..."
        if ! grep -qF "precedence ::ffff:0:0/96 100" /etc/gai.conf 2>/dev/null; then
            sudo bash -c 'echo "precedence ::ffff:0:0/96 100" >> /etc/gai.conf'
        fi
        # Fix 2: Disable IPv6 routing at kernel level (WSL-specific workaround for connectivity issues).
        # When IPv6 is disabled, the discovery pipeline gracefully falls back to IPv4-only operation.
        echo "  - Disabling IPv6 routing..."
        sudo sysctl -w net.ipv6.conf.all.disable_ipv6=1 >/dev/null 2>&1
        sudo sysctl -w net.ipv6.conf.default.disable_ipv6=1 >/dev/null 2>&1

        # Make IPv6 disabling persistent across reboots
        for setting in "net.ipv6.conf.all.disable_ipv6=1" "net.ipv6.conf.default.disable_ipv6=1"; do
            grep -q "^$setting" /etc/sysctl.conf 2>/dev/null || sudo bash -c "echo '$setting' >> /etc/sysctl.conf"
        done
        # Fix 3: Ensure Google's DNS server is available as a fallback
        echo "  - Configuring DNS..."
        if ! grep -q "nameserver 8.8.8.8" /etc/resolv.conf 2>/dev/null; then
            sudo cp /etc/resolv.conf "/etc/resolv.conf.backup.$(date +%s)" 2>/dev/null || true
            sudo bash -c 'echo "nameserver 8.8.8.8" >> /etc/resolv.conf'
        fi

        # Fix 4: Prevent WSL from overwriting resolv.conf on restart
        if grep -q "generateResolvConf *= *false" /etc/wsl.conf 2>/dev/null; then
            : # Already configured correctly
        elif grep -q "generateResolvConf" /etc/wsl.conf 2>/dev/null; then
            # Setting exists but is true - change to false
            sudo sed -i 's/generateResolvConf *= *true/generateResolvConf = false/' /etc/wsl.conf
        elif grep -q "^\[network\]" /etc/wsl.conf 2>/dev/null; then
            # [network] section exists - add setting to it
            sudo sed -i '/^\[network\]/a generateResolvConf = false' /etc/wsl.conf
        else
            # No [network] section - append new section
            sudo bash -c 'printf "\n[network]\ngenerateResolvConf = false\n" >> /etc/wsl.conf'
        fi

        echo "Fixes applied. Testing connectivity..."

        max_retries=5
        backoff_seconds=2
        attempt=1
        connectivity_restored=0

        while [ "$attempt" -le "$max_retries" ]; do
            echo "  - Connectivity test attempt $attempt of $max_retries..."
            if curl -s --max-time 10 https://registry-1.docker.io/v2/ >/dev/null 2>&1; then
                connectivity_restored=1
                break
            fi

            if [ "$attempt" -lt "$max_retries" ]; then
                echo "    Connection still failing. Waiting ${backoff_seconds}s before retry..."
                sleep "$backoff_seconds"
                backoff_seconds=$((backoff_seconds * 2))
            fi

            attempt=$((attempt + 1))
        done

        if [ "$connectivity_restored" -ne 1 ]; then
            echo ""
            echo "ERROR: Still cannot reach Docker registry."
            echo "Please try the following:"
            echo "  1. Open PowerShell as Administrator"
            echo "  2. Run: wsl --shutdown"
            echo "  3. Re-open WSL and run this script again"
            echo ""
            exit 1
        fi

        echo "Network connectivity restored!"

        # Clear any corrupted Docker build cache from previous failed attempts
        echo "Clearing Docker build cache from any previous failed attempts..."
        docker builder prune -af >/dev/null 2>&1 || true
    else
        echo "Network connectivity OK."
    fi
}

# Unattended upgrades never mutate the host: every interactive repair below
# (installs, service enablement, group changes) must fail through this guard
# instead of running.
refuse_non_interactive_host_repair() {
    if [ "$NON_INTERACTIVE" = "true" ]; then
        echo "Error: $1" >&2
        exit 1
    fi
}

# ----------------------------------------------------------------------------
# Docker Installation Check and Setup
# ----------------------------------------------------------------------------

if ! command -v docker &> /dev/null; then
    refuse_non_interactive_host_repair "Docker is not installed; non-interactive upgrade cannot install host prerequisites."
    echo "Docker is not installed. Attempting to install Docker..."

    if [ "$(uname)" == "Linux" ]; then
        curl -fsSL https://get.docker.com | sudo sh

        if ! command -v docker &> /dev/null; then
            echo "Error: Docker installation failed. Please install Docker manually:"
            echo "Visit https://docs.docker.com/engine/install/"
            exit 1
        fi

        echo "Docker installed successfully!"
    else
        echo "Please install Docker manually:"
        echo "Visit https://docs.docker.com/get-docker/"
        exit 1
    fi
fi

# Configure Docker for Linux systems
if [ "$(uname)" == "Linux" ]; then
    # The Windows installer supports non-systemd WSL distros by starting
    # Docker through `service` or its init script and maintaining a Windows
    # scheduled task. Do not reject that supported setup before the
    # authoritative `docker info` probe below.
    if ! is_wsl; then
        # Check if Docker is set to start on boot on native Linux hosts.
        if ! systemctl is-enabled docker &>/dev/null; then
            refuse_non_interactive_host_repair "Docker is not enabled at boot; fix the host before retrying the upgrade."
            echo "Configuring Docker to start on system boot..."
            sudo systemctl enable docker
        fi
    fi

    # Check if current user is in the docker group.
    # Skip when running as root: root accesses /var/run/docker.sock directly
    # via socket-file permissions and does not need docker-group membership.
    # Without this skip, `sudo bash install.sh ...` (the recommended remediation
    # for the sudo-mismatch detection in install.sh) would exit here telling
    # the user to log out and back in, leaving the upgrade half-applied.
    if [ "$(id -u)" -ne 0 ] && ! groups $USER | grep -q '\bdocker\b'; then
        refuse_non_interactive_host_repair "the upgrade user cannot access Docker."
        echo "Adding current user to the docker group for passwordless Docker usage..."
        sudo usermod -aG docker $USER
        echo "Please log out and log back in to apply group changes, then re-run this script."
        exit 0
    fi
fi

# ----------------------------------------------------------------------------
# Docker Daemon Check and Startup
# ----------------------------------------------------------------------------

if ! docker info > /dev/null 2>&1; then
    refuse_non_interactive_host_repair "Docker daemon is not available; non-interactive upgrade will not mutate host services."
    echo "Docker daemon is not running. Starting Docker..."

    # For macOS, attempt to start Docker Desktop
    if [ "$(uname)" == "Darwin" ]; then
        open -a Docker

        echo "Waiting for Docker to start..."
        for i in {1..30}; do
            if docker info > /dev/null 2>&1; then
                echo "Docker daemon is now running."
                break
            fi
            sleep 1
            if [ $i -eq 30 ]; then
                echo "Error: Docker failed to start within 30 seconds."
                exit 1
            fi
        done
    else
        # For Linux systems
        echo "Attempting to start Docker service..."
        sudo systemctl start docker

        for i in {1..10}; do
            if docker info > /dev/null 2>&1; then
                echo "Docker daemon is now running."
                break
            fi
            sleep 1
            if [ $i -eq 10 ]; then
                echo "Error: Docker failed to start."
                exit 1
            fi
        done
    fi
else
    echo "Docker daemon is already running."
fi

# ----------------------------------------------------------------------------
# WSL Networking Fix
# ----------------------------------------------------------------------------

# Registry diagnostics and host-network repair are needed only when image
# preparation may pull. A prepared activation verifies local image IDs and
# starts with --no-build/--pull never, so it must remain usable offline.
if [ "$SKIP_BUILD" != "true" ] && is_wsl; then
    if [ "$NON_INTERACTIVE" = "true" ]; then
        echo "Detected WSL environment. Checking Docker registry connectivity..."
        if ! curl -s --max-time 5 https://registry-1.docker.io/v2/ >/dev/null 2>&1; then
            echo "Error: Docker registry connectivity is unavailable in WSL; non-interactive upgrade will not modify host networking." >&2
            exit 1
        fi
    else
        fix_wsl_networking
    fi
fi

# ----------------------------------------------------------------------------
# Docker Compose Installation Check
# ----------------------------------------------------------------------------

if ! docker compose version &> /dev/null; then
    refuse_non_interactive_host_repair "docker compose is not installed; non-interactive upgrade cannot install host prerequisites."
    echo "docker compose is not installed. Attempting to install it..."

    if [ "$(uname)" == "Linux" ]; then
        # For Linux
        if command -v apt-get &> /dev/null; then
            sudo apt-get install -y docker-compose-plugin
        elif command -v yum &> /dev/null; then
            sudo yum install -y docker-compose-plugin
        else
            echo "Could not automatically install docker compose. Please install it manually. https://docs.docker.com/compose/install/linux/"
            exit 1
        fi
    else
        echo "Please install docker compose manually. https://docs.docker.com/compose/install/"
        exit 1
    fi
fi

# The post-start readiness check below uses both `--wait` and `--wait-timeout`
# (Compose v2.17.0+). Fail fast here, before `docker compose down` takes an
# existing stack offline.
compose_up_help=$(docker compose up --help 2>&1 || true)
for flag in --wait --wait-timeout --no-build --pull; do
    if ! grep -qE -- "(^|[[:space:]])${flag}([[:space:]]|$)" <<<"$compose_up_help"; then
        echo "Error: your docker compose does not support ${flag}."
        echo "run-fleet.sh requires Compose v2.17.0+. Upgrade: https://docs.docker.com/compose/install/"
        exit 1
    fi
done

# Replace exact keys without ever truncating the live environment file. The
# temporary file lives beside .env so the final rename is atomic, while the
# checked ownership handoff preserves access for deployments upgraded by a
# root-owned service on behalf of a non-root operator.
file_owner_group() {
    local owner_group
    owner_group=$(stat -c '%u:%g' "$1" 2>/dev/null) ||
        owner_group=$(stat -f '%u:%g' "$1" 2>/dev/null) || return 1
    [ -n "$owner_group" ] || return 1
    printf '%s' "$owner_group"
}

ENV_REWRITE_TEMP=""

cleanup_env_rewrite() {
    if [ -n "$ENV_REWRITE_TEMP" ]; then
        if ! rm -f "$ENV_REWRITE_TEMP"; then
            echo "Warning: could not remove temporary environment file $ENV_REWRITE_TEMP." >&2
            return 1
        fi
        ENV_REWRITE_TEMP=""
    fi
    return 0
}

abort_env_rewrite() {
    echo "Error: $1; the original was not changed." >&2
    cleanup_env_rewrite || true
    return 1
}

atomic_set_env_values() {
    local key value key_csv original_owner temp_owner last_byte assignment
    local -a removed_keys=()
    local -a assignments=()

    if [ "$#" -eq 0 ] || [ $(( $# % 2 )) -ne 0 ]; then
        echo "Error: environment updates require key/value pairs." >&2
        return 1
    fi
    while [ "$#" -gt 0 ]; do
        key="$1"
        value="$2"
        shift 2
        if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
            echo "Error: invalid environment key: $key" >&2
            return 1
        fi
        if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
            echo "Error: refusing to persist a multiline value for $key." >&2
            return 1
        fi
        removed_keys+=("$key")
        assignments+=("${key}=${value}")
    done

    if [ ! -f "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; then
        echo "Error: $ENV_FILE must be a regular, non-symlink file." >&2
        return 1
    fi

    key_csv=$(IFS=,; printf '%s' "${removed_keys[*]}")

    original_owner=$(file_owner_group "$ENV_FILE") || {
        abort_env_rewrite "could not read owner/group metadata from $ENV_FILE"
        return 1
    }
    ENV_REWRITE_TEMP=$(umask 077; mktemp "${ENV_FILE}.tmp.XXXXXX") || {
        ENV_REWRITE_TEMP=""
        abort_env_rewrite "could not create a temporary environment file next to $ENV_FILE"
        return 1
    }

    if ! awk -v keys="$key_csv" '
        BEGIN {
            count = split(keys, names, ",")
            for (i = 1; i <= count; i++) {
                removed[names[i]] = 1
            }
        }
        {
            assignment = $0
            sub(/^[[:space:]]+/, "", assignment)
            sub(/^export[[:space:]]+/, "", assignment)
            if (match(assignment, /^[A-Za-z_][A-Za-z0-9_]*[[:space:]]*[:=]/)) {
                name = substr(assignment, 1, RLENGTH)
                sub(/[[:space:]]*[:=]$/, "", name)
                if (name in removed) {
                    next
                }
            }
            print
        }
    ' "$ENV_FILE" > "$ENV_REWRITE_TEMP"; then
        abort_env_rewrite "could not build a replacement for $ENV_FILE"
        return 1
    fi

    if [ -s "$ENV_REWRITE_TEMP" ]; then
        last_byte=$(tail -c 1 "$ENV_REWRITE_TEMP") || {
            abort_env_rewrite "could not inspect the temporary environment file"
            return 1
        }
        if [ -n "$last_byte" ] && ! printf '\n' >> "$ENV_REWRITE_TEMP"; then
            abort_env_rewrite "could not complete the temporary environment file"
            return 1
        fi
    fi
    for assignment in "${assignments[@]}"; do
        if ! printf '%s\n' "$assignment" >> "$ENV_REWRITE_TEMP"; then
            abort_env_rewrite "could not write the temporary environment file"
            return 1
        fi
    done

    temp_owner=$(file_owner_group "$ENV_REWRITE_TEMP") || {
        abort_env_rewrite "could not read owner/group metadata from the temporary environment file"
        return 1
    }
    if [ "$temp_owner" != "$original_owner" ] && ! chown "$original_owner" "$ENV_REWRITE_TEMP"; then
        abort_env_rewrite "could not preserve owner/group for $ENV_FILE"
        return 1
    fi
    if ! chmod 600 "$ENV_REWRITE_TEMP"; then
        abort_env_rewrite "could not restrict permissions on the replacement for $ENV_FILE"
        return 1
    fi
    if ! mv -f "$ENV_REWRITE_TEMP" "$ENV_FILE"; then
        abort_env_rewrite "could not atomically replace $ENV_FILE"
        return 1
    fi

    ENV_REWRITE_TEMP=""
}

# ----------------------------------------------------------------------------
# Database Volume Management Function
# ----------------------------------------------------------------------------

# Prompt user to reinitialize TimescaleDB data volume if it exists
prompt_store_reinit() {
  local proj="$FLEET_COMPOSE_PROJECT_NAME"
  local vol
  vol=$(docker volume ls -q | grep -E "^${proj}[-_]timescaledb-data$")
  if [[ -n $vol ]]; then
    echo "⚠️  Detected existing TimescaleDB data volume: $vol"
    read -p "   Remove & reinitialize this volume now? ALL DATA WILL BE LOST (y/N): " answer
    if [[ $answer =~ ^[Yy]$ ]]; then
      echo "   Shutting down containers…"
      compose down --remove-orphans
      echo "   Removing volume $vol…"
      docker volume rm "$vol"
      echo "   Volume removed; new credentials will apply next startup."
    else
      return 1
    fi
  fi
  return 0
}

# ----------------------------------------------------------------------------
# Environment File Validation and Setup
# ----------------------------------------------------------------------------

prompt_fleet_profile() {
    local choice
    echo ""
    echo "Select a host profile (tunes the database and poller for this hardware):"
    echo "  1) standard - Raspberry Pi 5 class host, 16GB RAM with SSD; up to ~5000 miners (default)"
    echo "  2) mini     - low-power or SD-card host, <=4GB RAM; up to ~200 miners"
    echo "  3) max      - dedicated server, 32GB+ RAM, 8+ cores, NVMe; 5000+ miners"
    echo -n "Profile [1]: "
    read -r profile_choice
    profile_choice=$(printf '%s' "$profile_choice" | tr '[:upper:]' '[:lower:]')
    case "$profile_choice" in
        2|mini) choice="mini" ;;
        3|max) choice="max" ;;
        *) choice="standard" ;;
    esac
    append_env_line "FLEET_PROFILE=$choice"
    echo "Host profile '$choice' saved to $ENV_FILE (edit FLEET_PROFILE there to change it)."
}

# curl | bash installs reach prompts with stdin at EOF; never let an
# unanswered prompt persist a profile
maybe_prompt_fleet_profile() {
    if [ "$NON_INTERACTIVE" = "true" ]; then
        echo "No FLEET_PROFILE is persisted; keeping the existing conservative defaults."
    elif [ -t 0 ]; then
        prompt_fleet_profile
    else
        echo "Hint: host profiles are available; set FLEET_PROFILE=standard|mini|max in $ENV_FILE and re-run to tune for this hardware."
    fi
}

use_existing="no"

# Check if environment file exists and validate its contents
if [ -f "$ENV_FILE" ]; then
    required_keys=(
        "DB_USERNAME"
        "DB_PASSWORD"
        "AUTH_CLIENT_SECRET_KEY"
        "ENCRYPT_SERVICE_MASTER_KEY"
    )

    # Check for missing required keys
    missing_keys=0
    for key in "${required_keys[@]}"; do
        if ! env_has_nonempty_value "$key"; then
            missing_keys=1
            echo "Missing or empty required key in environment file: $key"
        fi
    done

    if [ $missing_keys -eq 0 ]; then
        if [ "$NON_INTERACTIVE" = "true" ]; then
            use_existing_creds="y"
        else
            echo -n "Existing environment file found with all required keys. Use it? (Y/n): "
            read use_existing_creds
        fi
        if [[ -z "$use_existing_creds" || $use_existing_creds =~ ^[Yy]$ ]]; then
            use_existing="yes"
            echo "Using existing environment file."
            # Pre-profile installs upgrading: ask once
            if ! env_last_value FLEET_PROFILE >/dev/null 2>&1; then
                maybe_prompt_fleet_profile
            fi
        else
            prompt_store_reinit || { echo "Aborting due to existing data volume."; exit 1; }
        fi
    else
        if [ "$NON_INTERACTIVE" = "true" ]; then
            echo "Error: existing environment file is incomplete; refusing to regenerate secrets during an upgrade." >&2
            exit 1
        fi
        echo "Existing environment file is incomplete. Regenerating…"
        prompt_store_reinit || { echo "Cannot proceed with incomplete env + existing data."; exit 1; }
    fi
fi

if [ "$NON_INTERACTIVE" = "true" ] && [ "$use_existing" = "no" ]; then
    echo "Error: non-interactive mode requires an existing complete $ENV_FILE." >&2
    exit 1
fi

if [ "$NON_INTERACTIVE" = "true" ] && ! validate_non_interactive_environment; then
    echo "Error: non-interactive mode requires complete, valid persisted configuration." >&2
    exit 1
fi

# ----------------------------------------------------------------------------
# Generate New Environment Configuration
# ----------------------------------------------------------------------------

if [ "$use_existing" == "no" ]; then
    # Create with 0600 from birth; secrets land in this file before the
    # final chmod, and umask perms would expose them in the interim
    rm -f "$ENV_FILE"
    (umask 077; : > "$ENV_FILE")

    # Database user configuration
    echo -n "Enter username for the Database user [fleet]: "
    read DB_USERNAME
    DB_USERNAME=${DB_USERNAME:-fleet}
    echo "DB_USERNAME=$DB_USERNAME" >> "$ENV_FILE"

    echo -n "Generate a random password for the Database user? (Y/n): "
    read gen_db_pass
    if [[ -z "$gen_db_pass" || $gen_db_pass =~ ^[Yy]$ ]]; then
        DB_PASSWORD=$(openssl rand -base64 16)
        echo "Generated secure password for the Database user."
    else
        echo -n "Enter password for the Database user: "
        read -s DB_PASSWORD
        echo
    fi
    echo "DB_PASSWORD=$DB_PASSWORD" >> "$ENV_FILE"

    # Auth client secret key configuration
    echo -n "Generate a random Auth client secret key? (Y/n): "
    read gen_auth_key
    if [[ -z "$gen_auth_key" || $gen_auth_key =~ ^[Yy]$ ]]; then
        AUTH_CLIENT_SECRET_KEY=$(openssl rand -base64 32)
        echo "Generated secure Auth client secret key."
    else
        while true; do
            echo -n "Enter Auth client secret key (minimum 32 characters for security): "
            read -s AUTH_CLIENT_SECRET_KEY
            echo

            byte_length=${#AUTH_CLIENT_SECRET_KEY}
            if [ "$byte_length" -lt 32 ]; then
                echo "Error: Secret key must be at least 32 characters long."
                echo "Current length: $byte_length characters"
            else
                echo "Auth client secret key accepted."
                break
            fi
        done
    fi
    echo "AUTH_CLIENT_SECRET_KEY=$AUTH_CLIENT_SECRET_KEY" >> "$ENV_FILE"

    # Encryption service master key configuration
    echo -n "Generate a random encryption service master key? (Y/n): "
    read gen_key
    if [[ -z "$gen_key" || $gen_key =~ ^[Yy]$ ]]; then
        ENCRYPT_SERVICE_MASTER_KEY=$(openssl rand -base64 32)
        echo "Generated encryption service master key."
    else
        while true; do
            echo -n "Enter Encryption service master key: "
            read -s ENCRYPT_SERVICE_MASTER_KEY
            echo
            if ! validate_base64_key "$ENCRYPT_SERVICE_MASTER_KEY"; then
                echo "Error: The provided key is not valid Base64 or doesn't decode to 32 bytes."
            else
                echo "Encryption service master key accepted."
                break
            fi
        done
    fi
    echo "ENCRYPT_SERVICE_MASTER_KEY=$ENCRYPT_SERVICE_MASTER_KEY" >> "$ENV_FILE"

    maybe_prompt_fleet_profile

    # Secure the env file
    chmod 600 "$ENV_FILE"
    echo "Environment variables saved to $ENV_FILE"
fi

# Persist every deployment overlay as explicit state. Historically, flags were
# process-only, so the next upgrade could silently disable alerts, monitoring,
# or tracing. Replace the three values as one transaction so a failure cannot
# leave partially updated state. Last value wins, matching Compose's .env
# behavior.
if ! atomic_set_env_values \
    ENABLE_BETA_ALERTS "$ENABLE_BETA_ALERTS" \
    ENABLE_SYSTEM_MONITORING "$ENABLE_SYSTEM_MONITORING" \
    ENABLE_TRACING "$ENABLE_TRACING" \
    ENABLE_ONE_CLICK_UPDATES "$ENABLE_ONE_CLICK_UPDATES"; then
    echo "Error: could not persist deployment overlay settings; aborting before Compose validation or service changes." >&2
    exit 1
fi
if [ "$ONE_CLICK_UPDATES_WAS_CONFIGURED" != "$ENABLE_ONE_CLICK_UPDATES" ]; then
    DIRECT_UPDATER_ENV_ROLLBACK_PENDING=true
fi

# ----------------------------------------------------------------------------
# Docker Compose File Validation
# ----------------------------------------------------------------------------

if [ ! -f "$COMPOSE_FILE" ]; then
    echo "Error: Docker Compose file not found at $COMPOSE_FILE"
    exit 1
fi

if [ "$ENABLE_BETA_ALERTS" = "true" ]; then
    if [ ! -f "$COMPOSE_ALERTS_FILE" ]; then
        echo "Error: --enable-beta-alerts was passed but $COMPOSE_ALERTS_FILE is missing."
        exit 1
    fi

    # The Grafana sidecar runs the alerting engine + UI; give it a
    # rotated admin password the first time we boot the stack so the
    # default "admin / admin" never ships.
    if ! env_has_nonempty_value GRAFANA_ADMIN_PASSWORD; then
        GRAFANA_ADMIN_PASSWORD=$(openssl rand -base64 24)
        echo "GRAFANA_ADMIN_PASSWORD=$GRAFANA_ADMIN_PASSWORD" >> "$ENV_FILE"
        echo "Generated Grafana admin password (stored in $ENV_FILE)."
    fi

    if ! env_has_nonempty_value GRAFANA_DB_USERNAME; then
        if ! atomic_set_env_values GRAFANA_DB_USERNAME grafana_ro; then
            echo "Error: could not persist the Grafana database username." >&2
            exit 1
        fi
    fi
    if ! env_has_nonempty_value GRAFANA_DB_PASSWORD; then
        GRAFANA_DB_PASSWORD=$(openssl rand -base64 24)
        if ! atomic_set_env_values GRAFANA_DB_PASSWORD "$GRAFANA_DB_PASSWORD"; then
            echo "Error: could not persist the generated Grafana database password." >&2
            exit 1
        fi
        echo "Generated Grafana DB password (stored in $ENV_FILE)."
    fi

    # Shared secret the alertmanager webhook receiver requires on every
    # delivery. Mounted on the same listener as the public Connect-RPC
    # services, so without this token an unauthenticated caller on the
    # api-proxy network could forge system alert activity rows.
    if ! env_has_nonempty_value FLEET_ALERTS_WEBHOOK_TOKEN; then
        FLEET_ALERTS_WEBHOOK_TOKEN=$(openssl rand -base64 32)
        echo "FLEET_ALERTS_WEBHOOK_TOKEN=$FLEET_ALERTS_WEBHOOK_TOKEN" >> "$ENV_FILE"
        echo "Generated alertmanager webhook token (stored in $ENV_FILE)."
    fi

    # Grafana's secret_key encrypts secure settings persisted to the
    # grafana-data volume (datasource credentials, encrypted secrets).
    if ! env_has_nonempty_value GRAFANA_SECRET_KEY; then
        GRAFANA_SECRET_KEY=$(openssl rand -base64 32)
        echo "GRAFANA_SECRET_KEY=$GRAFANA_SECRET_KEY" >> "$ENV_FILE"
        echo "Generated Grafana secret key (stored in $ENV_FILE)."
    fi

    # Re-tighten in case the env file was carried over from an older
    # deployment with permissive (e.g. 0644) permissions.
    chmod 600 "$ENV_FILE"

    echo "Alerts stack: enabled (Grafana sidecar over TimescaleDB)"
else
    echo "Alerts stack: disabled (pass --enable-beta-alerts to turn on the beta alerts sidecars)"
fi

if [ "$ENABLE_SYSTEM_MONITORING" = "true" ]; then
    if [ ! -f "$COMPOSE_SYSTEM_MONITORING_FILE" ]; then
        echo "Error: --enable-system-monitoring was passed but $COMPOSE_SYSTEM_MONITORING_FILE is missing."
        exit 1
    fi
    echo "System monitoring: enabled (host CPU/RAM/disk alerts + slow-query dashboard)"
else
    echo "System monitoring: disabled (pass --enable-system-monitoring alongside --enable-beta-alerts to turn it on)"
fi

if [ "$ENABLE_TRACING" = "true" ]; then
    # Re-check after env setup: regenerating .env above drops keys the pre-layering check accepted.
    if ! env_has_nonempty_value DD_API_KEY; then
        echo "Error: $ENV_FILE was regenerated without DD_API_KEY; add it and re-run with --enable-tracing." >&2
        exit 1
    fi
    echo "Tracing: enabled (fleet-api request spans forwarded to Datadog APM)"
    # Re-run in case .env was regenerated above, then persist so later manual compose commands satisfy ${DD_HOSTNAME:?} too.
    ensure_dd_hostname
    if [ "$DD_HOSTNAME_DEFAULTED" = "true" ] && ! dotenv_has_nonempty_value DD_HOSTNAME; then
        append_env_line "DD_HOSTNAME=$DD_HOSTNAME"
        echo "         trace host.name defaults to '$DD_HOSTNAME' (saved to $ENV_FILE); change it there if the Datadog Agent reports a different hostname"
    fi
else
    echo "Tracing: disabled (pass --enable-tracing with DD_API_KEY in .env to forward request spans to Datadog)"
fi

# ----------------------------------------------------------------------------
# SSL/TLS Configuration
# ----------------------------------------------------------------------------

echo ""
echo "============================================================================"
echo "SSL/TLS Configuration"
echo "============================================================================"

# Non-interactive upgrades preserve the persisted transport. Certificate files
# can be stale after an operator switches back to HTTP, so their mere presence
# must not silently re-enable HTTPS.
if [ "$NON_INTERACTIVE" = "true" ]; then
    if persisted_cookie_mode=$(env_last_value SESSION_COOKIE_SECURE); then
        persisted_cookie_mode=$(printf '%s' "$persisted_cookie_mode" | tr '[:upper:]' '[:lower:]')
        case "$persisted_cookie_mode" in
            true)
                if [ ! -f "$SSL_CERT" ] || [ ! -f "$SSL_KEY" ]; then
                    echo "Error: HTTPS is persisted but its certificate/key are missing; refusing to switch protocol during upgrade." >&2
                    exit 1
                fi
                echo "Preserving HTTPS mode from $ENV_FILE."
                PROTOCOL_MODE="https"
                ;;
            false)
                echo "Preserving HTTP mode from $ENV_FILE."
                PROTOCOL_MODE="http"
                ;;
            *)
                echo "Error: SESSION_COOKIE_SECURE must be true or false in $ENV_FILE." >&2
                exit 1
                ;;
        esac
    elif [ -f "$SSL_CERT" ] && [ -f "$SSL_KEY" ]; then
        # Releases before transport mode was persisted inferred HTTPS from a
        # complete certificate pair. Preserve that secure legacy behavior and
        # write the explicit setting below for future unattended upgrades.
        echo "Preserving legacy HTTPS mode from the existing certificate pair."
        PROTOCOL_MODE="https"
    elif [ -f "$SSL_CERT" ] || [ -f "$SSL_KEY" ]; then
        echo "Error: SESSION_COOKIE_SECURE is missing and the legacy certificate pair is incomplete; refusing to switch protocol during upgrade." >&2
        exit 1
    else
        echo "No persisted transport or certificate pair found; preserving legacy HTTP mode."
        PROTOCOL_MODE="http"
    fi
else
    # Interactive setup treats a complete certificate pair as an explicit
    # request for HTTPS, preserving the existing installer behavior.
    if [ -f "$SSL_CERT" ] && [ -f "$SSL_KEY" ]; then
        echo "Found existing SSL certificates in $SSL_DIR"
        echo "  Certificate: $SSL_CERT"
        echo "  Private Key: $SSL_KEY"
        PROTOCOL_MODE="https"
    else
        echo ""
        echo "No SSL certificates found in $SSL_DIR"
        echo ""
        echo "Options:"
        echo "  1) HTTP only (no encryption) - simplest for isolated LANs"
        echo "  2) HTTPS with self-signed certificate - browsers will show warnings"
        echo "  3) HTTPS with your own certificates - place cert.pem and key.pem in $SSL_DIR"
        echo ""
        echo -n "Select option [1]: "
        read ssl_choice
        ssl_choice=${ssl_choice:-1}

        case "$ssl_choice" in
            2)
                if generate_self_signed_cert; then
                    PROTOCOL_MODE="https"
                else
                    echo "Falling back to HTTP mode."
                    PROTOCOL_MODE="http"
                fi
                ;;
            3)
                echo ""
                echo "Please place your SSL certificates in $SSL_DIR:"
                echo "  - $SSL_CERT (certificate)"
                echo "  - $SSL_KEY (private key)"
                echo ""
                echo "Then re-run this script."
                exit 0
                ;;
            *)
                echo "Using HTTP mode (no encryption)."
                PROTOCOL_MODE="http"
                ;;
        esac
    fi
fi

echo ""
echo "Protocol mode: $PROTOCOL_MODE"

# Ensure SSL directory exists (required for docker-compose volume mount)
mkdir -p "$SSL_DIR"

# Write appropriate nginx configuration
if ! copy_nginx_config "$PROTOCOL_MODE"; then
    echo "Error: Failed to set up nginx configuration. Exiting."
    exit 1
fi

# Update environment file with cookie security setting.
if [ "$PROTOCOL_MODE" == "https" ]; then
    cookie_secure=true
else
    cookie_secure=false
fi
if ! atomic_set_env_values SESSION_COOKIE_SECURE "$cookie_secure"; then
    echo "Error: could not persist the session cookie security setting; aborting before Compose validation or service changes." >&2
    exit 1
fi

# Pick up FLEET_PROFILE written during env setup
refresh_compose_env_args

echo "Validating Docker Compose configuration..."
if ! compose config --quiet; then
    echo "Error: Docker Compose configuration is invalid; services were not stopped." >&2
    exit 1
fi
if ! verify_release_image_references; then
    echo "Error: Docker Compose image references do not match this release; services were not stopped." >&2
    exit 1
fi

if [ "$SKIP_BUILD" = "true" ]; then
    if ! verify_preflight_marker; then
        echo "Error: --skip-build can only activate the unchanged release prepared by preflight." >&2
        exit 1
    fi
elif [ "$PREFLIGHT_ONLY" = "true" ]; then
    if ! verify_release_manifest; then
        echo "Error: immutable release manifest validation failed before preflight." >&2
        exit 1
    fi
    PREFLIGHT_FINGERPRINT=$(preflight_fingerprint) || {
        echo "Error: could not fingerprint the release before preflight." >&2
        exit 1
    }
fi

# ----------------------------------------------------------------------------
# Docker Image Preparation
# ----------------------------------------------------------------------------

if [ "$SKIP_BUILD" != "true" ]; then
    echo "Pulling latest Docker images..."
    # Compose v2.17+ reports pull_policy: never services as skipped without
    # requiring their image in the local cache. Keep external pulls first so
    # a registry failure cannot retag the bundled TimescaleDB image before the
    # preflight aborts; the verified archive is loaded immediately afterward.
    if ! compose pull --ignore-buildable; then
        echo "Error: Failed to pull required Docker images."
        exit 1
    fi

    # Load pre-built TimescaleDB image if available (built in CI for the target architecture)
    if [ -f "$TSDB_IMAGE" ]; then
        if ! validate_timescaledb_archive_tags; then
            exit 1
        fi
        echo "Loading pre-built TimescaleDB image..."
        if gunzip -c "$TSDB_IMAGE" | docker load; then
            echo "TimescaleDB image loaded successfully."
        else
            echo "Error: Failed to load TimescaleDB image from $TSDB_IMAGE"
            exit 1
        fi
        for image in "$TIMESCALEDB_IMAGE" "$TIMESCALEDB_HA_IMAGE"; do
            if ! docker image inspect "$image" >/dev/null 2>&1; then
                echo "Error: $TSDB_IMAGE loaded without required image $image." >&2
                exit 1
            fi
        done
    else
        if [ "$PREFLIGHT_ONLY" = "true" ]; then
            echo "Error: upgrade preflight requires the release's pre-built TimescaleDB image at $TSDB_IMAGE." >&2
            exit 1
        fi
        if ! docker image inspect "$TIMESCALEDB_IMAGE" >/dev/null 2>&1; then
            echo "Error: Pre-built TimescaleDB image not found at $TSDB_IMAGE and $TIMESCALEDB_IMAGE is not loaded." >&2
            exit 1
        fi
        echo "Pre-built TimescaleDB archive not found; reusing loaded image $TIMESCALEDB_IMAGE."
    fi

    # Build Docker images (fleet-api and fleet-client only; TimescaleDB uses pre-built image)
    compose build --no-cache || { echo "Error: Build failed. Exiting."; exit 1; }
else
    echo "Skipping image preparation; this release already passed updater preflight."
fi

if [ "$PREFLIGHT_ONLY" = "true" ]; then
    if ! record_preflight_marker "$PREFLIGHT_FINGERPRINT"; then
        echo "Error: could not record successful preflight at $PREFLIGHT_MARKER." >&2
        exit 1
    fi
    echo "Upgrade preflight completed successfully; the running stack was not stopped."
    exit 0
fi

# ----------------------------------------------------------------------------
# Service Management
# ----------------------------------------------------------------------------

echo "Stopping any running services..."
capture_previous_release_image_tags
DEPLOYMENT_MUTATION_STARTED=true
if ! compose down --remove-orphans; then
    echo "Error: could not stop the existing services cleanly." >&2
    exit 1
fi

echo "Starting services..."
# --wait blocks until every service is running (or healthy, when a healthcheck is defined).
# Without it, `up -d` can exit 0 while containers stay in Created (e.g. port conflicts under
# host networking), producing a false "Proto Fleet is now running!" banner.
if ! compose up --remove-orphans -d --wait --wait-timeout 300 --no-build --pull never; then
    echo "Error: services failed to reach running state."
    echo "Check logs with: docker compose --project-name $FLEET_COMPOSE_PROJECT_NAME ${COMPOSE_ENV_ARGS[*]} ${COMPOSE_FILES[*]} logs"
    exit 1
fi

wait_for_http_ready() {
    local label="$1" url="$2" attempt
    shift 2

    for attempt in $(seq 1 "$FLEET_API_READY_ATTEMPTS"); do
        if curl "$@" -o /dev/null --max-time 2 "$url"; then
            return 0
        fi
        if [ "$attempt" -eq "$FLEET_API_READY_ATTEMPTS" ]; then
            echo "Error: $label did not become ready within $((FLEET_API_READY_ATTEMPTS * 2))s." >&2
            return 1
        fi
        sleep 2
    done
}

wait_for_fleet_api_ready() {
    echo "Waiting for fleet-api readiness after migrations…"
    # The deployment Compose model fixes fleet-api to 0.0.0.0:4000 in its
    # extended service definition. Do not let an unrelated dotenv value
    # redirect this probe to nginx or another process.
    wait_for_http_ready fleet-api "http://127.0.0.1:4000/health/ready" -fsS
}

wait_for_fleet_client_ready() {
    local -a curl_flags=(-fsS)
    if [ "$PROTOCOL_MODE" = "https" ]; then
        # The packaged certificate can be self-signed; this is a loopback
        # liveness probe, not a remote identity check.
        curl_flags=(-fkSs)
    fi

    echo "Waiting for fleet-client readiness…"
    wait_for_http_ready fleet-client "${PROTOCOL_MODE}://127.0.0.1/" "${curl_flags[@]}"
}

if ! wait_for_fleet_api_ready; then
    echo "Error: refusing to consume the preflight proof or remove previous release images." >&2
    exit 1
fi

# ----------------------------------------------------------------------------
# Database Post-Start Tuning
# ----------------------------------------------------------------------------

# Needs a running, migrated database: pg_stat_statements for query
# diagnostics, and staggered Timescale policy job starts so compression and
# rollup refreshes don't all wake at once on small hosts. Idempotent;
# re-applied on every run so jobs added by new migrations get staggered on
# the next upgrade.
apply_database_tuning() {
    compose exec -T timescaledb \
        bash -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
DO $$
DECLARE
    r RECORD;
    n integer := 0;
BEGIN
    FOR r IN SELECT job_id FROM timescaledb_information.jobs
             WHERE proc_name IN ('policy_compression',
                                 'policy_refresh_continuous_aggregate',
                                 'policy_retention')
             ORDER BY job_id
    LOOP
        -- initial_start (not next_start) re-phases fixed-schedule jobs so
        -- the stagger survives future scheduled runs
        PERFORM public.alter_job(r.job_id, initial_start => now() + (n * interval '45 seconds'));
        n := n + 1;
    END LOOP;
END $$;
SQL
}

if ! apply_database_tuning; then
    echo "Warning: database tuning step failed; the stack is running, but pg_stat_statements and policy staggering are not applied. Re-run this script to retry." >&2
fi

# ----------------------------------------------------------------------------
# Grafana Read-Only DB Role Provisioning
# ----------------------------------------------------------------------------

# Create or rotate the dedicated `grafana_ro` DB role Grafana uses to
# query notification_metric_sample. We do this here (not in a SQL
# migration) so the password never has to be committed to source and
# can be rotated just by editing $ENV_FILE and re-running this script.
provision_grafana_db_role() {
    local grafana_user grafana_pass db_name app_user pw_escaped stats_grant stats_smoke

    grafana_user=$(env_last_value GRAFANA_DB_USERNAME 2>/dev/null || true)
    grafana_pass=$(env_last_value GRAFANA_DB_PASSWORD 2>/dev/null || true)
    db_name=$(env_last_value DB_NAME 2>/dev/null || true)
    db_name="${db_name:-fleet}"
    app_user=$(env_last_value DB_USERNAME 2>/dev/null || true)
    app_user="${app_user:-fleet}"

    if [ -z "$grafana_user" ] || [ -z "$grafana_pass" ]; then
        echo "Error: GRAFANA_DB_USERNAME / GRAFANA_DB_PASSWORD are missing or empty in $ENV_FILE." >&2
        echo "       Delete those entries from $ENV_FILE and re-run this script to regenerate them." >&2
        return 1
    fi

    if ! validate_grafana_db_role_names "$grafana_user" "$db_name" "$app_user"; then
        return 1
    fi

    # SQL-escape single quotes in the password so the inlined literal
    # parses regardless of what openssl rand produced.
    pw_escaped="${grafana_pass//\'/\'\'}"

    # fleet_slow_statements() is SECURITY DEFINER (migration 000115), so the
    # Grafana role reads this database's normalized statement stats without
    # pg_read_all_stats (which would also expose cluster-wide query text).
    # The reuse path's REVOKE-ALL-ON-ALL-FUNCTIONS wipes the grant each run;
    # it is re-granted here only while the feature is on. The smoke count()
    # executes the function, so it doubles as end-to-end preload verification.
    stats_grant=""
    stats_smoke=""
    if [ "$ENABLE_SYSTEM_MONITORING" = "true" ]; then
        stats_grant="GRANT EXECUTE ON FUNCTION fleet_slow_statements() TO \"${grafana_user}\";"
        stats_smoke="SELECT count(*) FROM fleet_slow_statements();"
    fi

    # `up --wait` only confirms containers are running, not that
    # fleet-api has finished its migration pass. Poll for every object
    # the Grafana alert rules read — the raw hypertable, the
    # fleet_telemetry_poll_heartbeat continuous aggregate, and the
    # fleet_pollable_device_presence / fleet_active_organization views
    # the protofleet-ingest-stalled and proto-fleet-system rules query.
    echo "Waiting for notification_metric_sample, fleet_telemetry_poll_heartbeat, fleet_pollable_device_presence and fleet_active_organization to be available…"
    if ! wait_for_psql_true "SELECT to_regclass('public.notification_metric_sample') IS NOT NULL AND to_regclass('public.fleet_telemetry_poll_heartbeat') IS NOT NULL AND to_regclass('public.fleet_pollable_device_presence') IS NOT NULL AND to_regclass('public.fleet_active_organization') IS NOT NULL"; then
        echo "Warning: notification_metric_sample / fleet_telemetry_poll_heartbeat / fleet_pollable_device_presence / fleet_active_organization did not appear; Grafana role not provisioned (datasource will fail until fleet-api migrations finish)." >&2
        return 1
    fi

    echo "Provisioning Grafana read-only DB role (${grafana_user})…"
    compose exec -T timescaledb \
        bash -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<SQL
-- The DO block below inlines the role password; keep this session out of
-- the slow-statement log (pg_stat_statements.track_utility=off does not
-- cover duration logging)
SET log_min_duration_statement = -1;
-- Create or rotate the Grafana DB role.
DO \$do\$
DECLARE
    target_role         text := '${grafana_user}';
    target_pass         text := '${pw_escaped}';
    target_db           text := '${db_name}';
    marker_comment      text := 'managed by proto-fleet run-fleet.sh (Grafana read-only role)';
    target_oid          oid;
    is_super            boolean;
    is_createdb         boolean;
    is_createrole       boolean;
    is_replication      boolean;
    is_bypassrls        boolean;
    existing_comment    text;
    member_count        integer;
    has_members_count   integer;
    owned_objects_count integer;
BEGIN
    SELECT oid, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls
      INTO target_oid, is_super, is_createdb, is_createrole, is_replication, is_bypassrls
      FROM pg_roles
     WHERE rolname = target_role;

    IF NOT FOUND THEN
        -- New role: create locked down so it has no path to escalate
        EXECUTE format(
            'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT',
            target_role, target_pass);
        EXECUTE format('COMMENT ON ROLE %I IS %L', target_role, marker_comment);
    ELSE
        -- Existing role: only repurpose when our own marker is present.
        existing_comment := shobj_description(target_oid, 'pg_authid');
        IF existing_comment IS DISTINCT FROM marker_comment THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: role exists but was not provisioned by this script (no managed-by marker on pg_authid). Pick a different GRAFANA_DB_USERNAME or drop the existing role first.',
                target_role;
        END IF;

        IF is_super OR is_createdb OR is_createrole OR is_replication OR is_bypassrls THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: existing role has elevated attributes (super/createdb/createrole/replication/bypassrls). Pick a different GRAFANA_DB_USERNAME or drop the existing role first.',
                target_role;
        END IF;

        SELECT count(*) INTO member_count
          FROM pg_auth_members
         WHERE member = target_oid;
        IF member_count > 0 THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: existing role is a member of other roles, which could grant inherited privileges. Pick a different GRAFANA_DB_USERNAME or drop the existing role first.',
                target_role;
        END IF;

        SELECT count(*) INTO has_members_count
          FROM pg_auth_members
         WHERE roleid = target_oid;
        IF has_members_count > 0 THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: other roles/users have been granted membership in this role and would inherit Grafana''s read-only access. Investigate and drop the role before re-running.',
                target_role;
        END IF;

        SELECT count(*) INTO owned_objects_count
          FROM pg_class
         WHERE relowner = target_oid;
        IF owned_objects_count > 0 THEN
            RAISE EXCEPTION
                'Refusing to reuse role % for Grafana: role owns % database objects, which suggests it is in use for something other than Grafana. Investigate and drop the role before re-running.',
                target_role, owned_objects_count;
        END IF;

        -- Wipe any direct grants accumulated outside of this script.
        EXECUTE format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON SCHEMA public FROM %I', target_role);
        EXECUTE format('REVOKE ALL PRIVILEGES ON DATABASE %I FROM %I', target_db, target_role);

        -- Known-safe: rotate the password.
        EXECUTE format(
            'ALTER ROLE %I WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT',
            target_role, target_pass);
        EXECUTE format('COMMENT ON ROLE %I IS %L', target_role, marker_comment);
    END IF;
END
\$do\$;

GRANT CONNECT ON DATABASE "${db_name}" TO "${grafana_user}";
GRANT USAGE ON SCHEMA public TO "${grafana_user}";
GRANT SELECT ON notification_metric_sample TO "${grafana_user}";
GRANT SELECT ON fleet_telemetry_poll_heartbeat TO "${grafana_user}";
-- Owner-privilege view: grafana_ro reads the boolean without grants on device/device_pairing.
GRANT SELECT ON fleet_pollable_device_presence TO "${grafana_user}";
-- Owner-privilege view: grafana_ro reads live org ids without grants on organization (miner_auth_private_key).
GRANT SELECT ON fleet_active_organization TO "${grafana_user}";
${stats_grant}

-- smoke check
SET ROLE "${grafana_user}";
SELECT 1 FROM notification_metric_sample LIMIT 0;
SELECT 1 FROM fleet_telemetry_poll_heartbeat LIMIT 0;
SELECT 1 FROM fleet_pollable_device_presence LIMIT 0;
SELECT 1 FROM fleet_active_organization LIMIT 0;
${stats_smoke}
RESET ROLE;
SQL
}

provision_grafana_service_account_token() {
    local admin_pass grafana_url sa_name token_name attempt sa_id token create_body

    if env_has_nonempty_value FLEET_ALERTS_GRAFANA_TOKEN; then
        echo "Grafana service-account token already present in $ENV_FILE; leaving it as-is."
        return 0
    fi

    admin_pass=$(env_last_value GRAFANA_ADMIN_PASSWORD 2>/dev/null || true)
    if [ -z "$admin_pass" ]; then
        echo "Error: GRAFANA_ADMIN_PASSWORD missing/empty in $ENV_FILE; cannot mint a Grafana token." >&2
        return 1
    fi

    grafana_url="http://127.0.0.1:3030"
    sa_name="fleet-api"
    token_name="fleet-api-$(date +%Y%m%d%H%M%S)"

    for attempt in $(seq 1 30); do
        if curl -fsS --max-time 5 -u "admin:${admin_pass}" "${grafana_url}/api/user" >/dev/null 2>&1; then
            break
        fi
        if [ "$attempt" -eq 30 ]; then
            echo "Error: Grafana API at ${grafana_url} not reachable with admin credentials; token not provisioned." >&2
            return 1
        fi
        sleep 2
    done

    create_body=$(curl -fsS --max-time 10 -u "admin:${admin_pass}" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"${sa_name}\",\"role\":\"Editor\",\"isDisabled\":false}" \
        "${grafana_url}/api/serviceaccounts" 2>/dev/null || true)
    sa_id=$(printf '%s' "$create_body" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)

    if [ -z "$sa_id" ]; then
        create_body=$(curl -fsS --max-time 10 -u "admin:${admin_pass}" \
            "${grafana_url}/api/serviceaccounts/search?query=${sa_name}" 2>/dev/null || true)
        sa_id=$(printf '%s' "$create_body" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
    fi

    if [ -z "$sa_id" ]; then
        echo "Error: could not create or locate the Grafana '${sa_name}' service account." >&2
        return 1
    fi

    token=$(curl -fsS --max-time 10 -u "admin:${admin_pass}" \
        -H "Content-Type: application/json" \
        -d "{\"name\":\"${token_name}\"}" \
        "${grafana_url}/api/serviceaccounts/${sa_id}/tokens" 2>/dev/null \
        | grep -o '"key":"[^"]*"' | head -1 | sed -E 's/.*"key":"([^"]+)".*/\1/')

    if [ -z "$token" ]; then
        echo "Error: failed to mint a token for the Grafana '${sa_name}' service account." >&2
        return 1
    fi

    if ! atomic_set_env_values FLEET_ALERTS_GRAFANA_TOKEN "$token"; then
        echo "Error: could not persist the Grafana service-account token." >&2
        return 1
    fi
    echo "Provisioned Grafana service-account token for fleet-api (stored in $ENV_FILE)."

    echo "Restarting fleet-api to pick up the Grafana token…"
    if ! compose up -d --no-deps --force-recreate fleet-api; then
        echo "Error: wrote the Grafana token to $ENV_FILE but failed to restart fleet-api; it is still" >&2
        echo "       running with the pre-token environment and will 401 against Grafana. Restart it with:" >&2
        echo "         docker compose --project-name $FLEET_COMPOSE_PROJECT_NAME ${COMPOSE_ENV_ARGS[*]} ${COMPOSE_FILES[*]} up -d --no-deps --force-recreate fleet-api" >&2
        return 1
    fi
}

if [ "$ENABLE_BETA_ALERTS" = "true" ]; then
    if ! provision_grafana_db_role; then
        echo "Error: Grafana DB role provisioning failed; Grafana alerting cannot query notification_metric_sample." >&2
        echo "       Fix the underlying cause (DB reachable, migrations complete) and re-run this script." >&2
        exit 1
    fi
    if ! provision_grafana_service_account_token; then
        echo "Warning: Grafana service-account token provisioning failed; fleet-api cannot authenticate to Grafana" >&2
        echo "         so alert channel/rule/silence management will 401 until this succeeds." >&2
        echo "         Re-run this script (Grafana must be reachable on 127.0.0.1:3030) to retry." >&2
    fi
fi

# Token provisioning can recreate fleet-api after the initial post-migration
# probe. Make the cleanup boundary definitive: never consume the preflight
# proof or remove recovery images unless the final API process is DB-ready and
# nginx is serving (or redirecting to) the frontend.
if ! wait_for_fleet_api_ready || ! wait_for_fleet_client_ready; then
    echo "Error: refusing to consume the preflight proof or remove previous release images." >&2
    exit 1
fi

# ----------------------------------------------------------------------------
# Docker Cleanup
# ----------------------------------------------------------------------------

echo "Cleaning up old Docker images and build cache..."
prune_obsolete_release_images
docker image prune -f 2>/dev/null || true
docker builder prune -f 2>/dev/null || true

echo "--------------------------------------------------------------"
echo "Proto Fleet is now running!"
echo ""
echo "Access URLs:"
protocol="http"
[ "$PROTOCOL_MODE" == "https" ] && protocol="https"
echo "  Local:  ${protocol}://localhost"
for ip in $(get_local_ips); do
    echo "  LAN:    ${protocol}://$ip"
done
echo "--------------------------------------------------------------"

if ! rm -f "$PREFLIGHT_MARKER"; then
    echo "Error: Fleet is running, but the consumed preflight marker could not be removed: $PREFLIGHT_MARKER" >&2
    exit 1
fi

exit 0
