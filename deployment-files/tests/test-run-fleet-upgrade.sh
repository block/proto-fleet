#!/bin/bash
# Exercises the non-interactive upgrade boundary without touching host Docker,
# networking, or services. External commands are recorded through PATH shims.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR=$(mktemp -d)
REAL_GREP=$(command -v grep)
FAILURES=0

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
                ! -path './deployment-manifest.sha256' \
                -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256 > deployment-manifest.sha256
        fi
    )
}

make_stage() {
    local name="$1"
    STAGE="$TMP_DIR/$name"
    local runtime="$STAGE-runtime"
    mkdir -p "$STAGE/client" "$STAGE/server/monitoring/grafana/provisioning/datasources" "$runtime/bin"
    ln -s "$runtime/bin" "$STAGE/bin"
    : > "$runtime/calls.log"
    : > "$runtime/output.log"
    ln -s "$runtime/calls.log" "$STAGE/calls.log"
    ln -s "$runtime/output.log" "$STAGE/output.log"
    cp "$DEPLOY_DIR/run-fleet.sh" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.alerts.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.system-monitoring.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.tracing.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/client/nginx.http.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/client/nginx.https.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/server/otel-collector-config.datadog.yaml" "$STAGE/server/"
    printf 'apiVersion: 1\n' > "$STAGE/server/monitoring/grafana/provisioning/datasources/base.yaml"
    printf '%s\n' 'version: v1.2.3' 'commit: test-release' > "$STAGE/version.txt"
    mkdir -p "$STAGE/images" "$STAGE/image-fixture"
    printf '[{"Config":"config.json","RepoTags":["proto-fleet-timescaledb:latest"],"Layers":[]}]\n' > "$STAGE/image-fixture/manifest.json"
    (cd "$STAGE/image-fixture" && tar -cf - manifest.json) | gzip > "$STAGE/images/timescaledb.tar.gz"
    rm -rf "$STAGE/image-fixture"
    write_valid_env "$STAGE/.env"
    : > "$STAGE/calls.log"

    cat > "$STAGE/bin/docker" <<'EOF'
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
        printf '%s\n' \
            'proto-fleet-api:latest' \
            'proto-fleet-client:latest' \
            'proto-fleet-timescaledb:latest'
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
            echo 'Image proto-fleet-timescaledb:latest Skipped'
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
    *" image inspect --format "*)
        image="${!#}"
        if [ "${FAKE_PREPARED_IMAGE_MISSING:-false}" = "true" ] && [ "$image" = "proto-fleet-api:latest" ]; then
            exit 1
        fi
        if [ "${FAKE_PREPARED_IMAGE_CHANGED:-false}" = "true" ] && [ "$image" = "proto-fleet-api:latest" ]; then
            printf 'sha256:changed-%s\n' "$image"
            exit 0
        fi
        printf 'sha256:test-%s\n' "$image"
        ;;
    *" image inspect proto-fleet-timescaledb:latest "*)
        if [ "${FAKE_TSDB_IMAGE_COLD_CACHE:-false}" = "true" ] && ! grep -qF 'docker load' "$CALL_LOG"; then
            exit 1
        fi
        [ "${FAKE_TSDB_IMAGE_MISSING:-false}" != "true" ]
        ;;
esac
EOF

    cat > "$STAGE/bin/systemctl" <<'EOF'
#!/bin/bash
printf 'systemctl %s\n' "$*" >> "$CALL_LOG"
exit 0
EOF

    cat > "$STAGE/bin/id" <<'EOF'
#!/bin/bash
if [ "${1:-}" = "-u" ]; then
    echo 0
else
    /usr/bin/id "$@"
fi
EOF

    cat > "$STAGE/bin/uname" <<'EOF'
#!/bin/bash
echo Linux
EOF

    cat > "$STAGE/bin/grep" <<'EOF'
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

    cat > "$STAGE/bin/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$CALL_LOG"
case " $* " in
    *registry-1.docker.io*)
        [ "${FAKE_REGISTRY_FAILURE:-false}" != "true" ]
        ;;
    *) exit 0 ;;
esac
EOF

    cat > "$STAGE/bin/sudo" <<'EOF'
#!/bin/bash
printf 'sudo %s\n' "$*" >> "$CALL_LOG"
exit 0
EOF

    cat > "$STAGE/bin/hostname" <<'EOF'
#!/bin/bash
if [ "${1:-}" = "-I" ]; then
    echo '192.0.2.10'
