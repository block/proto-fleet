#!/bin/bash
# Exercises the non-interactive upgrade boundary without touching host Docker,
# networking, or services. External commands are recorded through PATH shims.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR=$(mktemp -d)
REAL_AWK=$(command -v awk)
REAL_CHMOD=$(command -v chmod)
REAL_CHOWN=$(command -v chown)
REAL_GREP=$(command -v grep)
REAL_MKTEMP=$(command -v mktemp)
REAL_MV=$(command -v mv)
REAL_STAT=$(command -v stat)
FAILURES=0
RELEASE_TAG="v1.2.3"
FLEET_API_IMAGE="proto-fleet-api:${RELEASE_TAG}"
FLEET_CLIENT_IMAGE="proto-fleet-client:${RELEASE_TAG}"
TIMESCALEDB_IMAGE="proto-fleet-timescaledb:${RELEASE_TAG}"
TIMESCALEDB_HA_IMAGE="proto-fleet-timescaledb-ha:${RELEASE_TAG}"

trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
    echo "FAIL: $*" >&2
    FAILURES=$((FAILURES + 1))
}

pass() {
    echo "ok: $*"
}

assert_contains() {
    local description="$1" file="$2" expected="$3"
    if grep -qF -- "$expected" "$file"; then
        pass "$description"
    else
        fail "$description: expected '$expected' in $file"
    fi
}

assert_not_contains() {
    local description="$1" file="$2" unexpected="$3"
    if grep -qF -- "$unexpected" "$file"; then
        fail "$description: unexpected '$unexpected' in $file"
    else
        pass "$description"
    fi
}

file_owner_group() {
    local owner_group
    owner_group=$(stat -c '%u:%g' "$1" 2>/dev/null) ||
        owner_group=$(stat -f '%u:%g' "$1" 2>/dev/null) || return 1
    [ -n "$owner_group" ] || return 1
    printf '%s' "$owner_group"
}

file_mode() {
    local mode
    mode=$(stat -c '%a' "$1" 2>/dev/null) ||
        mode=$(stat -f '%Lp' "$1" 2>/dev/null) || return 1
    [ -n "$mode" ] || return 1
    printf '%s' "$mode"
}

write_valid_env() {
    local env_file="$1"
    printf '%s\n' \
        'DB_USERNAME=fleet' \
        'DB_PASSWORD=test-password' \
        'AUTH_CLIENT_SECRET_KEY=01234567890123456789012345678901' \
        'ENCRYPT_SERVICE_MASTER_KEY=MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=' \
        'SESSION_COOKIE_SECURE=true' \
        'SESSION_COOKIE_SECURE="false"' \
        'ENABLE_BETA_ALERTS=true' \
        'ENABLE_BETA_ALERTS="false"' \
        'ENABLE_SYSTEM_MONITORING=false' \
        'ENABLE_TRACING=true' \
        'ENABLE_ONE_CLICK_UPDATES=true' > "$env_file"
    printf 'ENABLE_TRACING="FALSE" \r\n' >> "$env_file"
    printf 'ENABLE_ONE_CLICK_UPDATES="FALSE" \r\n' >> "$env_file"
}

# Build a minimal docker-save-shaped archive whose manifest carries the given
# RepoTags JSON fragment (comma-separated, pre-quoted).
write_tsdb_archive() {
    local stage="$1" repo_tags="$2"
    local fixture="$stage/image-fixture"
    mkdir -p "$stage/images" "$fixture"
    printf '[{"Config":"config.json","RepoTags":[%s],"Layers":[]}]\n' \
        "$repo_tags" > "$fixture/manifest.json"
    (cd "$fixture" && tar -cf - manifest.json) | gzip > "$stage/images/timescaledb.tar.gz"
    rm -rf "$fixture"
}

enable_valid_alerts() {
    local env_file="$1"
    printf '%s\n' \
        'ENABLE_BETA_ALERTS=true' \
        'GRAFANA_ADMIN_PASSWORD=admin-secret' \
        'GRAFANA_DB_USERNAME=grafana_ro' \
        'GRAFANA_DB_PASSWORD=db-secret' \
        'FLEET_ALERTS_WEBHOOK_TOKEN=webhook-secret' \
        'GRAFANA_SECRET_KEY=grafana-secret' \
        'FLEET_ALERTS_GRAFANA_TOKEN=service-token' >> "$env_file"
}

write_release_manifest() {
    local stage="$1"
    local -a hasher=(sha256sum)
    command -v sha256sum >/dev/null 2>&1 || hasher=(shasum -a 256)
    (
        cd "$stage" || exit 1
        # Must exclude the same operator-owned paths as find_release_entries
        # in run-fleet.sh.
        find . -type f \
            ! -path './.env' \
            ! -path './.update-preflight-complete' \
            ! -path './.update-preflight-complete.tmp.*' \
            ! -path './client/nginx.conf' \
            ! -path './ssl/*' \
            ! -path './server/influx_config/.env' \
            ! -path './ha/node.env' \
            ! -path './deployment-manifest.sha256' \
            -print0 | LC_ALL=C sort -z | xargs -0 "${hasher[@]}" > deployment-manifest.sha256
    )
}

make_stage() {
    local name="$1"
    STAGE="$TMP_DIR/$name"
    local runtime="$STAGE-runtime"
    HARNESS_BIN_DIR="$runtime/bin"
    HARNESS_CALL_LOG="$runtime/calls.log"
    HARNESS_OUTPUT_LOG="$runtime/output.log"
    HARNESS_UPDATER_STATE_FILE="$runtime/updater-state"
    mkdir -p "$STAGE/client" "$STAGE/ha" "$STAGE/profiles" "$STAGE/server/monitoring/grafana/provisioning/datasources" "$HARNESS_BIN_DIR"
    : > "$HARNESS_CALL_LOG"
    : > "$HARNESS_OUTPUT_LOG"
    printf 'not-found\n' > "$HARNESS_UPDATER_STATE_FILE"
    cp "$DEPLOY_DIR/run-fleet.sh" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.alerts.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.system-monitoring.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.tracing.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.updater.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/ha/compose.yaml" "$STAGE/ha/"
    cp "$DEPLOY_DIR/profiles/mini.env" "$STAGE/profiles/"
    cp "$DEPLOY_DIR/client/nginx.http.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/client/nginx.https.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/server/otel-collector-config.datadog.yaml" "$STAGE/server/"
    printf 'apiVersion: 1\n' > "$STAGE/server/monitoring/grafana/provisioning/datasources/base.yaml"
    printf '%s\n' "version: $RELEASE_TAG" 'commit: test-release' > "$STAGE/version.txt"
    "$DEPLOY_DIR/scripts/pin-release-images.sh" "$STAGE" "$RELEASE_TAG"
    write_tsdb_archive "$STAGE" "\"$TIMESCALEDB_IMAGE\",\"$TIMESCALEDB_HA_IMAGE\""
    write_valid_env "$STAGE/.env"

    cat > "$HARNESS_BIN_DIR/docker" <<'EOF'
#!/bin/bash
printf 'docker' >> "$CALL_LOG"
printf ' %s' "$@" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"

if [ "${1:-}" = "image" ] && [ "${2:-}" = "ls" ]; then
    repository=''
    for argument in "$@"; do
        case "$argument" in
            proto-fleet-api|proto-fleet-client|proto-fleet-timescaledb|proto-fleet-timescaledb-ha)
                repository="$argument"
                ;;
        esac
    done
    if [ "${FAKE_IMAGE_LIST_FAILURE:-}" = "$repository" ]; then
        exit 1
    fi
    for image in ${FAKE_RELEASE_IMAGES:-}; do
        case "$image" in
            "$repository":*) printf '%s\n' "$image" ;;
        esac
    done
    exit 0
fi

if [ "${1:-}" = "container" ] && [ "${2:-}" = "ls" ]; then
    case " $* " in
        *" --filter label=com.docker.compose.project="*" --format {{.Image}} "*)
            if [ "${FAKE_ACTIVE_IMAGE_LIST_FAILURE:-false}" = "true" ]; then
                exit 1
            fi
            for image in ${FAKE_ACTIVE_RELEASE_IMAGES:-}; do
                printf '%s\n' "$image"
            done
            exit 0
            ;;
    esac
    if [ "${FAKE_CONTAINER_LIST_FAILURE:-false}" = "true" ]; then
        exit 1
    fi
    ancestor=''
    for argument in "$@"; do
        case "$argument" in
            ancestor=*) ancestor="${argument#ancestor=}" ;;
        esac
    done
    for image in ${FAKE_CONTAINER_IMAGE_REFS:-}; do
        if [ "sha256:test-$image" = "$ancestor" ]; then
            printf 'container-using-%s\n' "${image##*:}"
            break
        fi
    done
    exit 0
fi

if [ "${1:-}" = "image" ] && [ "${2:-}" = "rm" ]; then
    [ "${FAKE_IMAGE_RM_FAILURE:-}" != "${3:-}" ]
    exit
fi

case " $* " in
    *" compose up --help "*)
        echo 'Options: --wait --wait-timeout --no-build --pull string'
        ;;
    *" compose "*" config --quiet "*)
        [ "${FAKE_COMPOSE_CONFIG_FAILURE:-false}" != "true" ]
        ;;
    *" compose "*" config --images "*)
        if [ "${FAKE_SOURCE_INSTALL:-false}" = "true" ]; then
            printf '%s\n' \
                'proto-fleet-api:latest' \
                'proto-fleet-client:latest' \
                'proto-fleet-timescaledb:latest'
        elif [ "${FAKE_COMPOSE_USES_LATEST:-false}" = "true" ]; then
            printf '%s\n' \
                'proto-fleet-api:latest' \
                'proto-fleet-client:v1.2.3' \
                'proto-fleet-timescaledb:v1.2.3'
        else
            printf '%s\n' \
                'proto-fleet-api:v1.2.3' \
                'proto-fleet-client:v1.2.3' \
                'proto-fleet-timescaledb:v1.2.3'
        fi
        ;;
    *" compose "*" config "*)
        project_name=''
        previous=''
        for argument in "$@"; do
            if [ "$previous" = "--project-name" ]; then
                project_name="$argument"
            fi
            if [ "$previous" = "-f" ]; then
                printf 'compose_file: %s\n' "$argument"
            fi
            previous="$argument"
        done
        printf 'name: %s\n' "${project_name:-${STAGE_ROOT##*/}}"
        ;;
    *" compose "*" pull --ignore-buildable "*)
        if [ "${FAKE_TSDB_IMAGE_COLD_CACHE:-false}" = "true" ]; then
            echo 'Image proto-fleet-timescaledb:v1.2.3 Skipped'
        fi
        ;;
    *" compose "*" build --no-cache "*)
        if [ "${FAKE_MUTATE_TSDB_DURING_BUILD:-false}" = "true" ]; then
            printf 'changed-during-build' >> "$STAGE_ROOT/images/timescaledb.tar.gz"
        fi
        ;;
    *" compose "*" up --remove-orphans "*)
        [ "${FAKE_ACTIVATION_FAILURE:-false}" != "true" ]
        ;;
    *" compose "*" exec "*)
        echo 't'
        ;;
    *" volume ls -q "*)
        if [ -n "${FAKE_DOCKER_VOLUME:-}" ]; then
            printf '%s\n' "$FAKE_DOCKER_VOLUME"
        fi
        ;;
    *" image inspect --format "*)
        image="${!#}"
        if [ "${FAKE_IMAGE_INSPECT_FAILURE:-}" = "$image" ]; then
            exit 1
        fi
        if [ "${FAKE_PREPARED_IMAGE_MISSING:-false}" = "true" ] && [ "$image" = "proto-fleet-api:v1.2.3" ]; then
            exit 1
        fi
        if [ "${FAKE_PREPARED_IMAGE_CHANGED:-false}" = "true" ] && [ "$image" = "proto-fleet-api:v1.2.3" ]; then
            printf 'sha256:changed-%s\n' "$image"
            exit 0
        fi
        printf 'sha256:test-%s\n' "$image"
        ;;
    *" image inspect proto-fleet-timescaledb:v1.2.3 "*|*" image inspect proto-fleet-timescaledb-ha:v1.2.3 "*)
        if [ "${FAKE_TSDB_IMAGE_COLD_CACHE:-false}" = "true" ] && ! grep -qF 'docker load' "$CALL_LOG"; then
            exit 1
        fi
        [ "${FAKE_TSDB_IMAGE_MISSING:-false}" != "true" ]
        ;;
