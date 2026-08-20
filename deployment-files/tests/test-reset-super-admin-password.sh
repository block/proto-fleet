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

# The command (env assignments included) must fail without ever invoking the
# fake docker/fleet-ha, i.e. without creating its call log.
assert_fails_before_docker() {
    local description="$1" log_file="$2"
    shift 2
    # stdin is pinned to /dev/null so a terminal-run suite cannot trip the
    # wrapper's interactive-TTY rejection instead of the check under test.
    if "$@" >/dev/null 2>&1 </dev/null; then
        fail "$description: command unexpectedly succeeded"
    elif [ -e "$log_file" ]; then
        fail "$description: Docker was invoked"
    else
        echo "ok: $description"
    fi
}

mkdir -p "$STAGE/scripts" "$BIN_DIR"
cp "$DEPLOYMENT_FILES_DIR/reset-super-admin-password.sh" "$STAGE/"
cp "$DEPLOYMENT_FILES_DIR/scripts/compose-project.sh" "$STAGE/scripts/"
cp "$DEPLOYMENT_FILES_DIR/scripts/docker-daemon.sh" "$STAGE/scripts/"
printf 'services: {}\n' > "$STAGE/docker-compose.yaml"
printf 'COMPOSE_PROJECT_NAME=fleet-recovery\n' > "$STAGE/.env"
printf 'proto-fleet-docker-daemon-v1:test-daemon\n' > "$STAGE/.docker-daemon-id"

cat > "$BIN_DIR/docker" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "info" ]; then
    printf '%s\n' "${FAKE_DOCKER_ID:-test-daemon}"
    exit 0
fi
if [ "${1:-}" = "context" ] && [ "${2:-}" = "show" ]; then
    printf '%s\n' "${FAKE_DOCKER_CURRENT_CONTEXT:-pinned-context}"
    exit 0
fi
printf 'cwd=%s\n' "$PWD" > "$CALL_LOG"
printf 'docker_host=%s\n' "${DOCKER_HOST:-}" >> "$CALL_LOG"
printf 'docker_context=%s\n' "${DOCKER_CONTEXT:-}" >> "$CALL_LOG"
printf 'args=' >> "$CALL_LOG"
printf ' <%s>' "$@" >> "$CALL_LOG"
printf '\n' >> "$CALL_LOG"
cat > "$STDIN_LOG"
EOF
cat > "$BIN_DIR/openssl" <<'EOF'
#!/usr/bin/env bash
printf '0123456789abcdefghijklmnopqrstuv\n'
EOF
chmod +x "$BIN_DIR/docker" "$BIN_DIR/openssl" "$STAGE/reset-super-admin-password.sh"

if ! printf 'replacement-password\n' | \
    PATH="$BIN_DIR:$PATH" CALL_LOG="$CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh" --password-stdin; then
    fail "wrapper invocation failed"
fi

assert_contains "runs from deployment directory" "cwd=$STAGE"
assert_contains "uses the verified selected Docker daemon" "args= <compose>"
assert_contains "pins the Docker context for the check and Compose call" "docker_context=pinned-context"
assert_contains "uses the persisted Compose project" " <--project-name> <fleet-recovery>"
assert_contains "pins the project directory" " <--project-directory> <$STAGE>"
assert_contains "loads the deployment env" " <--env-file> <$STAGE/.env>"
assert_contains "uses the bundled compose file" " <-f> <$STAGE/docker-compose.yaml>"
assert_contains "runs a dependency-free disposable non-TTY container" " <run> <--rm> <--no-deps> <-T> <fleet-api>"
assert_contains "uses the absolute fleetd path" " </app/fleetd> <admin> <reset-password>"
assert_contains "forces credential-free container output" " <--password-stdin>"

if [ "$(cat "$STDIN_LOG")" = "replacement-password" ]; then
    echo "ok: forwards stdin"
else
    fail "stdin was not forwarded"
fi

for override in COMPOSE_PROJECT_NAME DB_DSN DB_PASSWORD; do
    OVERRIDE_CALL_LOG="$TMP_DIR/${override}.docker-call.log"
    assert_fails_before_docker "rejects caller $override override before Docker" "$OVERRIDE_CALL_LOG" \
        env "$override=conflicting-value" PATH="$BIN_DIR:$PATH" \
        CALL_LOG="$OVERRIDE_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
        "$STAGE/reset-super-admin-password.sh"
done

SELECTED_DAEMON_CALL_LOG="$TMP_DIR/selected-daemon.docker-call.log"
if ! printf 'replacement-password\n' | DOCKER_HOST=unix:///run/user/1000/docker.sock \
    DOCKER_CONTEXT=rootless PATH="$BIN_DIR:$PATH" CALL_LOG="$SELECTED_DAEMON_CALL_LOG" \
    STDIN_LOG="$STDIN_LOG" "$STAGE/reset-super-admin-password.sh" --password-stdin; then
    fail "matching custom Docker daemon was rejected"
fi
assert_contains "allows the installation's custom Docker host" \
    "docker_host=unix:///run/user/1000/docker.sock" "$SELECTED_DAEMON_CALL_LOG"
assert_contains "allows the installation's Docker context" \
    "docker_context=rootless" "$SELECTED_DAEMON_CALL_LOG"