else
    echo 'proto-fleet-test'
fi
EOF

    cat > "$STAGE/bin/ip" <<'EOF'
#!/bin/bash
exit 0
EOF

    chmod +x "$STAGE/run-fleet.sh" "$STAGE/bin/"*
    write_release_manifest "$STAGE"
}

run_stage() {
    local stage="$1"
    shift
    CALL_LOG="$stage/calls.log" \
    STAGE_ROOT="$stage" \
    REAL_GREP="$REAL_GREP" \
    PATH="$stage/bin:$PATH" \
    /bin/bash "$stage/run-fleet.sh" "$@" > "$stage/output.log" 2>&1
}

# The updater-specific option is intentionally deferred until its Compose
# overlay is shipped and packaged later in the stack.
make_stage help
if run_stage "$STAGE" --help; then
    assert_not_contains "help omits the unavailable updater overlay" "$STAGE/output.log" "enable-one-click-updates"
else
    fail "--help should succeed"
fi

# A successful preflight validates and prepares images, but never stops or
# starts the active stack. It records a same-directory activation marker.
make_stage preflight
if run_stage "$STAGE" --non-interactive --preflight-only; then
    pass "valid non-interactive preflight succeeds"
else
    fail "valid non-interactive preflight should succeed"
fi
assert_contains "preflight validates Compose" "$STAGE/calls.log" "config --quiet"
assert_contains "preflight pins the Compose project identity" "$STAGE/calls.log" "compose --project-name deployment"
assert_contains "preflight pulls images" "$STAGE/calls.log" " pull"
assert_contains "preflight builds images" "$STAGE/calls.log" " build --no-cache"
assert_not_contains "preflight leaves services running" "$STAGE/calls.log" " down --remove-orphans"
assert_not_contains "preflight never starts replacement services" "$STAGE/calls.log" " up --remove-orphans"
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
: > "$STAGE/calls.log"
if run_stage "$STAGE" --non-interactive --skip-build; then
    pass "prepared activation succeeds"
else
    fail "prepared activation should succeed"
fi
assert_not_contains "activation skips pulls" "$STAGE/calls.log" " pull"
assert_not_contains "activation skips builds" "$STAGE/calls.log" " build --no-cache"
assert_contains "activation forbids implicit image preparation" "$STAGE/calls.log" "up --remove-orphans -d --wait --wait-timeout 300 --no-build --pull never"
assert_contains "activation stops the old stack" "$STAGE/calls.log" " down --remove-orphans"
assert_contains "activation starts the new stack" "$STAGE/calls.log" " up --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "successful activation should consume its marker"

# The updater preflights under a temporary parent and then renames the release
# into the stable deployment path. Absolute paths in rendered Compose output
# are normalized so that safe relocation does not invalidate the proof.
make_stage relocated-preflight
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "relocation fixture preflight should succeed"
fi
relocated_stage="$TMP_DIR/activated-release/deployment"
mkdir -p "$(dirname "$relocated_stage")"
mv "$STAGE" "$relocated_stage"
STAGE="$relocated_stage"
: > "$STAGE/calls.log"
if run_stage "$STAGE" --non-interactive --skip-build; then
    pass "preflight proof survives the updater's directory rename"
else
    fail "unchanged relocated release should activate"
fi
assert_contains "relocated activation stops the old stack" "$STAGE/calls.log" " down --remove-orphans"

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
    : > "$STAGE/calls.log"
    if run_stage "$STAGE" --non-interactive --skip-build; then
        fail "$mutation changes should invalidate preflight"
    else
        pass "$mutation changes invalidate preflight"
    fi
    assert_contains "$mutation mismatch is diagnosed" "$STAGE/output.log" "changed after preflight"
    assert_not_contains "$mutation mismatch prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
    assert_not_contains "$mutation mismatch prevents startup" "$STAGE/calls.log" " up --remove-orphans"
    [ ! -e "$STAGE/.update-preflight-complete" ] || fail "$mutation mismatch should remove the stale marker"
done

make_stage missing-prepared-image
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing-image fixture preflight should succeed"
fi
: > "$STAGE/calls.log"
if FAKE_PREPARED_IMAGE_MISSING=true run_stage "$STAGE" --non-interactive --skip-build; then
    fail "missing prepared image should fail activation validation"
else
    pass "missing prepared image fails before activation"