esac
EOF

    cat > "$HARNESS_BIN_DIR/systemctl" <<'EOF'
#!/bin/bash
printf 'systemctl %s\n' "$*" >> "$CALL_LOG"
[ "${FAKE_SYSTEMD_UNAVAILABLE:-false}" != "true" ] || exit 1
case " $* " in
    *" proto-fleet-updater.service "*)
        updater_state=$(cat "$UPDATER_STATE_FILE")
        case "$*" in
            *'--property=LoadState --value'*)
                if [ "$updater_state" = "not-found" ]; then
                    printf 'not-found\n'
                else
                    printf 'loaded\n'
                fi
                ;;
            *'--property=ActiveState --value'*) printf '%s\n' "$updater_state" ;;
            'stop proto-fleet-updater.service') printf 'inactive\n' > "$UPDATER_STATE_FILE" ;;
            'restart proto-fleet-updater.service') printf 'active\n' > "$UPDATER_STATE_FILE" ;;
            'is-active --quiet proto-fleet-updater.service') [ "$updater_state" = "active" ] ;;
            *) exit 1 ;;
        esac
        ;;
esac
exit 0
EOF

    cat > "$HARNESS_BIN_DIR/id" <<'EOF'
#!/bin/bash
if [ "${1:-}" = "-u" ]; then
    echo 0
else
    /usr/bin/id "$@"
fi
EOF

    cat > "$HARNESS_BIN_DIR/uname" <<'EOF'
#!/bin/bash
echo Linux
EOF

    cat > "$HARNESS_BIN_DIR/grep" <<'EOF'
#!/bin/bash
if [ "${FAKE_WSL:-false}" = "true" ]; then
    for argument in "$@"; do
        if [ "$argument" = "/proc/version" ]; then
            exit 0
        fi
    done
fi
exec "$REAL_GREP" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/mktemp" <<'EOF'
#!/bin/bash
if [ "${FAKE_ENV_REWRITE_FAILURE:-}" = "mktemp" ] && \
    [ "${1:-}" = "$STAGE_ROOT/.env.tmp.XXXXXX" ]; then
    exit 1
fi
exec "$REAL_MKTEMP" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/stat" <<'EOF'
#!/bin/bash
target="${@: -1}"
if [ "$target" = "$STAGE_ROOT/.env" ]; then
    case "${FAKE_ENV_REWRITE_FAILURE:-}" in
        metadata) exit 1 ;;
        chown)
            printf '99999:99999\n'
            exit 0
            ;;
    esac
fi
exec "$REAL_STAT" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/chown" <<'EOF'
#!/bin/bash
target="${@: -1}"
if [ "${FAKE_ENV_REWRITE_FAILURE:-}" = "chown" ] && \
    [[ "$target" == "$STAGE_ROOT/.env.tmp."* ]]; then
    exit 1
fi
exec "$REAL_CHOWN" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/chmod" <<'EOF'
#!/bin/bash
target="${@: -1}"
if [ "${FAKE_ENV_REWRITE_FAILURE:-}" = "chmod" ] && \
    [[ "$target" == "$STAGE_ROOT/.env.tmp."* ]]; then
    exit 1
fi
exec "$REAL_CHMOD" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/awk" <<'EOF'
#!/bin/bash
if [ "${FAKE_ENV_REWRITE_FAILURE:-}" = "filter" ]; then
    for argument in "$@"; do
        if [ "$argument" = "$STAGE_ROOT/.env" ]; then
            printf 'PARTIAL_REPLACEMENT=true\n'
            exit 2
        fi
    done
fi
exec "$REAL_AWK" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/mv" <<'EOF'
#!/bin/bash
source_path="${@: -2:1}"
destination_path="${@: -1}"
if [ "${FAKE_ENV_REWRITE_FAILURE:-}" = "mv" ] && \
    [[ "$source_path" == "$STAGE_ROOT/.env.tmp."* ]] && \
    [ "$destination_path" = "$STAGE_ROOT/.env" ]; then
    exit 1
fi
exec "$REAL_MV" "$@"
EOF

    cat > "$HARNESS_BIN_DIR/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$CALL_LOG"
case " $* " in
    *registry-1.docker.io*)
        [ "${FAKE_REGISTRY_FAILURE:-false}" != "true" ]
        ;;
    *"/health/ready"*)
        [ "${FAKE_API_READINESS_FAILURE:-false}" != "true" ]
        ;;
    *"://127.0.0.1/"*)
        [ "${FAKE_CLIENT_READINESS_FAILURE:-false}" != "true" ]
        ;;
    *) exit 0 ;;
esac
EOF

    cat > "$HARNESS_BIN_DIR/sudo" <<'EOF'
#!/bin/bash
printf 'sudo %s\n' "$*" >> "$CALL_LOG"
exit 0
EOF

    cat > "$HARNESS_BIN_DIR/hostname" <<'EOF'
#!/bin/bash
if [ "${1:-}" = "-I" ]; then
    echo '192.0.2.10'
else
    echo 'proto-fleet-test'
fi
EOF

    cat > "$HARNESS_BIN_DIR/ip" <<'EOF'
#!/bin/bash
exit 0
EOF

    chmod +x "$STAGE/run-fleet.sh" "$HARNESS_BIN_DIR/"*
    write_release_manifest "$STAGE"
}

run_stage() {
    local stage="$1"
    shift
    CALL_LOG="$HARNESS_CALL_LOG" \
    STAGE_ROOT="$stage" \
    REAL_AWK="$REAL_AWK" \
    REAL_CHMOD="$REAL_CHMOD" \
    REAL_CHOWN="$REAL_CHOWN" \
    REAL_GREP="$REAL_GREP" \
    REAL_MKTEMP="$REAL_MKTEMP" \
    REAL_MV="$REAL_MV" \
    REAL_STAT="$REAL_STAT" \
    UPDATER_STATE_FILE="$HARNESS_UPDATER_STATE_FILE" \
    PATH="$HARNESS_BIN_DIR:$PATH" \
    /bin/bash "$stage/run-fleet.sh" "$@" > "$HARNESS_OUTPUT_LOG" 2>&1
}

# The packaged updater overlay is activated only after the installer has
# successfully started the host service.
make_stage help
if run_stage "$STAGE" --help; then
    assert_contains "help documents the updater overlay" "$HARNESS_OUTPUT_LOG" "enable-one-click-updates"
    assert_contains "help documents the updater fallback" "$HARNESS_OUTPUT_LOG" "disable-one-click-updates"
else
    fail "--help should succeed"
fi

# Ordinary source-tree runs retain their directory-derived project identity,
# so a checkout cannot stop or reuse resources from an installed `deployment`
# project on the same Docker daemon.
make_stage source-layout
rm -f "$STAGE/version.txt" "$STAGE/deployment-manifest.sha256" "$STAGE/images/timescaledb.tar.gz"
cp "$DEPLOY_DIR/docker-compose.yaml" "$STAGE/"
cp "$DEPLOY_DIR/ha/compose.yaml" "$STAGE/ha/"
if FAKE_SOURCE_INSTALL=true run_stage "$STAGE" --non-interactive; then
    pass "source-tree run succeeds with its own Compose project"
else
    fail "source-tree run should preserve its Compose project identity"
fi
assert_contains "source-tree run targets its directory project" "$HARNESS_CALL_LOG" "compose --project-name source-layout"
assert_not_contains "source-tree run cannot target an installed deployment project" "$HARNESS_CALL_LOG" "compose --project-name deployment "
assert_contains "source-tree run reaches teardown in its isolated project" "$HARNESS_CALL_LOG" " down --remove-orphans"

# Existing process-level overrides remain authoritative and are reused by
# volume detection. Do not persist a new multi-install identity here: the
# surrounding migration and uninstall tools do not support that contract.
make_stage project-override
printf 'COMPOSE_PROJECT_NAME=persisted-project\n' >> "$STAGE/.env"
if COMPOSE_PROJECT_NAME=fleet-blue run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "explicit Compose project override preflights"
else
    fail "explicit Compose project override should be preserved"
fi
assert_contains "explicit project override reaches Compose" "$HARNESS_CALL_LOG" "compose --project-name fleet-blue"
assert_not_contains "process project override beats persisted identity" "$HARNESS_CALL_LOG" "compose --project-name persisted-project"

# Compose historically accepted a project identity from the deployment .env.
# Preserve its last non-empty assignment when the process has no override.
make_stage persisted-project
printf 'COMPOSE_PROJECT_NAME=ignored-project\n' >> "$STAGE/.env"
printf 'export COMPOSE_PROJECT_NAME: "fleet-blue" # retained identity\r\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "persisted Compose project name preflights"
else
    fail "persisted Compose project name should be preserved"
fi
assert_contains "last persisted project name reaches Compose" "$HARNESS_CALL_LOG" "compose --project-name fleet-blue"
assert_not_contains "earlier persisted project name is ignored" "$HARNESS_CALL_LOG" "compose --project-name ignored-project"

# Compose also accepts `KEY: value`. Read true overlay settings through that
# syntax and atomically normalize every prior form to one final assignment.
make_stage compose-env-booleans
printf 'active\n' > "$HARNESS_UPDATER_STATE_FILE"
enable_valid_alerts "$STAGE/.env"
printf 'DD_API_KEY=test-datadog-key\n' >> "$STAGE/.env"
printf '%s\n' \
    'export ENABLE_BETA_ALERTS: true # preserve alerts' \
    'ENABLE_SYSTEM_MONITORING: "true"' \
    'ENABLE_TRACING: TRUE' \
    'ENABLE_ONE_CLICK_UPDATES: "true"' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "Compose colon-form overlay settings preflight"
else
    fail "Compose colon-form overlay settings should be preserved"
fi
for persisted_boolean in ENABLE_BETA_ALERTS ENABLE_SYSTEM_MONITORING ENABLE_TRACING ENABLE_ONE_CLICK_UPDATES; do
    if [ "$(grep -Ec "^[[:space:]]*(export[[:space:]]+)?${persisted_boolean}[[:space:]]*[:=]" "$STAGE/.env")" -eq 1 ] \
        && grep -q "^${persisted_boolean}=true$" "$STAGE/.env"; then
        pass "$persisted_boolean normalizes Compose colon syntax without changing its value"
    else
        fail "$persisted_boolean did not preserve its Compose colon-form true value"
    fi
done
assert_contains "enabled one-click updates layer the socket overlay" "$HARNESS_CALL_LOG" "-f $STAGE/docker-compose.updater.yaml"

