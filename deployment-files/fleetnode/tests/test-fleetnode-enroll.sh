#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source "$SCRIPT_DIR/../fleetnode-enroll"

TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

FLEETNODE_BIN="$TEST_DIR/fleetnode"
STATE_DIR="$TEST_DIR/state"
STATE_PATH="$STATE_DIR/state.yaml"
CALLS="$TEST_DIR/calls"
SYSTEMCTL_CALLS="$TEST_DIR/systemctl-calls"
TRACE="$TEST_DIR/trace"
export CALLS STATE_DIR STATE_PATH TRACE

cat >"$FLEETNODE_BIN" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CALLS"
printf 'fleetnode:%s\n' "$*" >>"$TRACE"
case " $* " in
  *" status "*)
    printf 'fleet_node_id:         %s\n' "${MOCK_FLEET_NODE_ID:-42}"
    printf 'api_key_present:       %s\n' "${MOCK_API_KEY_PRESENT:-false}"
    ;;
  *" enroll "*)
    [[ "${MOCK_ENROLL_EXIT:-0}" == "0" ]] || exit "$MOCK_ENROLL_EXIT"
    mkdir -p "$STATE_DIR"
    touch "$STATE_PATH"
    ;;
  *" refresh "*) exit "${MOCK_REFRESH_EXIT:-0}" ;;
esac
EOF
chmod 0755 "$FLEETNODE_BIN"

id() {
  [[ "${1:-}" == "-u" ]] && { printf '0\n'; return; }
  command id "$@"
}

runuser() {
  [[ "$1" == "-u" && "$2" == "fleetnode" && "$3" == "--" ]] || return 1
  shift 3
  "$@"
}

systemctl() {
  printf '%s\n' "$*" >>"$SYSTEMCTL_CALLS"
  printf 'systemctl:%s\n' "$*" >>"$TRACE"
  if [[ "${MOCK_SYSTEMCTL_FAIL_ONCE:-0}" == "1" ]]; then
    MOCK_SYSTEMCTL_FAIL_ONCE=0
    return 1
  fi
}

reset_test() {
  rm -f "$CALLS" "$SYSTEMCTL_CALLS" "$TRACE"
  rm -rf "$STATE_DIR"
  unset MOCK_API_KEY_PRESENT MOCK_ENROLL_EXIT MOCK_FLEET_NODE_ID MOCK_REFRESH_EXIT MOCK_SYSTEMCTL_FAIL_ONCE
}

assert_line() {
  local file="$1" expected="$2"
  grep -Fxq -- "$expected" "$file" || {
    echo "expected line not found in $file: $expected" >&2
    [[ ! -f "$file" ]] || sed 's/^/  /' "$file" >&2
    exit 1
  }
}

assert_no_line() {
  local file="$1" unexpected="$2"
  if [[ -f "$file" ]] && grep -Fq -- "$unexpected" "$file"; then
    echo "unexpected text found in $file: $unexpected" >&2
    sed 's/^/  /' "$file" >&2
    exit 1
  fi
}

assert_sequence() {
  local first="$1" second="$2"
  [[ "$(sed -n '1p' "$TRACE")" == "$first" ]] || {
    echo "unexpected first operation: $(sed -n '1p' "$TRACE")" >&2
    exit 1
  }
  [[ "$(sed -n '2p' "$TRACE")" == "$second" ]] || {
    echo "unexpected second operation: $(sed -n '2p' "$TRACE")" >&2
    exit 1
  }
}

# A fresh host enrolls with every caller-supplied argument before activation.
reset_test
main --server-url=https://fleet.example.com --name=test-node
assert_line "$CALLS" "--state-dir $STATE_DIR enroll --server-url=https://fleet.example.com --name=test-node"
assert_line "$SYSTEMCTL_CALLS" "enable --now fleet-node.service"
assert_sequence \
  "fleetnode:--state-dir $STATE_DIR enroll --server-url=https://fleet.example.com --name=test-node" \
  "systemctl:enable --now fleet-node.service"

# Partial state resumes the supported refresh flow, then activates the service.
reset_test
mkdir -p "$STATE_DIR"
touch "$STATE_PATH"
MOCK_API_KEY_PRESENT=false
export MOCK_API_KEY_PRESENT
main --server-url=https://fleet.example.com
assert_line "$CALLS" "--state-dir $STATE_DIR status"
assert_line "$CALLS" "--state-dir $STATE_DIR refresh"
assert_no_line "$CALLS" " enroll "
assert_line "$SYSTEMCTL_CALLS" "enable --now fleet-node.service"

# Complete state only retries service activation.
reset_test
mkdir -p "$STATE_DIR"
touch "$STATE_PATH"
MOCK_API_KEY_PRESENT=true
export MOCK_API_KEY_PRESENT
main --server-url=https://fleet.example.com
assert_line "$CALLS" "--state-dir $STATE_DIR status"
assert_no_line "$CALLS" " enroll "
assert_no_line "$CALLS" " refresh "
assert_line "$SYSTEMCTL_CALLS" "enable --now fleet-node.service"

# A service-start failure leaves complete state that the same command can retry.
reset_test
MOCK_API_KEY_PRESENT=true
MOCK_SYSTEMCTL_FAIL_ONCE=1
export MOCK_API_KEY_PRESENT MOCK_SYSTEMCTL_FAIL_ONCE
if main --server-url=https://fleet.example.com; then
  echo "failed service activation unexpectedly succeeded" >&2
  exit 1
fi
main --server-url=https://fleet.example.com
[[ "$(grep -Fc ' enroll ' "$CALLS")" == "1" ]] || {
  echo "service activation retry reran enrollment" >&2
  exit 1
}
assert_line "$CALLS" "--state-dir $STATE_DIR status"
[[ "$(grep -Fc 'enable --now fleet-node.service' "$SYSTEMCTL_CALLS")" == "2" ]] || {
  echo "service activation was not retried" >&2
  exit 1
}

# --force always reaches enroll with the original arguments, even with state.
reset_test
mkdir -p "$STATE_DIR"
touch "$STATE_PATH"
main --server-url=https://fleet.example.com --force
assert_line "$CALLS" "--state-dir $STATE_DIR enroll --server-url=https://fleet.example.com --force"
assert_no_line "$CALLS" " status"
assert_line "$SYSTEMCTL_CALLS" "enable --now fleet-node.service"

# Failed enrollment and refresh attempts must not activate the service.
reset_test
MOCK_ENROLL_EXIT=1
export MOCK_ENROLL_EXIT
if main --server-url=https://fleet.example.com; then
  echo "failed enrollment unexpectedly succeeded" >&2
  exit 1
fi
[[ ! -e "$SYSTEMCTL_CALLS" ]] || { echo "failed enrollment called systemctl" >&2; exit 1; }

reset_test
mkdir -p "$STATE_DIR"
touch "$STATE_PATH"
MOCK_API_KEY_PRESENT=false
MOCK_REFRESH_EXIT=1
export MOCK_API_KEY_PRESENT MOCK_REFRESH_EXIT
if main --server-url=https://fleet.example.com; then
  echo "failed refresh unexpectedly succeeded" >&2
  exit 1
fi
[[ ! -e "$SYSTEMCTL_CALLS" ]] || { echo "failed refresh called systemctl" >&2; exit 1; }

echo "Fleet Node enrollment helper tests passed"