fi
assert_contains "missing image is diagnosed" "$STAGE/output.log" "prepared image is missing before activation"
assert_not_contains "missing image prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "missing image should invalidate the marker"

make_stage changed-prepared-image
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "changed-image fixture preflight should succeed"
fi
: > "$STAGE/calls.log"
if FAKE_PREPARED_IMAGE_CHANGED=true run_stage "$STAGE" --non-interactive --skip-build; then
    fail "changed prepared image should fail activation validation"
else
    pass "changed prepared image fails before activation"
fi
assert_contains "changed image is diagnosed" "$STAGE/output.log" "changed after preflight"
assert_not_contains "changed image prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "changed image should invalidate the marker"

make_stage added-release-file
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "added-file fixture preflight should succeed"
fi
printf 'apiVersion: 1\n' > "$STAGE/server/monitoring/grafana/provisioning/datasources/added-after-preflight.yaml"
: > "$STAGE/calls.log"
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "an added immutable release file should fail activation validation"
else
    pass "an added immutable release file fails before activation"
fi
assert_contains "added release file is diagnosed" "$STAGE/output.log" "file set does not match"
assert_not_contains "added release file prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "added release file should invalidate the marker"

make_stage missing-release-manifest
rm -f "$STAGE/deployment-manifest.sha256"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing release manifest should fail preflight"
else
    pass "missing release manifest fails preflight"
fi
assert_contains "missing manifest is diagnosed" "$STAGE/output.log" "requires the immutable release manifest"
assert_not_contains "missing manifest prevents pulls" "$STAGE/calls.log" " pull"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "missing manifest must not create a marker"

make_stage changed-during-preflight
if FAKE_MUTATE_TSDB_DURING_BUILD=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "inputs changed during preparation should fail preflight"
else
    pass "inputs changed during preparation fail preflight"
fi
assert_contains "mid-preflight mutation is diagnosed" "$STAGE/output.log" "release or configuration changed during preflight"
assert_contains "mid-preflight mutation occurs after the build starts" "$STAGE/calls.log" " build --no-cache"
assert_not_contains "mid-preflight mutation prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "mid-preflight mutation must not create a marker"

make_stage forged-marker
: > "$STAGE/.update-preflight-complete"
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "an empty forged marker should not authorize activation"
else
    pass "an empty forged marker is rejected"
fi
assert_not_contains "forged marker prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -e "$STAGE/.update-preflight-complete" ] || fail "forged marker should be invalidated"

# A failed activation retains the marker so the recovery command can retry the
# prepared release without an unsafe rebuild.
make_stage failed-activation
if ! run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "failed-activation fixture preflight should succeed"
fi
: > "$STAGE/calls.log"
if FAKE_ACTIVATION_FAILURE=true run_stage "$STAGE" --non-interactive --skip-build; then
    fail "simulated activation failure should propagate"
else
    pass "activation failure propagates"
fi
[ -f "$STAGE/.update-preflight-complete" ] || fail "failed activation should retain its recovery marker"

# Skip-build must not be usable without proof that this exact deployment
# directory completed preflight.
make_stage missing-marker
if run_stage "$STAGE" --non-interactive --skip-build; then
    fail "skip-build should fail without a preflight marker"
else
    pass "skip-build rejects an unprepared deployment"
fi
assert_not_contains "unprepared activation makes no Docker calls" "$STAGE/calls.log" "docker "

# Invalid persisted booleans fail before even read-only Docker probes.
make_stage invalid-boolean
printf 'ENABLE_TRACING=maybe\n' >> "$STAGE/.env"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "invalid persisted boolean should fail preflight"
else
    pass "invalid persisted boolean fails preflight"
fi
assert_not_contains "invalid boolean makes no Docker calls" "$STAGE/calls.log" "docker "

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
assert_not_contains "invalid settings prevent pulls" "$STAGE/calls.log" " pull"
assert_not_contains "invalid settings prevent teardown" "$STAGE/calls.log" " down --remove-orphans"

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
assert_contains "short auth secret is diagnosed" "$STAGE/output.log" "must be at least 32 characters"
assert_contains "invalid encryption key is diagnosed" "$STAGE/output.log" "must decode to exactly 32 bytes"
assert_not_contains "malformed secrets prevent pulls" "$STAGE/calls.log" " pull"

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
assert_contains "missing alert service token is diagnosed" "$STAGE/output.log" "FLEET_ALERTS_GRAFANA_TOKEN"
assert_not_contains "missing alert secrets prevent pulls" "$STAGE/calls.log" " pull"

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
    assert_contains "$role_case is diagnosed" "$STAGE/output.log" "$expected_error"
    assert_not_contains "$role_case prevents pulls" "$STAGE/calls.log" " pull"
    assert_not_contains "$role_case prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
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
assert_not_contains "invalid Compose prevents pulls" "$STAGE/calls.log" " pull"
assert_not_contains "invalid Compose prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "invalid Compose must not leave a preflight marker"