# Exercise the exact installer contract: a successful service bootstrap must
# override a persisted false value, layer the socket overlay, and survive the
# preflight-to-activation handoff.
make_stage enable-updater-overlay
# The installer validates and then stops the loaded service while run-fleet
# changes the deployment; it restarts the updater only after this handoff.
printf 'inactive\n' > "$HARNESS_UPDATER_STATE_FILE"
if run_stage "$STAGE" --enable-one-click-updates --non-interactive --preflight-only; then
    pass "installer updater enablement preflights"
else
    fail "installer updater enablement should override persisted false state"
fi
assert_contains "installer updater enablement layers the socket overlay" "$HARNESS_CALL_LOG" "-f $STAGE/docker-compose.updater.yaml"
if [ "$(grep -c '^ENABLE_ONE_CLICK_UPDATES=' "$STAGE/.env")" -eq 1 ] \
    && grep -q '^ENABLE_ONE_CLICK_UPDATES=true$' "$STAGE/.env"; then
    pass "installer updater enablement persists enabled state"
else
    fail "installer updater enablement did not normalize persisted state to true"
fi
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --skip-build; then
    pass "enabled updater activation succeeds"
else
    fail "enabled updater state should survive activation"
fi
assert_contains "enabled updater activation retains the socket overlay" "$HARNESS_CALL_LOG" "-f $STAGE/docker-compose.updater.yaml"

# A manual runner operating on a deployment whose fleet-api can still reach
# the updater must stop that writer before Compose teardown and restore it on
# every exit. Updater-owned children identify themselves explicitly and the
# staged preflight remains non-mutating with respect to the active tree.
make_stage direct-updater-serialization
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
printf 'active\n' > "$HARNESS_UPDATER_STATE_FILE"
if run_stage "$STAGE" --non-interactive; then
    pass "direct deployment run serializes with the active host updater"
else
    fail "direct deployment run should stop and restore the host updater"
fi
direct_stop_line=$(grep -n '^systemctl stop proto-fleet-updater.service$' "$HARNESS_CALL_LOG" | cut -d: -f1)
direct_down_line=$(grep -n ' compose .* down --remove-orphans$' "$HARNESS_CALL_LOG" | cut -d: -f1)
direct_restart_line=$(grep -n '^systemctl restart proto-fleet-updater.service$' "$HARNESS_CALL_LOG" | cut -d: -f1)
if [ -n "$direct_stop_line" ] \
    && [ -n "$direct_down_line" ] \
    && [ -n "$direct_restart_line" ] \
    && [ "$direct_stop_line" -lt "$direct_down_line" ] \
    && [ "$direct_down_line" -lt "$direct_restart_line" ] \
    && [ "$(cat "$HARNESS_UPDATER_STATE_FILE")" = active ]; then
    pass "direct runner holds the updater quiesced across deployment mutation"
else
    fail "direct runner did not serialize its complete mutation window"
fi

make_stage direct-updater-failure
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
printf 'active\n' > "$HARNESS_UPDATER_STATE_FILE"
if FAKE_COMPOSE_CONFIG_FAILURE=true run_stage "$STAGE" --non-interactive; then
    fail "direct runner fixture should fail after updater quiescing"
else
    pass "direct runner failure exercises updater EXIT restoration"
fi
assert_contains "failed direct run stops the updater first" "$HARNESS_CALL_LOG" "systemctl stop proto-fleet-updater.service"
assert_contains "failed direct run restores the updater" "$HARNESS_CALL_LOG" "systemctl restart proto-fleet-updater.service"
if [ "$(cat "$HARNESS_UPDATER_STATE_FILE")" = active ]; then
    pass "failed direct run leaves the updater active"
else
    fail "failed direct run left the updater stopped"
fi

# Process-level Compose overrides select the new effective state, but cannot
# erase evidence that the running deployment was wired to the updater. The
# active daemon must be quiesced before a persisted true value is changed.
make_stage process-disable-active-updater
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
printf 'active\n' > "$HARNESS_UPDATER_STATE_FILE"
if ENABLE_ONE_CLICK_UPDATES=false \
    run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "process override can disable a persisted updater safely"
else
    fail "process override failed while disabling a persisted updater"
fi
assert_contains "persisted updater state survives process override for serialization" \
    "$HARNESS_CALL_LOG" "systemctl stop proto-fleet-updater.service"
if [ "$(grep -c '^ENABLE_ONE_CLICK_UPDATES=' "$STAGE/.env")" -eq 1 ] \
    && grep -q '^ENABLE_ONE_CLICK_UPDATES=false$' "$STAGE/.env" \
    && [ "$(cat "$HARNESS_UPDATER_STATE_FILE")" = active ]; then
    pass "process disable persists false after updater serialization"
else
    fail "process disable did not persist or restore updater state correctly"
fi

make_stage updater-managed-runner
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
printf 'active\n' > "$HARNESS_UPDATER_STATE_FILE"
if PROTO_FLEET_UPDATER_MANAGED_RUN=1 run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "updater preflight remains available while the daemon is active"
else
    fail "preflight should not attempt to stop its owning updater"
fi
assert_not_contains "preflight does not stop its updater" "$HARNESS_CALL_LOG" "systemctl stop proto-fleet-updater.service"
: > "$HARNESS_CALL_LOG"
if PROTO_FLEET_UPDATER_MANAGED_RUN=1 run_stage "$STAGE" --non-interactive --skip-build; then
    pass "updater-managed activation bypasses direct-run quiescing"
else
    fail "updater-managed activation should not stop its parent daemon"
fi
assert_not_contains "managed activation does not stop its updater" "$HARNESS_CALL_LOG" "systemctl stop proto-fleet-updater.service"
assert_not_contains "managed activation leaves lifecycle ownership with the daemon" "$HARNESS_CALL_LOG" "systemctl restart proto-fleet-updater.service"

# An installer bootstrap failure must explicitly override previously persisted
# state so fleet-api does not advertise or mount a dead host updater.
make_stage disable-updater-overlay
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
if run_stage "$STAGE" --disable-one-click-updates --non-interactive --preflight-only; then
    pass "explicit updater fallback preflights"
else
    fail "explicit updater fallback should override persisted true state"
fi
assert_not_contains "explicit updater fallback omits the socket overlay" "$HARNESS_CALL_LOG" "docker-compose.updater.yaml"
if [ "$(grep -c '^ENABLE_ONE_CLICK_UPDATES=' "$STAGE/.env")" -eq 1 ] \
    && grep -q '^ENABLE_ONE_CLICK_UPDATES=false$' "$STAGE/.env"; then
    pass "explicit updater fallback persists disabled state"
else
    fail "explicit updater fallback did not normalize persisted state to false"
fi

# Once one-click updates ship, a host may lose its systemd manager before a
# later installer run. An explicit disable remains safe because no systemd
# updater can be active, while leaving the feature enabled must still fail
# before any deployment mutation.
make_stage disable-updater-without-systemd
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
if FAKE_WSL=true FAKE_SYSTEMD_UNAVAILABLE=true \
    run_stage "$STAGE" --disable-one-click-updates --non-interactive --preflight-only; then
    pass "explicit updater fallback survives an unavailable systemd manager"
else
    fail "unavailable systemd manager blocked the explicit updater fallback"
fi
if [ "$(grep -c '^ENABLE_ONE_CLICK_UPDATES=' "$STAGE/.env")" -eq 1 ] \
    && grep -q '^ENABLE_ONE_CLICK_UPDATES=false$' "$STAGE/.env"; then
    pass "non-systemd fallback persists disabled updater state"
else
    fail "non-systemd fallback did not normalize persisted updater state"
fi

make_stage enabled-updater-without-systemd
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
if FAKE_WSL=true FAKE_SYSTEMD_UNAVAILABLE=true \
    run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "enabled updater should not proceed without a systemd manager"
else
    pass "enabled updater fails closed without a systemd manager"
fi
assert_contains "missing updater manager is diagnosed" "$HARNESS_OUTPUT_LOG" \
    "systemd manager is unavailable"
assert_not_contains "missing updater manager prevents Docker activity" \
    "$HARNESS_CALL_LOG" "docker "

make_stage enabled-updater-without-service
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "enabled updater should not proceed without its systemd unit"
else
    pass "enabled updater fails closed when its systemd unit is missing"
fi
assert_contains "missing updater service is diagnosed" "$HARNESS_OUTPUT_LOG" \
    "proto-fleet-updater.service is not installed"
assert_not_contains "missing updater service prevents environment and Docker mutation" \
    "$HARNESS_CALL_LOG" "docker "

# Persisted one-click state must fail before Docker activity when its release
# bundle is incomplete; silently omitting the socket would expose a false
# upgrade capability state to Fleet API.
make_stage missing-updater-overlay
printf 'ENABLE_ONE_CLICK_UPDATES=true\n' >> "$STAGE/.env"
rm -f "$STAGE/docker-compose.updater.yaml"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing updater overlay should fail preflight"
else
    pass "missing updater overlay fails closed"
fi
assert_contains "missing updater overlay is diagnosed" "$HARNESS_OUTPUT_LOG" "one-click updates are enabled but $STAGE/docker-compose.updater.yaml is missing"
assert_not_contains "missing updater overlay prevents Docker activity" "$HARNESS_CALL_LOG" "docker "

# Required credentials use the same literal dotenv forms accepted for project
# identity and booleans. Mixed duplicate delimiters, export prefixes, quoting,
# and inline comments must validate the values Compose will actually consume.
make_stage compose-env-credentials
printf '%s\n' \
    'DB_USERNAME: "fleet" # database role' \
    "export DB_PASSWORD: 'test\$password' # literal dollar" \
    'AUTH_CLIENT_SECRET_KEY: "01234567890123456789012345678901" # auth secret' \
    'ENCRYPT_SERVICE_MASTER_KEY: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=" # encryption key' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "Compose dotenv credential forms preflight"
else
    fail "Compose dotenv credential forms should validate their effective values"
fi

make_stage compose-env-profile
printf 'FLEET_PROFILE: "MINI" # low-power host\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "Compose colon-form host profile preflights"
else
    fail "Compose colon-form host profile should select its profile env"
fi
assert_contains "colon-form host profile reaches Compose" "$HARNESS_CALL_LOG" "--env-file $STAGE/profiles/mini.env"

make_stage interpolated-env
printf 'AUTH_CLIENT_SECRET_KEY: "${SHORT_SECRET:-01234567890123456789012345678901}"\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "interpolated persisted secrets should fail closed"
else
    pass "interpolated persisted secrets fail before validation diverges from Compose"
fi
assert_contains "unsupported interpolation is diagnosed without printing its value" "$HARNESS_OUTPUT_LOG" "AUTH_CLIENT_SECRET_KEY in $STAGE/.env uses unsupported Compose dotenv syntax"
assert_not_contains "unsupported interpolation makes no Docker calls" "$HARNESS_CALL_LOG" "docker "

# Optional overlay values are runner-consumed only while their overlay is
# active. Dormant Compose interpolation must not block an otherwise unrelated
# preflight, but the same syntax must still fail closed before Docker activity
# once the matching overlay is enabled.
make_stage dormant-overlay-interpolation
printf '%s\n' \
    'DD_API_KEY=${DATADOG_API_KEY}' \
    'GRAFANA_ADMIN_PASSWORD=${GRAFANA_ADMIN_PASSWORD_FROM_VAULT}' \
    'GRAFANA_DB_USERNAME=${GRAFANA_DB_USERNAME_FROM_VAULT}' \
    'GRAFANA_DB_PASSWORD=${GRAFANA_DB_PASSWORD_FROM_VAULT}' \
    'GRAFANA_SECRET_KEY=${GRAFANA_SECRET_KEY_FROM_VAULT}' \
    'FLEET_ALERTS_WEBHOOK_TOKEN=${FLEET_ALERTS_WEBHOOK_TOKEN_FROM_VAULT}' \
    'FLEET_ALERTS_GRAFANA_TOKEN=${FLEET_ALERTS_GRAFANA_TOKEN_FROM_VAULT}' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "disabled overlays tolerate dormant Compose interpolation"
