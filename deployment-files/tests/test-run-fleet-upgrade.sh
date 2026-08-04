#!/bin/bash
# Exercises the non-interactive upgrade boundary without touching host Docker,
# networking, or services. External commands are recorded through PATH shims.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR=$(mktemp -d)
REAL_GREP=$(command -v grep)
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
    if grep -qF "$expected" "$file"; then
        pass "$description"
    else
        fail "$description: expected '$expected' in $file"
    fi
}

assert_not_contains() {
    local description="$1" file="$2" unexpected="$3"
    if grep -qF "$unexpected" "$file"; then
        fail "$description: unexpected '$unexpected' in $file"
    else
        pass "$description"
    fi
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
        'ENABLE_TRACING=true' > "$env_file"
    printf 'ENABLE_TRACING="FALSE" \r\n' >> "$env_file"
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
    (
        cd "$stage" || exit 1
        if command -v sha256sum >/dev/null 2>&1; then
            find . -type f \
                ! -path './.env' \
                ! -path './.update-preflight-complete' \
                ! -path './.update-preflight-complete.tmp.*' \
                ! -path './client/nginx.conf' \
                ! -path './ssl/*' \
                ! -path './server/influx_config/.env' \
                ! -path './ha/node.env' \
                ! -path './deployment-manifest.sha256' \
                -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > deployment-manifest.sha256
        else
            find . -type f \
                ! -path './.env' \
                ! -path './.update-preflight-complete' \
                ! -path './.update-preflight-complete.tmp.*' \
                ! -path './client/nginx.conf' \
                ! -path './ssl/*' \
                ! -path './server/influx_config/.env' \
                ! -path './ha/node.env' \
                ! -path './deployment-manifest.sha256' \
                -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256 > deployment-manifest.sha256
        fi
    )
}

make_stage() {
    local name="$1"
    STAGE="$TMP_DIR/$name"
    local runtime="$STAGE-runtime"
    HARNESS_BIN_DIR="$runtime/bin"
    HARNESS_CALL_LOG="$runtime/calls.log"
    HARNESS_OUTPUT_LOG="$runtime/output.log"
    mkdir -p "$STAGE/client" "$STAGE/ha" "$STAGE/server/monitoring/grafana/provisioning/datasources" "$HARNESS_BIN_DIR"
    : > "$HARNESS_CALL_LOG"
    : > "$HARNESS_OUTPUT_LOG"
    cp "$DEPLOY_DIR/run-fleet.sh" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.alerts.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.system-monitoring.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.tracing.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/ha/compose.yaml" "$STAGE/ha/"
    cp "$DEPLOY_DIR/client/nginx.http.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/client/nginx.https.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/server/otel-collector-config.datadog.yaml" "$STAGE/server/"
    printf 'apiVersion: 1\n' > "$STAGE/server/monitoring/grafana/provisioning/datasources/base.yaml"
    printf '%s\n' "version: $RELEASE_TAG" 'commit: test-release' > "$STAGE/version.txt"
    "$DEPLOY_DIR/scripts/pin-release-images.sh" "$STAGE" "$RELEASE_TAG"
    mkdir -p "$STAGE/images" "$STAGE/image-fixture"
    printf '[{"Config":"config.json","RepoTags":["%s","%s"],"Layers":[]}]\n' \
        "$TIMESCALEDB_IMAGE" "$TIMESCALEDB_HA_IMAGE" > "$STAGE/image-fixture/manifest.json"
    (cd "$STAGE/image-fixture" && tar -cf - manifest.json) | gzip > "$STAGE/images/timescaledb.tar.gz"
    rm -rf "$STAGE/image-fixture"
    write_valid_env "$STAGE/.env"
    : > "$HARNESS_CALL_LOG"

    cat > "$HARNESS_BIN_DIR/docker" <<'EOF'
#!/bin/bash
printf 'docker' >> "$CALL_LOG"
printf ' %s' "$@" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"

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
[ "${FAKE_SYSTEMD_UNAVAILABLE:-false}" != "true" ]
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

    cat > "$HARNESS_BIN_DIR/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$CALL_LOG"
case " $* " in
    *registry-1.docker.io*)
        [ "${FAKE_REGISTRY_FAILURE:-false}" != "true" ]
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
    REAL_GREP="$REAL_GREP" \
    PATH="$HARNESS_BIN_DIR:$PATH" \
    /bin/bash "$stage/run-fleet.sh" "$@" > "$HARNESS_OUTPUT_LOG" 2>&1
}

