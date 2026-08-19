#!/usr/bin/env bash
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_FILES_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR=$(mktemp -d)
STAGE="$TMP_DIR/deployment"
BIN_DIR="$TMP_DIR/bin"
CALL_LOG="$TMP_DIR/docker-call.log"
STDIN_LOG="$TMP_DIR/docker-stdin.log"
HA_CONFIG_DIR="$TMP_DIR/ha-config"
HA_NODE_ENV="$HA_CONFIG_DIR/node.env"
HA_CALL_LOG="$TMP_DIR/fleet-ha-call.log"
HA_STDIN_LOG="$TMP_DIR/fleet-ha-stdin.log"
HA_DOCKER_CALL_LOG="$TMP_DIR/ha-docker-call.log"
FAILURES=0

trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
    echo "FAIL: $*" >&2
    FAILURES=$((FAILURES + 1))
}

assert_contains() {
    local description="$1" expected="$2" log_file="${3:-$CALL_LOG}"
    if [ -f "$log_file" ] && grep -qF -- "$expected" "$log_file"; then
        echo "ok: $description"
    else
        fail "$description: expected '$expected'"
    fi
}

mkdir -p "$STAGE/scripts" "$BIN_DIR"
cp "$DEPLOYMENT_FILES_DIR/reset-super-admin-password.sh" "$STAGE/"
cp "$DEPLOYMENT_FILES_DIR/scripts/compose-project.sh" "$STAGE/scripts/"
printf 'services: {}\n' > "$STAGE/docker-compose.yaml"
printf 'COMPOSE_PROJECT_NAME=fleet-recovery\n' > "$STAGE/.env"

cat > "$BIN_DIR/docker" <<'EOF'
#!/usr/bin/env bash
printf 'cwd=%s\n' "$PWD" > "$CALL_LOG"
printf 'args=' >> "$CALL_LOG"
printf ' <%s>' "$@" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"
cat > "$STDIN_LOG"
EOF
chmod +x "$BIN_DIR/docker" "$STAGE/reset-super-admin-password.sh"

if ! printf 'replacement-password\n' | \
    PATH="$BIN_DIR:$PATH" CALL_LOG="$CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh" --password-stdin; then
    fail "wrapper invocation failed"
fi

assert_contains "runs from deployment directory" "cwd=$STAGE"
assert_contains "uses the persisted Compose project" " <--project-name> <fleet-recovery>"
assert_contains "pins the project directory" " <--project-directory> <$STAGE>"
assert_contains "loads the deployment env" " <--env-file> <$STAGE/.env>"
assert_contains "uses the bundled compose file" " <-f> <$STAGE/docker-compose.yaml>"
assert_contains "runs a disposable non-TTY container" " <run> <--rm> <-T> <fleet-api>"
assert_contains "uses the absolute fleetd path" " </app/fleetd> <admin> <reset-password>"
assert_contains "forwards command arguments" " <--password-stdin>"

if [ "$(cat "$STDIN_LOG")" = "replacement-password" ]; then
    echo "ok: forwards stdin"
else
    fail "stdin was not forwarded"
fi

mkdir -p "$STAGE/ha" "$HA_CONFIG_DIR"
printf 'HA_NODE_NAME=fleet-1\n' > "$HA_NODE_ENV"
cat > "$STAGE/ha/fleet-ha" <<'EOF'
#!/usr/bin/env bash
printf 'args=' > "$HA_CALL_LOG"
printf ' <%s>' "$@" >> "$HA_CALL_LOG"
printf '\n' >> "$HA_CALL_LOG"
cat > "$HA_STDIN_LOG"
EOF
chmod +x "$STAGE/ha/fleet-ha"

if ! printf 'ha-replacement-password\n' | \
    PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$HA_DOCKER_CALL_LOG" HA_CALL_LOG="$HA_CALL_LOG" HA_STDIN_LOG="$HA_STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh" --password-stdin; then
    fail "HA wrapper invocation failed"
fi

assert_contains "delegates installed HA recovery" "args= <reset-password> <--password-stdin>" "$HA_CALL_LOG"
if [ "$(cat "$HA_STDIN_LOG")" = "ha-replacement-password" ]; then
    echo "ok: forwards stdin to fleet-ha"
else
    fail "stdin was not forwarded to fleet-ha"
fi
if [ -e "$HA_DOCKER_CALL_LOG" ]; then
    fail "HA recovery bypassed fleet-ha and invoked Docker directly"
else
    echo "ok: HA recovery does not invoke the standalone topology"
fi

chmod -x "$STAGE/ha/fleet-ha"
if PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$HA_DOCKER_CALL_LOG" "$STAGE/reset-super-admin-password.sh" >/dev/null 2>&1; then
    fail "HA recovery succeeded without an executable fleet-ha"
else
    echo "ok: HA recovery fails closed when fleet-ha is unusable"
fi

if [ "$FAILURES" -ne 0 ]; then
    exit 1
fi