else
    fail "disabled overlays should ignore values they do not consume"
fi
assert_not_contains "disabled tracing omits its Compose overlay" "$HARNESS_CALL_LOG" "docker-compose.tracing.yaml"
assert_not_contains "disabled alerts omit their Compose overlay" "$HARNESS_CALL_LOG" "docker-compose.alerts.yaml"
assert_not_contains "disabled one-click updates omit the socket overlay" "$HARNESS_CALL_LOG" "docker-compose.updater.yaml"
assert_contains "dormant tracing value is preserved" "$STAGE/.env" 'DD_API_KEY=${DATADOG_API_KEY}'
assert_contains "dormant alert value is preserved" "$STAGE/.env" 'GRAFANA_ADMIN_PASSWORD=${GRAFANA_ADMIN_PASSWORD_FROM_VAULT}'

make_stage interpolated-tracing-env
printf '%s\n' \
    'ENABLE_TRACING=true' \
    'DD_API_KEY=${DATADOG_API_KEY}' >> "$STAGE/.env"
cp "$STAGE/.env" "$HARNESS_OUTPUT_LOG.env-before"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "enabled tracing should reject unsupported DD_API_KEY interpolation"
else
    pass "enabled tracing validates its API key syntax"
fi
assert_contains "enabled tracing interpolation is diagnosed" "$HARNESS_OUTPUT_LOG" "DD_API_KEY in $STAGE/.env uses unsupported Compose dotenv syntax"
assert_not_contains "invalid tracing interpolation makes no Docker calls" "$HARNESS_CALL_LOG" "docker "
if cmp -s "$HARNESS_OUTPUT_LOG.env-before" "$STAGE/.env"; then
    pass "invalid tracing interpolation leaves the environment unchanged"
else
    fail "invalid tracing interpolation should not rewrite the environment"
fi

make_stage interpolated-alerts-env
enable_valid_alerts "$STAGE/.env"
printf 'GRAFANA_ADMIN_PASSWORD=${GRAFANA_ADMIN_PASSWORD_FROM_VAULT}\n' >> "$STAGE/.env"
cp "$STAGE/.env" "$HARNESS_OUTPUT_LOG.env-before"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "enabled alerts should reject unsupported secret interpolation"
else
    pass "enabled alerts validate their secret syntax"
fi
assert_contains "enabled alert interpolation is diagnosed" "$HARNESS_OUTPUT_LOG" "GRAFANA_ADMIN_PASSWORD in $STAGE/.env uses unsupported Compose dotenv syntax"
assert_not_contains "invalid alert interpolation makes no Docker calls" "$HARNESS_CALL_LOG" "docker "
if cmp -s "$HARNESS_OUTPUT_LOG.env-before" "$STAGE/.env"; then
    pass "invalid alert interpolation leaves the environment unchanged"
else
    fail "invalid alert interpolation should not rewrite the environment"
fi

make_stage empty-commented-secret
printf 'DB_PASSWORD="" # valid Compose comment\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "an empty quoted credential with a comment should fail"
else
    pass "empty commented credential is validated as Compose consumes it"
fi
assert_contains "empty commented credential is diagnosed" "$HARNESS_OUTPUT_LOG" "Missing or empty required key in environment file: DB_PASSWORD"
assert_not_contains "empty commented credential prevents image preparation" "$HARNESS_CALL_LOG" " pull"

make_stage short-commented-secret
printf 'AUTH_CLIENT_SECRET_KEY="short" # this padding must not count\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "a short quoted auth secret with a comment should fail"
else
    pass "comment text cannot satisfy auth secret length validation"
fi
assert_contains "short commented auth secret is diagnosed" "$HARNESS_OUTPUT_LOG" "AUTH_CLIENT_SECRET_KEY in $STAGE/.env must be at least 32 characters"
assert_not_contains "short commented auth secret prevents image preparation" "$HARNESS_CALL_LOG" " pull"

make_stage no-space-colon-secret
printf '%s\n' \
    'AUTH_CLIENT_SECRET_KEY=01234567890123456789012345678901' \
    'AUTH_CLIENT_SECRET_KEY:short' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "a no-space colon must participate in Compose last-value precedence"
else
    pass "no-space colon syntax cannot hide a short effective auth secret"
fi
assert_contains "no-space colon supplies the Compose-effective short secret" "$HARNESS_OUTPUT_LOG" \
    "AUTH_CLIENT_SECRET_KEY in $STAGE/.env must be at least 32 characters"
assert_not_contains "no-space colon bypass prevents image preparation" "$HARNESS_CALL_LOG" " pull"

make_stage empty-process-secret
if DB_PASSWORD= run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "an empty process credential override should fail"
else
    pass "process credential precedence matches Compose and fails closed"
fi
assert_contains "empty process credential is validated as effective" "$HARNESS_OUTPUT_LOG" "Missing or empty required key in environment file: DB_PASSWORD"
assert_not_contains "empty process credential prevents image preparation" "$HARNESS_CALL_LOG" " pull"

make_stage project-volume
printf 'COMPOSE_PROJECT_NAME=fleet-blue\n' >> "$STAGE/.env"
env_without_password="$STAGE/.env.without-password"
grep -Ev '^DB_PASSWORD=' "$STAGE/.env" > "$env_without_password"
mv "$env_without_password" "$STAGE/.env"
if printf 'n\n' | FAKE_DOCKER_VOLUME=fleet-blue_timescaledb-data run_stage "$STAGE"; then
    fail "declining removal of an overridden project volume should abort"
else
    pass "volume guard uses the persisted Compose project name"
fi
assert_contains "volume guard finds the persisted project volume" "$HARNESS_OUTPUT_LOG" "Detected existing TimescaleDB data volume: fleet-blue_timescaledb-data"

make_stage invalid-persisted-project
printf 'COMPOSE_PROJECT_NAME=Fleet Blue\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "invalid persisted Compose project name should fail"
else
    pass "invalid persisted Compose project name fails before Docker activity"
fi
assert_contains "invalid persisted project name is diagnosed" "$HARNESS_OUTPUT_LOG" "COMPOSE_PROJECT_NAME must start with a lowercase letter or digit"
assert_not_contains "invalid persisted project name cannot target Docker" "$HARNESS_CALL_LOG" "docker "

make_stage invalid-project-override
if COMPOSE_PROJECT_NAME='Fleet Blue' run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "invalid Compose project override should fail"
else
    pass "invalid Compose project override fails before Docker activity"
fi
assert_contains "invalid project override is diagnosed" "$HARNESS_OUTPUT_LOG" "COMPOSE_PROJECT_NAME must start with a lowercase letter or digit"
assert_not_contains "invalid project override cannot target Docker" "$HARNESS_CALL_LOG" "docker "

# Every failure in the .env replacement transaction must leave secrets and
# metadata untouched, clean up the partial file, and abort before Compose
# validation or any image/service mutation.
for rewrite_failure in metadata mktemp filter chown chmod mv; do
    make_stage "env-rewrite-$rewrite_failure"
    env_before="$HARNESS_BIN_DIR/../env.before"
    cp -p "$STAGE/.env" "$env_before"
    metadata_available=true
    if ! owner_before=$(file_owner_group "$STAGE/.env"); then
        fail "$rewrite_failure fixture could not read .env owner/group"
        metadata_available=false
    fi
    if ! mode_before=$(file_mode "$STAGE/.env"); then
        fail "$rewrite_failure fixture could not read .env mode"
        metadata_available=false
    fi
    case "$rewrite_failure" in
        metadata) expected_rewrite_error="could not read owner/group metadata" ;;
        mktemp) expected_rewrite_error="could not create a temporary environment file" ;;
        filter) expected_rewrite_error="could not build a replacement" ;;
        chown) expected_rewrite_error="could not preserve owner/group" ;;
        chmod) expected_rewrite_error="could not restrict permissions" ;;
        mv) expected_rewrite_error="could not atomically replace" ;;
    esac

    if FAKE_ENV_REWRITE_FAILURE="$rewrite_failure" run_stage "$STAGE" --non-interactive --preflight-only; then
        fail "$rewrite_failure failure should abort the environment rewrite"
    else
        pass "$rewrite_failure failure aborts the environment rewrite"
    fi
    cmp -s "$env_before" "$STAGE/.env" || fail "$rewrite_failure failure changed .env contents"
    if ! owner_after=$(file_owner_group "$STAGE/.env"); then
        fail "$rewrite_failure result could not read .env owner/group"
        metadata_available=false
    fi
    if ! mode_after=$(file_mode "$STAGE/.env"); then
        fail "$rewrite_failure result could not read .env mode"
        metadata_available=false
    fi
    if [ "$metadata_available" = "true" ]; then
        if [ "$owner_after" = "$owner_before" ] && [ "$mode_after" = "$mode_before" ]; then
            pass "$rewrite_failure failure preserves .env metadata"
        else
            fail "$rewrite_failure failure changed .env metadata"
        fi
    fi
    assert_contains "$rewrite_failure failure reaches the intended operation" "$HARNESS_OUTPUT_LOG" "$expected_rewrite_error"
    assert_contains "$rewrite_failure failure is diagnosed" "$HARNESS_OUTPUT_LOG" "could not persist deployment overlay settings"
    assert_not_contains "$rewrite_failure failure prevents Compose validation" "$HARNESS_CALL_LOG" " config --quiet"
    assert_not_contains "$rewrite_failure failure prevents pulls" "$HARNESS_CALL_LOG" " pull"
    assert_not_contains "$rewrite_failure failure prevents builds" "$HARNESS_CALL_LOG" " build --no-cache"
    assert_not_contains "$rewrite_failure failure prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
    assert_not_contains "$rewrite_failure failure prevents startup" "$HARNESS_CALL_LOG" " up --remove-orphans"
    if compgen -G "$STAGE/.env.tmp.*" >/dev/null; then
        fail "$rewrite_failure failure left an environment rewrite temporary file"
    else
        pass "$rewrite_failure failure cleans its environment rewrite temporary file"
    fi
    [ ! -e "$STAGE/.update-preflight-complete" ] || fail "$rewrite_failure failure must not create a preflight marker"
done

# A successful preflight validates and prepares images, but never stops or
# starts the active stack. It records a same-directory activation marker.
make_stage preflight-parent/deployment
printf 'NO_TRAILING_NEWLINE=preserved' >> "$STAGE/.env"
if ! preflight_env_owner=$(file_owner_group "$STAGE/.env"); then
    fail "preflight fixture could not read .env owner/group"
    preflight_env_owner=""
fi
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "valid non-interactive preflight succeeds"
else
    fail "valid non-interactive preflight should succeed"
