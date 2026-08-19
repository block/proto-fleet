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
HA_ACTIVE_INSTALL="$HA_CONFIG_DIR/active-install"
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
assert_contains "pins the local Docker daemon" "args= <--host> <unix:///var/run/docker.sock> <compose>"
assert_contains "uses the persisted Compose project" " <--project-name> <fleet-recovery>"
assert_contains "pins the project directory" " <--project-directory> <$STAGE>"
assert_contains "loads the deployment env" " <--env-file> <$STAGE/.env>"
assert_contains "uses the bundled compose file" " <-f> <$STAGE/docker-compose.yaml>"
assert_contains "runs a dependency-free disposable non-TTY container" " <run> <--rm> <--no-deps> <-T> <fleet-api>"
assert_contains "uses the absolute fleetd path" " </app/fleetd> <admin> <reset-password>"
assert_contains "forwards command arguments" " <--password-stdin>"

if [ "$(cat "$STDIN_LOG")" = "replacement-password" ]; then
    echo "ok: forwards stdin"
else
    fail "stdin was not forwarded"
fi

for override in COMPOSE_PROJECT_NAME DB_DSN DB_PASSWORD DOCKER_HOST DOCKER_CONTEXT; do
    OVERRIDE_CALL_LOG="$TMP_DIR/${override}.docker-call.log"
    if env "$override=conflicting-value" PATH="$BIN_DIR:$PATH" \
        CALL_LOG="$OVERRIDE_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
        "$STAGE/reset-super-admin-password.sh" >/dev/null 2>&1; then
        fail "caller $override override was accepted"
    elif [ -e "$OVERRIDE_CALL_LOG" ]; then
        fail "caller $override override reached Docker"
    else
        echo "ok: rejects caller $override override before Docker"
    fi
done

for rejected_arg in --db-explicit-dsn --db-address --username unexpected; do
    REJECTED_ARG_CALL_LOG="$TMP_DIR/rejected-${rejected_arg#--}.docker-call.log"
    if PATH="$BIN_DIR:$PATH" CALL_LOG="$REJECTED_ARG_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
        "$STAGE/reset-super-admin-password.sh" "$rejected_arg" >/dev/null 2>&1; then
        fail "unsupported argument $rejected_arg was accepted"
    elif [ -e "$REJECTED_ARG_CALL_LOG" ]; then
        fail "unsupported argument $rejected_arg reached Docker"
    else
        echo "ok: rejects unsupported argument $rejected_arg before Docker"
    fi
done

MULTI_ARG_CALL_LOG="$TMP_DIR/multiple-args.docker-call.log"
if PATH="$BIN_DIR:$PATH" CALL_LOG="$MULTI_ARG_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh" --password-stdin --db-address database >/dev/null 2>&1; then
    fail "multiple recovery arguments were accepted"
elif [ -e "$MULTI_ARG_CALL_LOG" ]; then
    fail "multiple recovery arguments reached Docker"
else
    echo "ok: rejects multiple arguments before Docker"
fi

SAME_PROJECT_CALL_LOG="$TMP_DIR/same-project-docker-call.log"
if ! COMPOSE_PROJECT_NAME=fleet-recovery PATH="$BIN_DIR:$PATH" \
    CALL_LOG="$SAME_PROJECT_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh"; then
    fail "matching caller project identity was rejected"
fi
assert_contains "accepts a matching caller project identity" \
    " <--project-name> <fleet-recovery>" "$SAME_PROJECT_CALL_LOG"

mkdir -p "$STAGE/ha" "$HA_CONFIG_DIR"
printf 'HA_NODE_NAME=fleet-1\n' > "$HA_NODE_ENV"
printf '%s\n' "$STAGE" > "$HA_ACTIVE_INSTALL"
cat > "$STAGE/ha/fleet-ha" <<'EOF'
#!/usr/bin/env bash
printf 'args=' > "$HA_CALL_LOG"
printf ' <%s>' "$@" >> "$HA_CALL_LOG"
printf '\n' >> "$HA_CALL_LOG"
cat > "$HA_STDIN_LOG"
EOF
chmod +x "$STAGE/ha/fleet-ha"
rm "$STAGE/.env"

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

printf 'COMPOSE_PROJECT_NAME=fleet-recovery\n' > "$STAGE/.env"
AMBIGUOUS_CALL_LOG="$TMP_DIR/ambiguous-docker-call.log"
if PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$AMBIGUOUS_CALL_LOG" "$STAGE/reset-super-admin-password.sh" >/dev/null 2>&1; then
    fail "ambiguous standalone and HA state was accepted"
elif [ -e "$AMBIGUOUS_CALL_LOG" ]; then
    fail "ambiguous standalone and HA state reached Docker"
else
    echo "ok: ambiguous standalone and HA state fails closed before Docker"
fi

rm "$STAGE/.env"

printf '%s\n' "$TMP_DIR/not-this-deployment" > "$HA_ACTIVE_INSTALL"
MISMATCH_CALL_LOG="$TMP_DIR/mismatched-marker-docker-call.log"
if PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$MISMATCH_CALL_LOG" "$STAGE/reset-super-admin-password.sh" >/dev/null 2>&1; then
    fail "mismatched HA installation marker was accepted"
elif [ -e "$MISMATCH_CALL_LOG" ]; then
    fail "mismatched HA installation marker reached Docker"
else
    echo "ok: mismatched HA installation marker fails closed before Docker"
fi
printf '%s\n' "$STAGE" > "$HA_ACTIVE_INSTALL"

rm "$HA_ACTIVE_INSTALL"
printf 'COMPOSE_PROJECT_NAME=fleet-recovery\n' > "$STAGE/.env"
STALE_HA_CALL_LOG="$TMP_DIR/stale-ha-docker-call.log"
if ! PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$STALE_HA_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh"; then
    fail "stale unmarked HA configuration blocked standalone recovery"
fi
assert_contains "ignores stale unmarked HA configuration" \
    " <--project-name> <fleet-recovery>" "$STALE_HA_CALL_LOG"
rm "$STAGE/.env"
printf '%s\n' "$STAGE" > "$HA_ACTIVE_INSTALL"

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
