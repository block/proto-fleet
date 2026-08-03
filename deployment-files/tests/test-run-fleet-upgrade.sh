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

make_stage() {
    local name="$1"
    STAGE="$TMP_DIR/$name"
    mkdir -p "$STAGE/bin" "$STAGE/client"
    cp "$DEPLOY_DIR/run-fleet.sh" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.alerts.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.system-monitoring.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/docker-compose.tracing.yaml" "$STAGE/"
    cp "$DEPLOY_DIR/client/nginx.http.conf" "$STAGE/client/"
    cp "$DEPLOY_DIR/client/nginx.https.conf" "$STAGE/client/"
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
        echo 'Options: --wait --wait-timeout'
        ;;
    *" compose "*" config --quiet "*)
        [ "${FAKE_COMPOSE_CONFIG_FAILURE:-false}" != "true" ]
        ;;
    *" compose "*" up --remove-orphans "*)
        [ "${FAKE_ACTIVATION_FAILURE:-false}" != "true" ]
        ;;
    *" compose "*" exec "*)
        echo 't'
        ;;
    *" image inspect proto-fleet-timescaledb:latest "*)
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
}

run_stage() {
    local stage="$1"
    shift
    CALL_LOG="$stage/calls.log" \
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
assert_contains "preflight pulls images" "$STAGE/calls.log" " pull"
assert_contains "preflight builds images" "$STAGE/calls.log" " build --no-cache"
assert_not_contains "preflight leaves services running" "$STAGE/calls.log" " down --remove-orphans"
assert_not_contains "preflight never starts replacement services" "$STAGE/calls.log" " up --remove-orphans"
[ -f "$STAGE/.update-preflight-complete" ] || fail "preflight should create its activation marker"
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
assert_contains "activation stops the old stack" "$STAGE/calls.log" " down --remove-orphans"
assert_contains "activation starts the new stack" "$STAGE/calls.log" " up --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "successful activation should consume its marker"

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
if run_stage "$STAGE" --non-interactive --preflight-only; then
    fail "missing TimescaleDB image should fail preflight"
else
    pass "missing TimescaleDB image fails preflight"
fi
assert_not_contains "missing database image prevents builds" "$STAGE/calls.log" " build --no-cache"
assert_not_contains "missing database image prevents teardown" "$STAGE/calls.log" " down --remove-orphans"
[ ! -f "$STAGE/.update-preflight-complete" ] || fail "missing database image must not leave a preflight marker"

make_stage mistagged-tsdb-image
mkdir -p "$STAGE/image-fixture"
printf '[{"Config":"config.json","RepoTags":["wrong-image:latest"],"Layers":[]}]\n' > "$STAGE/image-fixture/manifest.json"
(cd "$STAGE/image-fixture" && tar -cf - manifest.json) | gzip > "$STAGE/images/timescaledb.tar.gz"
rm -rf "$STAGE/image-fixture"
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