fi
assert_contains "preflight validates Compose" "$HARNESS_CALL_LOG" "config --quiet"
assert_contains "preflight pins the Compose project identity" "$HARNESS_CALL_LOG" "compose --project-name deployment"
assert_contains "preflight pulls images" "$HARNESS_CALL_LOG" " pull"
assert_contains "preflight builds images" "$HARNESS_CALL_LOG" " build --no-cache"
assert_contains "preflight verifies the release API image" "$HARNESS_CALL_LOG" "image inspect --format {{.Id}} $FLEET_API_IMAGE"
assert_contains "preflight verifies the release client image" "$HARNESS_CALL_LOG" "image inspect --format {{.Id}} $FLEET_CLIENT_IMAGE"
assert_contains "preflight verifies the release database image" "$HARNESS_CALL_LOG" "image inspect --format {{.Id}} $TIMESCALEDB_IMAGE"
assert_not_contains "preflight never targets shared latest image tags" "$HARNESS_CALL_LOG" ":latest"
assert_not_contains "preflight leaves services running" "$HARNESS_CALL_LOG" " down --remove-orphans"
assert_not_contains "preflight never starts replacement services" "$HARNESS_CALL_LOG" " up --remove-orphans"
[ -f "$STAGE/.update-preflight-complete" ] || fail "preflight should create its activation marker"
if grep -Eq '^proto-fleet-preflight-v2:[0-9a-f]{64}:[0-9a-f]{64}$' "$STAGE/.update-preflight-complete"; then
    pass "preflight marker records versioned release metadata"
else
    fail "preflight marker should contain versioned release metadata"
fi
marker_mode=$(stat -c '%a' "$STAGE/.update-preflight-complete" 2>/dev/null || stat -f '%Lp' "$STAGE/.update-preflight-complete")
if [ "$marker_mode" = "600" ]; then
    pass "preflight marker is readable only by its owner"
else
    fail "preflight marker mode should be 600, got $marker_mode"
fi
for persisted_boolean in ENABLE_BETA_ALERTS ENABLE_SYSTEM_MONITORING ENABLE_TRACING ENABLE_ONE_CLICK_UPDATES SESSION_COOKIE_SECURE; do
    if [ "$(grep -c "^${persisted_boolean}=" "$STAGE/.env")" -eq 1 ] && \
        grep -q "^${persisted_boolean}=false$" "$STAGE/.env"; then
        pass "$persisted_boolean uses one normalized last-value assignment"
    else
        fail "$persisted_boolean did not honor last-value-wins"
    fi
done
for preserved_secret in \
    'DB_PASSWORD=test-password' \
    'AUTH_CLIENT_SECRET_KEY=01234567890123456789012345678901' \
    'ENCRYPT_SERVICE_MASTER_KEY=MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE='; do
    if [ "$(grep -cFx "$preserved_secret" "$STAGE/.env")" -eq 1 ]; then
        pass "environment rewrite preserves ${preserved_secret%%=*}"
    else
        fail "environment rewrite lost ${preserved_secret%%=*}"
    fi
done
if grep -qFx 'NO_TRAILING_NEWLINE=preserved' "$STAGE/.env"; then
    pass "environment rewrite handles a source without a trailing newline"
else
    fail "environment rewrite corrupted the source's final line"
fi
if preflight_env_mode=$(file_mode "$STAGE/.env") && [ "$preflight_env_mode" = "600" ]; then
    pass "environment rewrite restricts .env permissions to 600"
else
    fail "environment rewrite did not restrict .env permissions"
fi
if preflight_env_owner_after=$(file_owner_group "$STAGE/.env") && \
    [ -n "$preflight_env_owner" ] && [ "$preflight_env_owner_after" = "$preflight_env_owner" ]; then
    pass "environment rewrite preserves .env owner and group"
else
    fail "environment rewrite changed .env owner or group"
fi

# Activation consumes the marker, reuses prepared images, and performs the
# service transition without another pull or build.
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --skip-build; then
    pass "prepared activation succeeds"
else
    fail "prepared activation should succeed"
fi
assert_not_contains "activation skips pulls" "$HARNESS_CALL_LOG" " pull"
assert_not_contains "activation skips builds" "$HARNESS_CALL_LOG" " build --no-cache"
assert_contains "activation forbids implicit image preparation" "$HARNESS_CALL_LOG" "up --remove-orphans -d --wait --wait-timeout 300 --no-build --pull never"
assert_not_contains "activation never resolves shared latest image tags" "$HARNESS_CALL_LOG" ":latest"
assert_contains "activation stops the old stack" "$HARNESS_CALL_LOG" " down --remove-orphans"
assert_contains "activation starts the new stack" "$HARNESS_CALL_LOG" " up --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "successful activation should consume its marker"

# Preflight must never delete an installed release merely because its stack is
# stopped. Garbage collection runs only after successful activation, when the
# old deployment is no longer the recovery path.
make_stage release-image-retention/deployment
gc_images=$(printf '%s\n' \
    'proto-fleet-api:latest' \
    'proto-fleet-api:debug' \
    "proto-fleet-api:$RELEASE_TAG" \
    'proto-fleet-timescaledb-ha:v1.2.3' \
    'proto-fleet-api:v1.0.0' \
    'proto-fleet-client:v1.0.0' \
    'proto-fleet-timescaledb:v1.0.0' \
    'proto-fleet-timescaledb-ha:v1.0.0' \
    'proto-fleet-api:v0.9.0' \
    'proto-fleet-client:v0.9.0' \
    'proto-fleet-timescaledb:v0.9.0' \
    'proto-fleet-timescaledb-ha:v0.9.0' \
    'proto-fleet-api:v1.1.0' \
    'proto-fleet-client:v1.1.0' \
    'proto-fleet-timescaledb:v1.1.0' \
    'proto-fleet-timescaledb-ha:v1.1.0' \
    'proto-fleet-api:nightly-20260731-abcdef123456')
if FAKE_RELEASE_IMAGES="$gc_images" run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "preflight preserves installed and abandoned release tags"
else
    fail "release image retention fixture should preflight"
fi
assert_not_contains "preflight never garbage-collects release images" "$HARNESS_CALL_LOG" "docker image rm "

# After activation, remove only unused obsolete Proto Fleet release tags. Keep
# source :latest tags, the target release, local/debug tags, and all four
# images for a release if any running or stopped container still uses it.
: > "$HARNESS_CALL_LOG"
if FAKE_RELEASE_IMAGES="$gc_images" \
    FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    FAKE_CONTAINER_IMAGE_REFS='proto-fleet-api:v0.9.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    pass "successful activation prunes only unused obsolete release tags"
else
    fail "post-activation release image cleanup should be nonfatal"
fi
for obsolete_image in \
    proto-fleet-api:v1.1.0 \
    proto-fleet-client:v1.1.0 \
    proto-fleet-timescaledb:v1.1.0 \
    proto-fleet-timescaledb-ha:v1.1.0 \
    proto-fleet-api:nightly-20260731-abcdef123456; do
    assert_contains "activation removes obsolete image $obsolete_image" "$HARNESS_CALL_LOG" "docker image rm $obsolete_image"
done
for retained_image in \
    proto-fleet-api:latest \
    proto-fleet-api:debug \
    "proto-fleet-api:$RELEASE_TAG" \
    proto-fleet-timescaledb-ha:v1.2.3 \
    proto-fleet-api:v1.0.0 \
    proto-fleet-client:v1.0.0 \
    proto-fleet-timescaledb:v1.0.0 \
    proto-fleet-timescaledb-ha:v1.0.0 \
    proto-fleet-api:v0.9.0 \
    proto-fleet-client:v0.9.0 \
    proto-fleet-timescaledb:v0.9.0 \
    proto-fleet-timescaledb-ha:v0.9.0; do
    assert_not_contains "activation retains image $retained_image" "$HARNESS_CALL_LOG" "docker image rm $retained_image"
done
assert_contains "retention checks stopped containers by immutable image ID" "$HARNESS_CALL_LOG" \
    "docker container ls --all --quiet --filter ancestor=sha256:test-proto-fleet-api:v0.9.0"
assert_contains "activation records the previous release before teardown" "$HARNESS_CALL_LOG" \
    "docker container ls --all --filter label=com.docker.compose.project=deployment --format {{.Image}}"
assert_not_contains "activation retains its current API image" "$HARNESS_CALL_LOG" "docker image rm $FLEET_API_IMAGE"
up_line=$(grep -nF ' up --remove-orphans' "$HARNESS_CALL_LOG" | head -1 | cut -d: -f1)
gc_line=$(grep -nF 'docker image rm proto-fleet-api:v1.1.0' "$HARNESS_CALL_LOG" | head -1 | cut -d: -f1)
final_ready_line=$(grep -nF 'curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:4000/health/ready' "$HARNESS_CALL_LOG" | tail -1 | cut -d: -f1)
client_ready_line=$(grep -nF 'curl -fsS -o /dev/null --max-time 2 http://127.0.0.1/' "$HARNESS_CALL_LOG" | tail -1 | cut -d: -f1)
ready_probe_count=$(grep -cF 'curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:4000/health/ready' "$HARNESS_CALL_LOG")
if [ -n "$up_line" ] && [ -n "$final_ready_line" ] && [ -n "$client_ready_line" ] && [ -n "$gc_line" ] \
    && [ "$up_line" -lt "$final_ready_line" ] && [ "$final_ready_line" -lt "$client_ready_line" ] \
    && [ "$client_ready_line" -lt "$gc_line" ] \
    && [ "$ready_probe_count" -ge 2 ]; then
    pass "obsolete release cleanup runs only after final API and client readiness"
else
    fail "obsolete release cleanup should follow the final API and client readiness probes"
fi

# A retry can find target-release containers plus an unrelated stale project
# container after the first activation attempt. Neither identifies the actual
# previous known-good tag, so leave every managed image available for recovery.
make_stage retry-release-discovery/deployment
if FAKE_RELEASE_IMAGES='proto-fleet-api:v1.1.0 proto-fleet-api:v1.0.0' \
    FAKE_ACTIVE_RELEASE_IMAGES="$FLEET_API_IMAGE proto-fleet-api:v0.9.0" \
    run_stage "$STAGE" --non-interactive; then
    pass "retry release discovery skips ambiguous cleanup"
else
    fail "retry release discovery should remain a successful activation"
fi
assert_contains "retry discovery is diagnosed" "$HARNESS_OUTPUT_LOG" "the target Proto Fleet release is already active"
assert_not_contains "retry discovery preserves immediate recovery images" "$HARNESS_CALL_LOG" "docker image rm proto-fleet-api:v1.1.0"
assert_not_contains "retry discovery preserves unidentifiable recovery images" "$HARNESS_CALL_LOG" "docker image rm proto-fleet-api:v1.0.0"

# Docker inspection/removal errors are fail-safe housekeeping failures: no
# uncertain tag is deleted, and a successful deployment remains successful.
make_stage release-image-list-failure/deployment
if FAKE_RELEASE_IMAGES='proto-fleet-api:v1.1.0' \
    FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    FAKE_IMAGE_LIST_FAILURE=proto-fleet-client \
    run_stage "$STAGE" --non-interactive; then
    pass "image-list failure does not fail deployment"
else
    fail "image-list failure should skip cleanup without failing deployment"
fi
assert_contains "image-list failure is diagnosed" "$HARNESS_OUTPUT_LOG" "skipping Proto Fleet release image cleanup"
assert_not_contains "image-list failure prevents partial deletion" "$HARNESS_CALL_LOG" "docker image rm proto-fleet-api:v1.1.0"

make_stage release-image-remove-failure/deployment
if FAKE_RELEASE_IMAGES='proto-fleet-api:v1.1.0' \
    FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    FAKE_IMAGE_RM_FAILURE=proto-fleet-api:v1.1.0 \
    run_stage "$STAGE" --non-interactive; then
    pass "image removal failure does not fail deployment"
else
    fail "image removal failure should remain best-effort"
fi
assert_contains "image removal failure is diagnosed" "$HARNESS_OUTPUT_LOG" "could not remove obsolete Proto Fleet image proto-fleet-api:v1.1.0"