# The updater-specific option is intentionally deferred until its Compose
# overlay is shipped and packaged later in the stack.
make_stage help
if run_stage "$STAGE" --help; then
    assert_not_contains "help omits the unavailable updater overlay" "$HARNESS_OUTPUT_LOG" "enable-one-click-updates"
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
if COMPOSE_PROJECT_NAME=fleet-blue run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "explicit Compose project override preflights"
else
    fail "explicit Compose project override should be preserved"
fi
assert_contains "explicit project override reaches Compose" "$HARNESS_CALL_LOG" "compose --project-name fleet-blue"

make_stage project-volume
env_without_password="$STAGE/.env.without-password"
grep -Ev '^DB_PASSWORD=' "$STAGE/.env" > "$env_without_password"
mv "$env_without_password" "$STAGE/.env"
if printf 'n\n' | COMPOSE_PROJECT_NAME=fleet-blue FAKE_DOCKER_VOLUME=fleet-blue_timescaledb-data run_stage "$STAGE"; then
    fail "declining removal of an overridden project volume should abort"
else
    pass "volume guard uses the explicit Compose project override"
fi
assert_contains "volume guard finds the overridden project volume" "$HARNESS_OUTPUT_LOG" "Detected existing TimescaleDB data volume: fleet-blue_timescaledb-data"

make_stage invalid-project-override
if COMPOSE_PROJECT_NAME='Fleet Blue' run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "invalid Compose project override should fail"
else
    pass "invalid Compose project override fails before Docker activity"
fi
assert_contains "invalid project override is diagnosed" "$HARNESS_OUTPUT_LOG" "COMPOSE_PROJECT_NAME must start with a lowercase letter or digit"
assert_not_contains "invalid project override cannot target Docker" "$HARNESS_CALL_LOG" "docker "

# A successful preflight validates and prepares images, but never stops or
# starts the active stack. It records a same-directory activation marker.
make_stage preflight-parent/deployment
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
if [ "$(grep -c '^ENABLE_BETA_ALERTS=' "$STAGE/.env")" -eq 1 ] && grep -q '^ENABLE_BETA_ALERTS=false$' "$STAGE/.env"; then
    pass "persisted booleans use the last Compose-style assignment"
else
    fail "persisted booleans did not honor last-value-wins"
fi
if [ "$(grep -c '^SESSION_COOKIE_SECURE=' "$STAGE/.env")" -eq 1 ] && grep -q '^SESSION_COOKIE_SECURE=false$' "$STAGE/.env"; then
    pass "cookie mode uses the last Compose-style assignment"
else
    fail "cookie mode did not honor last-value-wins"
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

# The updater preflights and activates directories that are both named
# `deployment`; only their parent changes. Absolute paths in rendered Compose
# output are normalized so that this safe relocation does not invalidate the
# proof, while Compose's native project identity stays stable.
make_stage relocated-preflight/deployment
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "relocation fixture preflight should succeed"
fi
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
if FAKE_ACTIVATION_FAILURE=true run_stage "$STAGE" --non-interactive --skip-build; then
    fail "simulated activation failure should propagate"
else
    pass "activation failure propagates"
fi
[ -f "$STAGE/.update-preflight-complete" ] || fail "failed activation should retain its recovery marker"
assert_contains "activation recovery command keeps the resolved project" "$HARNESS_OUTPUT_LOG" "docker compose --project-name failed-activation"

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
mkdir -p "$STAGE/image-fixture"
printf '[{"Config":"config.json","RepoTags":["proto-fleet-timescaledb:latest","%s"],"Layers":[]}]\n' \
    "$TIMESCALEDB_HA_IMAGE" > "$STAGE/image-fixture/manifest.json"
(cd "$STAGE/image-fixture" && tar -cf - manifest.json) | gzip > "$STAGE/images/timescaledb.tar.gz"
rm -rf "$STAGE/image-fixture"
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
mkdir -p "$STAGE/image-fixture"
printf '[{"Config":"config.json","RepoTags":["%s","%s","proto-fleet-timescaledb-ha:latest"],"Layers":[]}]\n' \
    "$TIMESCALEDB_IMAGE" "$TIMESCALEDB_HA_IMAGE" > "$STAGE/image-fixture/manifest.json"
(cd "$STAGE/image-fixture" && tar -cf - manifest.json) | gzip > "$STAGE/images/timescaledb.tar.gz"
rm -rf "$STAGE/image-fixture"
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

if [ "$FAILURES" -ne 0 ]; then
    while IFS= read -r -d '' output; do
        echo "--- $output" >&2
        sed 's/^/    /' "$output" >&2
    done < <(find "$TMP_DIR" -type f -name output.log -print0)
    echo "$FAILURES failure(s)" >&2
    exit 1
fi

echo "All run-fleet upgrade checks passed."
