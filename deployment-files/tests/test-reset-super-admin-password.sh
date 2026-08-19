#!/usr/bin/env bash
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_FILES_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR=$(mktemp -d)
STAGE="$TMP_DIR/deployment"
BIN_DIR="$TMP_DIR/bin"
CALL_LOG="$TMP_DIR/docker-call.log"
STDIN_LOG="$TMP_DIR/docker-stdin.log"
FAILURES=0

trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
    echo "FAIL: $*" >&2
    FAILURES=$((FAILURES + 1))
}

assert_contains() {
    local description="$1" expected="$2"
    if grep -qF -- "$expected" "$CALL_LOG"; then
        echo "ok: $description"
    else
        fail "$description: expected '$expected'"
    fi
}

mkdir -p "$STAGE" "$BIN_DIR"
cp "$DEPLOYMENT_FILES_DIR/reset-super-admin-password.sh" "$STAGE/"
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

if [ "$FAILURES" -ne 0 ]; then
    exit 1
fi