make_stage release-image-active-query-failure/deployment
if FAKE_RELEASE_IMAGES='proto-fleet-api:v1.1.0' \
    FAKE_ACTIVE_IMAGE_LIST_FAILURE=true \
    run_stage "$STAGE" --non-interactive; then
    pass "active-release discovery failure does not fail deployment"
else
    fail "active-release discovery failure should only skip cleanup"
fi
assert_contains "active-release discovery failure is diagnosed" "$HARNESS_OUTPUT_LOG" "could not identify the active Proto Fleet release"
assert_not_contains "unknown previous-release state prevents release deletion" "$HARNESS_CALL_LOG" "docker image rm proto-fleet-api:v1.1.0"

# A container being merely "running" is not sufficient after forward-only
# migrations. Require fleet-api's DB-backed readiness endpoint before any
# cleanup or success marker consumption.
make_stage api-readiness-failure/deployment
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "API readiness failure fixture should preflight"
fi
: > "$HARNESS_CALL_LOG"
if FLEET_API_READY_ATTEMPTS=1 \
    FAKE_API_READINESS_FAILURE=true \
    FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    fail "activation should fail when fleet-api never becomes ready"
else
    pass "fleet-api readiness failure blocks upgrade completion"
fi
[ -f "$STAGE/.update-preflight-complete" ] || fail "readiness failure should retain its recovery marker"
assert_contains "activation probes the DB-backed readiness endpoint" "$HARNESS_CALL_LOG" "curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:4000/health/ready"
assert_not_contains "readiness failure preserves previous release images" "$HARNESS_CALL_LOG" "docker image rm "
assert_not_contains "readiness failure never reports success" "$HARNESS_OUTPUT_LOG" "Proto Fleet is now running!"

make_stage ignored-api-listen-override/deployment
printf 'HTTP_LISTEN_ADDRESS=:80\n' >> "$STAGE/.env"
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "ignored API listen override fixture should preflight"
fi
: > "$HARNESS_CALL_LOG"
if FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    pass "API readiness uses the deployment service port"
else
    fail "an unrelated dotenv listen address should not redirect API readiness"
fi
assert_contains "API readiness probes Compose's fixed fleet-api port" "$HARNESS_CALL_LOG" \
    "curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:4000/health/ready"
assert_not_contains "nginx cannot masquerade as API readiness" "$HARNESS_CALL_LOG" \
    "http://127.0.0.1:80/health/ready"

make_stage client-readiness-failure/deployment
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "client readiness failure fixture should preflight"
fi
: > "$HARNESS_CALL_LOG"
if FLEET_API_READY_ATTEMPTS=1 \
    FAKE_CLIENT_READINESS_FAILURE=true \
    FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    fail "activation should fail when fleet-client never becomes ready"
else
    pass "fleet-client readiness failure blocks upgrade completion"
fi
[ -f "$STAGE/.update-preflight-complete" ] || fail "client readiness failure should retain its recovery marker"
assert_contains "activation probes the fleet-client endpoint" "$HARNESS_CALL_LOG" "curl -fsS -o /dev/null --max-time 2 http://127.0.0.1/"
assert_not_contains "client readiness failure preserves previous release images" "$HARNESS_CALL_LOG" "docker image rm "
assert_not_contains "client readiness failure never reports success" "$HARNESS_OUTPUT_LOG" "Proto Fleet is now running!"

# Alerts-enabled activation exercises the DB-object readiness poll. Keep its
# attempt budget tied to the validated service-readiness setting.
make_stage alerts-activation/deployment
enable_valid_alerts "$STAGE/.env"
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "alerts activation fixture should preflight"
fi
: > "$HARNESS_CALL_LOG"
if FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    pass "alerts-enabled activation completes its DB readiness poll"
else
    fail "alerts-enabled activation should not lose its readiness attempt budget"
fi
assert_contains "alerts activation provisions the Grafana DB role" "$HARNESS_OUTPUT_LOG" "Provisioning Grafana read-only DB role"

# The updater preflights and activates directories that are both named
# `deployment`; only their parent changes. Absolute paths in rendered Compose
# output are normalized so that this safe relocation does not invalidate the
# proof, while Compose's native project identity stays stable.
make_stage relocated-preflight/deployment
printf 'COMPOSE_PROJECT_NAME=fleet-blue\n' >> "$STAGE/.env"
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "relocation fixture preflight should succeed"
fi
assert_contains "relocated preflight uses the persisted project" "$HARNESS_CALL_LOG" "compose --project-name fleet-blue"
relocated_stage="$TMP_DIR/activated-release/deployment"
mkdir -p "$(dirname "$relocated_stage")"
mv "$STAGE" "$relocated_stage"
STAGE="$relocated_stage"
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --skip-build; then
    pass "preflight proof survives the updater's directory rename"
else
    fail "unchanged relocated release should activate"
fi
assert_contains "relocated activation stops the old stack" "$HARNESS_CALL_LOG" " down --remove-orphans"
assert_contains "relocated activation keeps the persisted project" "$HARNESS_CALL_LOG" "compose --project-name fleet-blue"

# Every input that defines the prepared release is bound into the marker. A
# changed or forged marker fails before the active deployment is stopped and
# is invalidated so recovery cannot keep retrying stale preparation.
for mutation in env compose version nginx runner runtime tls timescaledb; do
    make_stage "changed-$mutation"
    if [ "$mutation" = "tls" ]; then
        printf 'SESSION_COOKIE_SECURE=true\n' >> "$STAGE/.env"
        mkdir -p "$STAGE/ssl"
        printf 'test certificate\n' > "$STAGE/ssl/cert.pem"
        printf 'test private key\n' > "$STAGE/ssl/key.pem"
    fi
    if ! run_stage "$STAGE" --non-interactive --preflight-only; then
        fail "$mutation mutation fixture preflight should succeed"
        continue
    fi
    case "$mutation" in
        env) printf 'DB_PASSWORD=changed-after-preflight\n' >> "$STAGE/.env" ;;
        compose) printf '\n# changed after preflight\n' >> "$STAGE/docker-compose.yaml" ;;
        version) printf 'commit: changed-after-preflight\n' >> "$STAGE/version.txt" ;;
        nginx) printf '\n# changed after preflight\n' >> "$STAGE/client/nginx.http.conf" ;;
        runner) printf '\n# changed after preflight\n' >> "$STAGE/run-fleet.sh" ;;
        runtime) printf '\n# changed after preflight\n' >> "$STAGE/server/otel-collector-config.datadog.yaml" ;;
        tls) printf '\nchanged after preflight\n' >> "$STAGE/ssl/cert.pem" ;;
        timescaledb) printf 'changed-after-preflight' >> "$STAGE/images/timescaledb.tar.gz" ;;
    esac
    : > "$HARNESS_CALL_LOG"
    if run_stage "$STAGE" --non-interactive --skip-build; then
        fail "$mutation changes should invalidate preflight"
    else
        pass "$mutation changes invalidate preflight"
    fi
    assert_contains "$mutation mismatch is diagnosed" "$HARNESS_OUTPUT_LOG" "changed after preflight"
    assert_not_contains "$mutation mismatch prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
    assert_not_contains "$mutation mismatch prevents startup" "$HARNESS_CALL_LOG" " up --remove-orphans"
    [ ! -e "$STAGE/.update-preflight-complete" ] || fail "$mutation mismatch should remove the stale marker"
done

make_stage missing-prepared-image
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing-image fixture preflight should succeed"
fi
: > "$HARNESS_CALL_LOG"
if FAKE_PREPARED_IMAGE_MISSING=true run_stage "$STAGE" --non-interactive --skip-build; then
    fail "missing prepared image should fail activation validation"
else
    pass "missing prepared image fails before activation"
fi
assert_contains "missing image is diagnosed" "$HARNESS_OUTPUT_LOG" "prepared image is missing before activation"
assert_not_contains "missing image prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "missing image should invalidate the marker"

make_stage changed-prepared-image
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "changed-image fixture preflight should succeed"
fi
: > "$HARNESS_CALL_LOG"
if FAKE_PREPARED_IMAGE_CHANGED=true run_stage "$STAGE" --non-interactive --skip-build; then
    fail "changed prepared image should fail activation validation"
else
    pass "changed prepared image fails before activation"
fi
assert_contains "changed image is diagnosed" "$HARNESS_OUTPUT_LOG" "changed after preflight"
assert_not_contains "changed image prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "changed image should invalidate the marker"

make_stage added-release-file
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "added-file fixture preflight should succeed"
fi
printf 'apiVersion: 1\n' > "$STAGE/server/monitoring/grafana/provisioning/datasources/added-after-preflight.yaml"
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "an added immutable release file should fail activation validation"
else
    pass "an added immutable release file fails before activation"
fi
assert_contains "added release file is diagnosed" "$HARNESS_OUTPUT_LOG" "file set does not match"
assert_not_contains "added release file prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "added release file should invalidate the marker"

# client/nginx.conf is generated runtime state, so it is intentionally absent
# from the immutable release manifest. It must still never be allowed to turn
# the privileged config copy into a write through an attacker-controlled link.
make_stage generated-nginx-symlink-preflight
nginx_victim="$TMP_DIR/generated-nginx-preflight-victim"
printf 'keep preflight victim unchanged\n' > "$nginx_victim"
ln -s "$nginx_victim" "$STAGE/client/nginx.conf"
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "a symlinked generated nginx config should fail preflight"
else
    pass "a symlinked generated nginx config fails preflight"
fi
assert_contains "preflight nginx symlink is diagnosed" "$HARNESS_OUTPUT_LOG" "must be a regular, non-symlink file"
assert_not_contains "preflight nginx symlink prevents image preparation" "$HARNESS_CALL_LOG" " build --no-cache"
assert_not_contains "preflight nginx symlink prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
if grep -Fxq 'keep preflight victim unchanged' "$nginx_victim"; then
    pass "preflight nginx symlink does not overwrite its target"
else
    fail "preflight nginx symlink overwrote its target"
fi
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "nginx symlink preflight must not create a marker"

make_stage generated-nginx-hardlink-preflight
nginx_victim="$TMP_DIR/generated-nginx-hardlink-victim"
printf 'keep hardlink victim unchanged\n' > "$nginx_victim"
if ! ln "$nginx_victim" "$STAGE/client/nginx.conf"; then
    fail "hard-linked nginx fixture could not create its link"
else
    if run_stage "$STAGE" --non-interactive --preflight-only; then
        pass "a hard-linked generated nginx config is replaced safely"
    else
        fail "a hard-linked generated nginx config should preflight"
    fi
    if grep -Fxq 'keep hardlink victim unchanged' "$nginx_victim"; then
        pass "nginx config replacement does not mutate another hard link"
    else
        fail "nginx config replacement mutated another hard link"
    fi
    if cmp -s "$STAGE/client/nginx.http.conf" "$STAGE/client/nginx.conf"; then
        pass "hard-linked nginx destination receives the selected configuration"
    else
        fail "hard-linked nginx destination was not replaced"
    fi
fi

make_stage generated-nginx-symlink-activation
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "nginx activation symlink fixture preflight should succeed"
fi
nginx_victim="$TMP_DIR/generated-nginx-activation-victim"
printf 'keep activation victim unchanged\n' > "$nginx_victim"
rm -f "$STAGE/client/nginx.conf"
ln -s "$nginx_victim" "$STAGE/client/nginx.conf"
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "a symlinked generated nginx config should fail activation"
else
    pass "a symlinked generated nginx config fails activation"