# A non-removable stale marker fails closed before Docker activity.
make_stage invalid-marker
mkdir "$STAGE/.update-preflight-complete"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "non-removable preflight marker should fail"
else
    pass "non-removable preflight marker fails closed"
fi
assert_not_contains "marker cleanup failure makes no Docker calls" "$STAGE/calls.log" "docker "

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
assert_not_contains "missing database image prevents builds" "$STAGE/calls.log" " build --no-cache"
assert_not_contains "missing database image prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
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
assert_contains "cold cache still pulls external dependencies" "$STAGE/calls.log" " pull --ignore-buildable"
assert_contains "cold cache loads the packaged database image" "$STAGE/calls.log" "docker load"
pull_line=$(grep -nF ' pull --ignore-buildable' "$STAGE/calls.log" | head -1 | cut -d: -f1)
load_line=$(grep -nF 'docker load' "$STAGE/calls.log" | head -1 | cut -d: -f1)
if [ -n "$pull_line" ] && [ -n "$load_line" ] && [ "$pull_line" -lt "$load_line" ]; then
    pass "external pull fails before any packaged database retagging"
else
    fail "external images should be pulled before loading the packaged database image"
fi

make_stage mistagged-tsdb-image
mkdir -p "$STAGE/image-fixture"
printf '[{"Config":"config.json","RepoTags":["wrong-image:latest"],"Layers":[]}]\n' > "$STAGE/image-fixture/manifest.json"
(cd "$STAGE/image-fixture" && tar -cf - manifest.json) | gzip > "$STAGE/images/timescaledb.tar.gz"
rm -rf "$STAGE/image-fixture"
write_release_manifest "$STAGE"
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "mistagged TimescaleDB archive should fail preflight"
else
    pass "mistagged TimescaleDB archive fails preflight"
fi
assert_not_contains "mistagged database image is not loaded" "$STAGE/calls.log" "docker load"
assert_not_contains "mistagged database image prevents builds" "$STAGE/calls.log" " build --no-cache"

make_stage unloaded-tsdb-image
if FAKE_TSDB_IMAGE_MISSING=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "archive that does not load the expected tag should fail preflight"
else
    pass "loaded archive must expose the expected TimescaleDB tag"
fi
assert_not_contains "unloaded database image prevents builds" "$STAGE/calls.log" " build --no-cache"
assert_not_contains "unloaded database image prevents teardown" "$STAGE/calls.log" " down --remove-orphans"

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
assert_contains "legacy HTTPS transport is preserved" "$STAGE/output.log" "Preserving legacy HTTPS mode"
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
assert_contains "HTTP transport is explicitly preserved" "$STAGE/output.log" "Preserving HTTP mode"
if cmp -s "$STAGE/client/nginx.http.conf" "$STAGE/client/nginx.conf"; then
    pass "HTTP nginx configuration remains active"
else
    fail "stale certificates selected the wrong nginx configuration"
fi

# A transient WSL registry outage is diagnostic-only in non-interactive mode;
# it must not invoke sudo, edit host networking, or prune the build cache.
make_stage wsl-outage
if FAKE_WSL=true FAKE_REGISTRY_FAILURE=true run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "WSL registry outage should fail preflight"
else
    pass "WSL registry outage fails without repair mutations"
fi
assert_not_contains "WSL failure does not invoke sudo" "$STAGE/calls.log" "sudo "
assert_not_contains "WSL failure does not prune build cache" "$STAGE/calls.log" "builder prune -af"
assert_not_contains "WSL failure prevents pulls" "$STAGE/calls.log" " pull"

if [ "$FAILURES" -ne 0 ]; then
    for output in "$TMP_DIR"/*/output.log; do
        [ -f "$output" ] || continue
        echo "--- $output" >&2
        sed 's/^/    /' "$output" >&2
    done
    echo "$FAILURES failure(s)" >&2
    exit 1
fi

echo "All run-fleet upgrade checks passed."