MISMATCHED_DAEMON_CALL_LOG="$TMP_DIR/mismatched-daemon.docker-call.log"
assert_fails_before_docker "mismatched Docker daemon fails closed before Compose" "$MISMATCHED_DAEMON_CALL_LOG" \
    env FAKE_DOCKER_ID=other-daemon PATH="$BIN_DIR:$PATH" \
    CALL_LOG="$MISMATCHED_DAEMON_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh" --password-stdin

mv "$STAGE/.docker-daemon-id" "$TMP_DIR/.docker-daemon-id.bak"
MISSING_DAEMON_CALL_LOG="$TMP_DIR/missing-daemon.docker-call.log"
assert_fails_before_docker "missing Docker daemon state fails closed before Docker" "$MISSING_DAEMON_CALL_LOG" \
    env PATH="$BIN_DIR:$PATH" CALL_LOG="$MISSING_DAEMON_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh"
mv "$TMP_DIR/.docker-daemon-id.bak" "$STAGE/.docker-daemon-id"

# A real pty via script(1) is the only way to make [ -t 0 ] true here; stdin
# of script itself stays /dev/null so an interactive suite run cannot block.
TTY_CALL_LOG="$TMP_DIR/tty.docker-call.log"
if command -v script >/dev/null 2>&1; then
    if [ "$(uname -s)" = "Darwin" ]; then
        script -q /dev/null env PATH="$BIN_DIR:$PATH" CALL_LOG="$TTY_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
            "$STAGE/reset-super-admin-password.sh" --password-stdin </dev/null >/dev/null 2>&1
    else
        script -qec "env PATH='$BIN_DIR:$PATH' CALL_LOG='$TTY_CALL_LOG' STDIN_LOG='$STDIN_LOG' \
            '$STAGE/reset-super-admin-password.sh' --password-stdin" /dev/null </dev/null >/dev/null 2>&1
    fi
    if [ -e "$TTY_CALL_LOG" ]; then
        fail "interactive --password-stdin reached Docker instead of failing closed"
    else
        echo "ok: rejects interactive --password-stdin before Docker"
    fi
else
    echo "skip: script(1) unavailable; interactive TTY rejection not exercised"
fi

GENERATED_CALL_LOG="$TMP_DIR/generated.docker-call.log"
GENERATED_STDIN_LOG="$TMP_DIR/generated.docker-stdin.log"
if ! GENERATED_OUTPUT=$(PATH="$BIN_DIR:$PATH" CALL_LOG="$GENERATED_CALL_LOG" \
    STDIN_LOG="$GENERATED_STDIN_LOG" "$STAGE/reset-super-admin-password.sh"); then
    fail "generated password recovery failed"
fi
assert_contains "generated recovery still forces stdin mode" " <--password-stdin>" "$GENERATED_CALL_LOG"
if [ "$(cat "$GENERATED_STDIN_LOG")" = "0123456789abcdefghijklmnopqrstuv" ]; then
    echo "ok: generated password reaches only container stdin"
else
    fail "generated password was not sent through container stdin"
fi
case "$GENERATED_OUTPUT" in
    *"Temporary password: 0123456789abcdefghijklmnopqrstuv"*)
        echo "ok: host prints generated password after reset"
        ;;
    *) fail "host did not print the generated password" ;;
esac
if grep -qF '0123456789abcdefghijklmnopqrstuv' "$GENERATED_CALL_LOG"; then
    fail "generated password appeared in Docker arguments"
else
    echo "ok: generated password is absent from Docker arguments"
fi

for rejected_arg in --db-explicit-dsn --db-address --username unexpected; do
    REJECTED_ARG_CALL_LOG="$TMP_DIR/rejected-${rejected_arg#--}.docker-call.log"
    assert_fails_before_docker "rejects unsupported argument $rejected_arg before Docker" "$REJECTED_ARG_CALL_LOG" \
        env PATH="$BIN_DIR:$PATH" CALL_LOG="$REJECTED_ARG_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
        "$STAGE/reset-super-admin-password.sh" "$rejected_arg"
done

MULTI_ARG_CALL_LOG="$TMP_DIR/multiple-args.docker-call.log"
assert_fails_before_docker "rejects multiple arguments before Docker" "$MULTI_ARG_CALL_LOG" \
    env PATH="$BIN_DIR:$PATH" CALL_LOG="$MULTI_ARG_CALL_LOG" STDIN_LOG="$STDIN_LOG" \
    "$STAGE/reset-super-admin-password.sh" --password-stdin --db-address database

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
assert_fails_before_docker "ambiguous standalone and HA state fails closed before Docker" "$AMBIGUOUS_CALL_LOG" \
    env PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$AMBIGUOUS_CALL_LOG" "$STAGE/reset-super-admin-password.sh"

rm "$STAGE/.env"

printf '%s\n' "$TMP_DIR/not-this-deployment" > "$HA_ACTIVE_INSTALL"
MISMATCH_CALL_LOG="$TMP_DIR/mismatched-marker-docker-call.log"
assert_fails_before_docker "mismatched HA installation marker fails closed before Docker" "$MISMATCH_CALL_LOG" \
    env PATH="$BIN_DIR:$PATH" PROTO_FLEET_HA_CONFIG_DIR="$HA_CONFIG_DIR" \
    CALL_LOG="$MISMATCH_CALL_LOG" "$STAGE/reset-super-admin-password.sh"
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