fi
assert_contains "activation nginx symlink is diagnosed" "$HARNESS_OUTPUT_LOG" "must be a regular, non-symlink file"
assert_not_contains "activation nginx symlink prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
assert_not_contains "activation nginx symlink prevents startup" "$HARNESS_CALL_LOG" " up --remove-orphans"
if grep -Fxq 'keep activation victim unchanged' "$nginx_victim"; then
    pass "activation nginx symlink does not overwrite its target"
else
    fail "activation nginx symlink overwrote its target"
fi
[ -f "$STAGE/.update-preflight-complete" ] || fail "nginx symlink activation should retain its recovery marker"

# Non-regular entries are not valid release content. In particular, Grafana
# recursively consumes this provisioning directory, so a symlink or FIFO added
# after preflight must invalidate activation just like an added regular file.
for entry_type in symlink fifo; do
    make_stage "added-release-$entry_type"
    if ! run_stage "$STAGE" --non-interactive --preflight-only; then
        fail "$entry_type fixture preflight should succeed"
        continue
    fi
    entry="$STAGE/server/monitoring/grafana/provisioning/datasources/added-after-preflight-$entry_type.yaml"
    case "$entry_type" in
        symlink) ln -s base.yaml "$entry" ;;
        fifo) mkfifo "$entry" ;;
    esac
    : > "$HARNESS_CALL_LOG"
    if run_stage "$STAGE" --non-interactive --skip-build; then
        fail "an added immutable $entry_type should fail activation validation"
    else
        pass "an added immutable $entry_type fails before activation"
    fi
    assert_contains "$entry_type addition is diagnosed" "$HARNESS_OUTPUT_LOG" "unsupported non-regular entries"
    assert_not_contains "$entry_type addition prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
    assert_not_contains "$entry_type addition prevents startup" "$HARNESS_CALL_LOG" " up --remove-orphans"
    [ ! -e "$STAGE/.update-preflight-complete" ] || fail "$entry_type addition should invalidate the marker"
done

# HA's node.env is operator-owned runtime state, not a packaged release file.
# Permit that exact path while keeping every other added HA artifact inside the
# immutable release boundary.
make_stage ha-operator-state
mkdir -p "$STAGE/ha"
printf 'HA_NODE_NAME=ha-a\n' > "$STAGE/ha/node.env"
chmod 600 "$STAGE/ha/node.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "HA node environment remains outside the immutable release set"
else
    fail "operator-owned HA node environment should not fail preflight"
fi
printf 'tampered\n' > "$STAGE/ha/added-after-preflight.yaml"
: > "$HARNESS_CALL_LOG"
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "an added HA release file should fail activation validation"
else
    pass "other added HA files remain immutable"
fi
assert_contains "added HA release file is diagnosed" "$HARNESS_OUTPUT_LOG" "file set does not match"
assert_not_contains "added HA release file prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "added HA release file should invalidate the marker"

make_stage missing-release-manifest
rm -f "$STAGE/deployment-manifest.sha256"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing release manifest should fail preflight"
else
    pass "missing release manifest fails preflight"
fi
assert_contains "missing manifest is diagnosed" "$HARNESS_OUTPUT_LOG" "requires the immutable release manifest"
assert_not_contains "missing manifest prevents pulls" "$HARNESS_CALL_LOG" " pull"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "missing manifest must not create a marker"

# Release bundles must pin literal image references before any preparation.
# This protects bare Compose and Windows installer paths that do not inherit
# state from run-fleet.sh.
make_stage unpinned-compose
if FAKE_COMPOSE_USES_LATEST=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "release Compose model with a shared latest tag should fail preflight"
else
    pass "release Compose model rejects shared latest tags"
fi
assert_contains "unpinned Compose is diagnosed" "$HARNESS_OUTPUT_LOG" "not pinned to required image $FLEET_API_IMAGE"
assert_not_contains "unpinned Compose prevents pulls" "$HARNESS_CALL_LOG" " pull"
assert_not_contains "unpinned Compose prevents archive loading" "$HARNESS_CALL_LOG" "docker load"
assert_not_contains "unpinned Compose prevents builds" "$HARNESS_CALL_LOG" " build --no-cache"

make_stage unpinned-ha-compose
sed -i.bak "s|$TIMESCALEDB_HA_IMAGE|proto-fleet-timescaledb-ha:latest|" "$STAGE/ha/compose.yaml"
rm -f "$STAGE/ha/compose.yaml.bak"
write_release_manifest "$STAGE"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "release HA Compose model with a shared latest tag should fail preflight"
else
    pass "release HA Compose model rejects the shared latest tag"
fi
assert_contains "unpinned HA Compose is diagnosed" "$HARNESS_OUTPUT_LOG" "not pinned to required image $TIMESCALEDB_HA_IMAGE"
assert_not_contains "unpinned HA Compose prevents archive loading" "$HARNESS_CALL_LOG" "docker load"

make_stage changed-during-preflight
if FAKE_MUTATE_TSDB_DURING_BUILD=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "inputs changed during preparation should fail preflight"
else
    pass "inputs changed during preparation fail preflight"
fi
assert_contains "mid-preflight mutation is diagnosed" "$HARNESS_OUTPUT_LOG" "release or configuration changed during preflight"
assert_contains "mid-preflight mutation occurs after the build starts" "$HARNESS_CALL_LOG" " build --no-cache"
assert_not_contains "failed preflight leaves shared latest tags untouched" "$HARNESS_CALL_LOG" ":latest"
assert_not_contains "mid-preflight mutation prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "mid-preflight mutation must not create a marker"

make_stage forged-marker
: > "$STAGE/.update-preflight-complete"
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "an empty forged marker should not authorize activation"
else
    pass "an empty forged marker is rejected"
fi
assert_not_contains "forged marker prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "forged marker should be invalidated"

# A failed activation retains the marker so the recovery command can retry the
# prepared release without an unsafe rebuild.
make_stage failed-activation
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "failed-activation fixture preflight should succeed"
fi
: > "$HARNESS_CALL_LOG"
if FAKE_ACTIVATION_FAILURE=true \
    FAKE_RELEASE_IMAGES='proto-fleet-api:v1.1.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    fail "simulated activation failure should propagate"
else
    pass "activation failure propagates"
fi
[ -f "$STAGE/.update-preflight-complete" ] || fail "failed activation should retain its recovery marker"
assert_contains "activation recovery command keeps the resolved project" "$HARNESS_OUTPUT_LOG" "docker compose --project-name failed-activation"
assert_not_contains "failed activation cannot prune prepared or retired releases" "$HARNESS_CALL_LOG" "docker image rm proto-fleet-api:v1.1.0"

# Skip-build must not be usable without proof that this exact deployment
# directory completed preflight.
make_stage missing-marker
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "skip-build should fail without a preflight marker"
else
    pass "skip-build rejects an unprepared deployment"
fi
assert_not_contains "unprepared activation makes no Docker calls" "$HARNESS_CALL_LOG" "docker "

# Invalid persisted booleans fail before even read-only Docker probes.
make_stage invalid-boolean
printf 'ENABLE_TRACING=maybe\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "invalid persisted boolean should fail preflight"
else
    pass "invalid persisted boolean fails preflight"
fi
assert_not_contains "invalid boolean makes no Docker calls" "$HARNESS_CALL_LOG" "docker "

# Invalid persisted configuration must fail before mutation or image/service
# operations, so activation cannot discover it only after teardown.
make_stage invalid-env
sed -i.bak 's/^DB_PASSWORD=.*/DB_PASSWORD=/' "$STAGE/.env"
rm -f "$STAGE/.env.bak"
cp "$STAGE/.env" "$STAGE/env.before"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "empty required settings should fail preflight"
else
    pass "empty required settings fail preflight"
fi
cmp -s "$STAGE/env.before" "$STAGE/.env" || fail "invalid preflight should not rewrite .env"
assert_not_contains "invalid settings prevent pulls" "$HARNESS_CALL_LOG" " pull"
assert_not_contains "invalid settings prevent teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"

make_stage invalid-secrets
sed -i.bak \
    -e 's/^AUTH_CLIENT_SECRET_KEY=.*/AUTH_CLIENT_SECRET_KEY=too-short/' \
    -e 's|^ENCRYPT_SERVICE_MASTER_KEY=.*|ENCRYPT_SERVICE_MASTER_KEY=not-base64|' \
    "$STAGE/.env"
rm -f "$STAGE/.env.bak"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "malformed secrets should fail preflight"
else
    pass "malformed secrets fail preflight"
fi
assert_contains "short auth secret is diagnosed" "$HARNESS_OUTPUT_LOG" "must be at least 32 characters"
assert_contains "invalid encryption key is diagnosed" "$HARNESS_OUTPUT_LOG" "must decode to exactly 32 bytes"
assert_not_contains "malformed secrets prevent pulls" "$HARNESS_CALL_LOG" " pull"

# Existing alert deployments must retain their secrets; an unattended run
# fails instead of generating replacements for incomplete persisted state.
make_stage invalid-alerts
printf '%s\n' \
    'ENABLE_BETA_ALERTS=true' \
    'GRAFANA_ADMIN_PASSWORD=admin-secret' \
    'GRAFANA_DB_USERNAME=grafana_ro' \
    'GRAFANA_DB_PASSWORD=db-secret' \
    'FLEET_ALERTS_WEBHOOK_TOKEN=webhook-secret' \
    'GRAFANA_SECRET_KEY=grafana-secret' >> "$STAGE/.env"
cp "$STAGE/.env" "$STAGE/env.before"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing alert secrets should fail preflight"
else
    pass "missing alert secrets fail preflight"
fi
cmp -s "$STAGE/env.before" "$STAGE/.env" || fail "alert validation should not rewrite .env"
assert_contains "missing alert service token is diagnosed" "$HARNESS_OUTPUT_LOG" "FLEET_ALERTS_GRAFANA_TOKEN"
assert_not_contains "missing alert secrets prevent pulls" "$HARNESS_CALL_LOG" " pull"

# Grafana role names are deterministic local validation, so reject known-bad
# SQL identifiers and privileged/conflicting role names during preflight rather
# than after the running stack has been replaced.
make_stage valid-alert-role
enable_valid_alerts "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "valid dedicated Grafana role passes preflight"
else
    fail "valid dedicated Grafana role should pass preflight"
fi

for role_case in invalid-grafana-identifier invalid-db-identifier postgres-role app-role reserved-pg-role; do
    make_stage "$role_case"
    enable_valid_alerts "$STAGE/.env"
    case "$role_case" in
        invalid-grafana-identifier)
            printf 'GRAFANA_DB_USERNAME=grafana-ro\n' >> "$STAGE/.env"
            expected_error='GRAFANA_DB_USERNAME must be a valid SQL identifier'
            ;;
        invalid-db-identifier)
            printf 'DB_NAME=fleet-prod\n' >> "$STAGE/.env"
            expected_error='DB_NAME must be a valid SQL identifier'
            ;;
        postgres-role)
            printf 'GRAFANA_DB_USERNAME=postgres\n' >> "$STAGE/.env"
            expected_error='must not match the application DB role'
            ;;
        app-role)
            printf 'GRAFANA_DB_USERNAME=fleet\n' >> "$STAGE/.env"
            expected_error='must not match the application DB role'
            ;;
        reserved-pg-role)
            printf 'GRAFANA_DB_USERNAME=pg_custom\n' >> "$STAGE/.env"
            expected_error="must not use PostgreSQL's reserved pg_ role prefix"
            ;;
    esac
    cp "$STAGE/.env" "$STAGE/env.before"
    if run_stage "$STAGE" --non-interactive --preflight-only; then
        fail "$role_case should fail preflight"
    else
        pass "$role_case fails preflight"
    fi
    cmp -s "$STAGE/env.before" "$STAGE/.env" || fail "$role_case validation should not rewrite .env"
    assert_contains "$role_case is diagnosed" "$HARNESS_OUTPUT_LOG" "$expected_error"
    assert_not_contains "$role_case prevents pulls" "$HARNESS_CALL_LOG" " pull"
    assert_not_contains "$role_case prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
    [ ! -e "$STAGE/.update-preflight-complete" ] || fail "$role_case must not create a preflight marker"
done

# Compose interpolation/render errors are also caught before image work or
# service teardown.
make_stage invalid-compose
: > "$STAGE/.update-preflight-complete"
if FAKE_COMPOSE_CONFIG_FAILURE=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "invalid Compose configuration should fail preflight"
else
    pass "invalid Compose configuration fails preflight"
fi
assert_not_contains "invalid Compose prevents pulls" "$HARNESS_CALL_LOG" " pull"
assert_not_contains "invalid Compose prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "invalid Compose must not leave a preflight marker"

# A non-removable stale marker fails closed before Docker activity.
make_stage invalid-marker
mkdir "$STAGE/.update-preflight-complete"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "non-removable preflight marker should fail"
else
    pass "non-removable preflight marker fails closed"
fi
assert_not_contains "marker cleanup failure makes no Docker calls" "$HARNESS_CALL_LOG" "docker "

# Missing packaged and local database images must fail before a preflight can
# authorize service activation.
make_stage missing-tsdb-image
rm -f "$STAGE/images/timescaledb.tar.gz"
write_release_manifest "$STAGE"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing TimescaleDB image should fail preflight"
else
    pass "missing TimescaleDB image fails preflight"
fi
assert_not_contains "missing database image prevents builds" "$HARNESS_CALL_LOG" " build --no-cache"
assert_not_contains "missing database image prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "missing database image must not leave a preflight marker"

# Compose skips pull_policy: never services even when their local image is
# absent. A clean host therefore pulls external dependencies first, then loads
# the packaged TimescaleDB archive and completes preflight.
make_stage cold-tsdb-cache
if FAKE_TSDB_IMAGE_COLD_CACHE=true run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "a cold TimescaleDB image cache preflights from the packaged archive"
else
    fail "compose pull should skip an uncached pull_policy-never TimescaleDB image"
fi
assert_contains "cold cache still pulls external dependencies" "$HARNESS_CALL_LOG" " pull --ignore-buildable"
assert_contains "cold cache loads the packaged database image" "$HARNESS_CALL_LOG" "docker load"
pull_line=$(grep -nF ' pull --ignore-buildable' "$HARNESS_CALL_LOG" | head -1 | cut -d: -f1)
load_line=$(grep -nF 'docker load' "$HARNESS_CALL_LOG" | head -1 | cut -d: -f1)
if [ -n "$pull_line" ] && [ -n "$load_line" ] && [ "$pull_line" -lt "$load_line" ]; then
    pass "external pull fails before any packaged database retagging"
else
    fail "external images should be pulled before loading the packaged database image"
fi

make_stage mistagged-tsdb-image
write_tsdb_archive "$STAGE" "\"proto-fleet-timescaledb:latest\",\"$TIMESCALEDB_HA_IMAGE\""
write_release_manifest "$STAGE"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "mistagged TimescaleDB archive should fail preflight"
else
    pass "mistagged TimescaleDB archive fails preflight"
fi
assert_not_contains "mistagged database image is not loaded" "$HARNESS_CALL_LOG" "docker load"
assert_not_contains "mistagged database image prevents builds" "$HARNESS_CALL_LOG" " build --no-cache"

# Requiring only the standard release tag would still allow docker load to
# move the daemon-global HA latest tag.
make_stage mixed-ha-tags
write_tsdb_archive "$STAGE" "\"$TIMESCALEDB_IMAGE\",\"$TIMESCALEDB_HA_IMAGE\",\"proto-fleet-timescaledb-ha:latest\""
write_release_manifest "$STAGE"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "archive with a shared HA latest tag should fail preflight"
else
    pass "archive with a shared HA latest tag fails preflight"
fi
assert_contains "shared HA archive tag is diagnosed" "$HARNESS_OUTPUT_LOG" "contains forbidden shared image proto-fleet-timescaledb-ha:latest"
assert_not_contains "shared HA archive tag is not loaded" "$HARNESS_CALL_LOG" "docker load"
assert_not_contains "shared HA archive tag prevents builds" "$HARNESS_CALL_LOG" " build --no-cache"

make_stage unloaded-tsdb-image
if FAKE_TSDB_IMAGE_MISSING=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "archive that does not load the expected tag should fail preflight"
else
    pass "loaded archive must expose the expected TimescaleDB tag"
fi
assert_not_contains "unloaded database image prevents builds" "$HARNESS_CALL_LOG" " build --no-cache"
assert_not_contains "unloaded database image prevents teardown" "$HARNESS_CALL_LOG" " down --remove-orphans"

# Legacy HTTPS deployments predate SESSION_COOKIE_SECURE persistence. A full
# certificate pair remains authoritative only when the key is absent; this
# avoids silently downgrading those installs while explicit false still wins.
make_stage legacy-https
legacy_env="$STAGE/.env.without-cookie-mode"
grep -Ev '^SESSION_COOKIE_SECURE[[:space:]]*=' "$STAGE/.env" > "$legacy_env"
mv "$legacy_env" "$STAGE/.env"
mkdir -p "$STAGE/ssl"
: > "$STAGE/ssl/cert.pem"
: > "$STAGE/ssl/key.pem"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "legacy HTTPS without a persisted cookie mode preflights"
else
    fail "legacy HTTPS should be inferred from its complete certificate pair"
fi
assert_contains "legacy HTTPS transport is preserved" "$HARNESS_OUTPUT_LOG" "Preserving legacy HTTPS mode"
if cmp -s "$STAGE/client/nginx.https.conf" "$STAGE/client/nginx.conf" && grep -q '^SESSION_COOKIE_SECURE=true$' "$STAGE/.env"; then
    pass "legacy HTTPS is persisted explicitly"
else
    fail "legacy HTTPS did not select HTTPS config and persist secure cookies"
fi
: > "$HARNESS_CALL_LOG"
if FAKE_ACTIVE_RELEASE_IMAGES='proto-fleet-api:v1.0.0' \
    run_stage "$STAGE" --non-interactive --skip-build; then
    pass "HTTPS activation probes the served frontend"
else
    fail "HTTPS activation should verify the app entrypoint directly"
fi
assert_contains "HTTPS frontend readiness bypasses only the loopback self-signed certificate" "$HARNESS_CALL_LOG" \
    "curl -fkSs -o /dev/null --max-time 2 https://127.0.0.1/"

# Persisted HTTP remains authoritative even if stale certificate files remain
# on disk from an older HTTPS configuration.
make_stage stale-certificates
mkdir -p "$STAGE/ssl"
: > "$STAGE/ssl/cert.pem"
: > "$STAGE/ssl/key.pem"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "stale certificates do not change persisted HTTP mode"
else
    fail "persisted HTTP with stale certificates should preflight"
fi
assert_contains "HTTP transport is explicitly preserved" "$HARNESS_OUTPUT_LOG" "Preserving HTTP mode"
if cmp -s "$STAGE/client/nginx.http.conf" "$STAGE/client/nginx.conf"; then
    pass "HTTP nginx configuration remains active"
else
    fail "stale certificates selected the wrong nginx configuration"
fi

# The Windows installer supports WSL distros whose Docker daemon is managed by
# `service` or an init script. A healthy daemon must remain authoritative even
# when systemctl is unavailable; native Linux retains its boot-enable guard.
make_stage wsl-without-systemd
if FAKE_WSL=true FAKE_SYSTEMD_UNAVAILABLE=true run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "healthy Docker on non-systemd WSL preflights"
else
    fail "non-systemd WSL should rely on the Docker daemon probe"
fi
assert_not_contains "non-systemd WSL skips the systemd boot check" "$HARNESS_CALL_LOG" "systemctl is-enabled docker"
assert_contains "non-systemd WSL reaches the Docker daemon probe" "$HARNESS_CALL_LOG" "docker info"

make_stage native-linux-without-systemd
if FAKE_SYSTEMD_UNAVAILABLE=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "native Linux without Docker boot enablement should fail preflight"
else
    pass "native Linux retains the Docker boot-enable guard"
fi
assert_contains "native Linux boot failure is diagnosed" "$HARNESS_OUTPUT_LOG" "Docker is not enabled at boot"
assert_not_contains "native Linux boot failure prevents image pulls" "$HARNESS_CALL_LOG" " pull"

# A transient WSL registry outage is diagnostic-only in non-interactive mode;
# it must not invoke sudo, edit host networking, or prune the build cache.
make_stage wsl-outage
if FAKE_WSL=true FAKE_REGISTRY_FAILURE=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "WSL registry outage should fail preflight"
else
    pass "WSL registry outage fails without repair mutations"
fi
assert_not_contains "WSL failure does not invoke sudo" "$HARNESS_CALL_LOG" "sudo "
assert_not_contains "WSL failure does not prune build cache" "$HARNESS_CALL_LOG" "builder prune -af"
assert_not_contains "WSL failure prevents pulls" "$HARNESS_CALL_LOG" " pull"

# A prepared activation is local-only: its marker binds every required image
# ID and Compose runs with --no-build/--pull never, so a later registry outage
# must not block activation or trigger WSL host-network repair.
make_stage wsl-prepared-offline-activation
if FAKE_WSL=true run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "WSL activation fixture preflights while the registry is available"
else
    fail "WSL activation fixture should preflight"
fi
assert_contains "WSL preflight checks required registry connectivity" "$HARNESS_CALL_LOG" "registry-1.docker.io"
: > "$HARNESS_CALL_LOG"
if FAKE_WSL=true FAKE_REGISTRY_FAILURE=true \
    run_stage "$STAGE" --non-interactive --skip-build; then
    pass "prepared WSL activation tolerates a registry outage"
else
    fail "prepared WSL activation should not require registry connectivity"
fi
assert_not_contains "prepared WSL activation skips the registry probe" "$HARNESS_CALL_LOG" "registry-1.docker.io"
assert_not_contains "prepared WSL activation does not mutate host networking" "$HARNESS_CALL_LOG" "sudo "
assert_not_contains "prepared WSL activation does not clear build cache" "$HARNESS_CALL_LOG" "builder prune -af"
assert_not_contains "prepared WSL activation skips image pulls" "$HARNESS_CALL_LOG" " pull --ignore-buildable"
assert_not_contains "prepared WSL activation skips image builds" "$HARNESS_CALL_LOG" " build --no-cache"
assert_contains "prepared WSL activation reaches local-only Compose startup" "$HARNESS_CALL_LOG" "up --remove-orphans -d --wait --wait-timeout 300 --no-build --pull never"
if [ -f "$STAGE/.update-preflight-complete" ]; then
    fail "successful offline WSL activation should consume its marker"
else
    pass "successful offline WSL activation consumes its marker"
fi

if [ "$FAILURES" -ne 0 ]; then
    while IFS= read -r -d '' output; do
        echo "--- $output" >&2
        sed 's/^/    /' "$output" >&2
    done < <(find "$TMP_DIR" -type f -name output.log -print0)
    echo "$FAILURES failure(s)" >&2
    exit 1
fi

echo "All run-fleet upgrade checks passed."
