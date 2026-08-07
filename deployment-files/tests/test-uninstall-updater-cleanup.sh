#!/usr/bin/env bash
# Focused regression checks for exact host-updater binary cleanup. Production
# paths are redirected into a temporary directory; no host files are touched.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
UNINSTALL_SCRIPT="$REPO_ROOT/deployment-files/uninstall.sh"
TEST_TMP=""
TEST_TMP_RESOLVED=""
TEST_TMP_PARENT=""
TEST_TMP_PREFIX=""

cleanup_test_tmp() {
  case "${TEST_TMP:-}" in
    "$TEST_TMP_PREFIX".*)
      [[ -d "$TEST_TMP" && ! -L "$TEST_TMP" ]] \
        && rm -rf -- "${TEST_TMP:?}"
      ;;
  esac
}

if ! TEST_TMP_PARENT=$(cd "${TMPDIR:-/tmp}" 2>/dev/null && pwd -P); then
  echo "FAIL: could not resolve the temporary-directory parent" >&2
  exit 1
fi
TEST_TMP_PREFIX="${TEST_TMP_PARENT%/}/proto-fleet-uninstall-updater"
if ! TEST_TMP=$(mktemp -d "$TEST_TMP_PREFIX.XXXXXX"); then
  echo "FAIL: could not create the test temporary directory" >&2
  exit 1
fi
case "$TEST_TMP" in
  "$TEST_TMP_PREFIX".*) ;;
  *)
    echo "FAIL: mktemp returned an unexpected path: $TEST_TMP" >&2
    exit 1
    ;;
esac
if ! TEST_TMP_RESOLVED=$(cd "$TEST_TMP" 2>/dev/null && pwd -P); then
  echo "FAIL: could not resolve the test temporary directory" >&2
  rm -rf -- "${TEST_TMP:?}"
  TEST_TMP=""
  exit 1
fi
TEST_TMP="$TEST_TMP_RESOLVED"
case "$TEST_TMP" in
  "$TEST_TMP_PREFIX".*) ;;
  *)
    echo "FAIL: temporary directory escaped its expected parent: $TEST_TMP" >&2
    exit 1
    ;;
esac
trap cleanup_test_tmp EXIT

# uninstall.sh is a standalone executable, so extract only the constants and
# helpers under test instead of sourcing its interactive main path.
sed -n \
  -e '/^HOST_UPDATER_ENV_PATH=/p' \
  -e '/^HOST_UPDATER_STATE_DIR=/p' \
  -e '/^HOST_UPDATER_RUNTIME_DIR=/p' \
  -e '/^HOST_UPDATER_LOCK_PATH=/p' \
  -e '/^HOST_UPDATER_SYSTEMD_RUNTIME_DIR=/p' \
  -e '/^HOST_UPDATER_RESTORE_PENDING=/p' \
  -e '/^HOST_UPDATER_REMOVAL_STARTED=/p' \
  -e '/^HOST_UPDATER_ORIGINAL_ACTIVE=/p' \
  -e '/^HOST_UPDATER_ORIGINAL_UNIT_FILE_STATE=/p' \
  -e '/^HOST_UPDATER_BINARY_PATHS=(/,/^)/p' \
  -e '/^HOST_UPDATER_SELF_UPDATE_SUFFIXES=(/,/^)/p' \
  -e '/^canonicalize_existing_dir()/,/^}/p' \
  -e '/^host_updater_binary_artifact_paths()/,/^}/p' \
  -e '/^host_updater_binary_artifacts_present()/,/^}/p' \
  -e '/^remove_host_updater_binary_artifacts_with()/,/^}/p' \
  -e '/^parse_host_updater_install_root()/,/^}/p' \
  -e '/^read_host_updater_install_root_with()/,/^}/p' \
  -e '/^verify_host_updater_ownership_with()/,/^}/p' \
  -e '/^host_updater_staging_artifacts_present()/,/^}/p' \
  -e '/^host_updater_artifacts_present()/,/^}/p' \
  -e '/^validate_host_updater_process_lock_prerequisites_with()/,/^}/p' \
  -e '/^verify_host_updater_process_lock_free_with()/,/^}/p' \
  -e '/^restore_host_updater_service_if_needed()/,/^}/p' \
  -e '/^prepare_host_updater_removal()/,/^}/p' \
  "$UNINSTALL_SCRIPT" > "$TEST_TMP/uninstall-updater-functions.sh"
# shellcheck source=/dev/null
source "$TEST_TMP/uninstall-updater-functions.sh"

FAILURES=0

fail() {
  echo "FAIL: $*" >&2
  FAILURES=$((FAILURES + 1))
}

pass() {
  echo "ok: $*"
}

LAST_ERROR=""
print_error() {
  LAST_ERROR="${LAST_ERROR}${LAST_ERROR:+$'\n'}$*"
}

if [[ "${#HOST_UPDATER_BINARY_PATHS[@]}" -eq 2 ]] \
  && [[ "${HOST_UPDATER_BINARY_PATHS[0]}" == /usr/local/libexec/proto-fleet/proto-fleet-updater ]] \
  && [[ "${HOST_UPDATER_BINARY_PATHS[1]}" == /usr/local/libexec/proto-fleet-updater ]]; then
  pass "canonical and legacy updater binary paths are covered"
else
  fail "updater binary path contract is incomplete"
fi

if [[ "${HOST_UPDATER_SELF_UPDATE_SUFFIXES[*]}" == ".previous .candidate .handoff .handoff.tmp .restore" ]]; then
  pass "all self-update sibling suffixes are covered"
else
  fail "self-update sibling suffix contract is incomplete"
fi

owned_root="$TEST_TMP/owned root with a \"quote\" and \\slash"
other_root="$TEST_TMP/other-root"
mkdir -p "$owned_root" "$other_root"
HOST_UPDATER_ENV_PATH="$TEST_TMP/updater.env"
INSTALL_ROOT="$owned_root"
escaped_owned_root=${owned_root//\\/\\\\}
escaped_owned_root=${escaped_owned_root//\"/\\\"}
cat > "$HOST_UPDATER_ENV_PATH" <<EOF
PROTO_FLEET_INSTALL_ROOT="$escaped_owned_root"
PROTO_FLEET_DOWNLOAD_BASE_URL="https://github.com/block/proto-fleet/releases/download"
PROTO_FLEET_UPDATER_SOCKET_PATH="/run/proto-fleet-updater/updater.sock"
EOF

if [[ "$(read_host_updater_install_root_with)" == "$owned_root" ]] \
  && verify_host_updater_ownership_with; then
  pass "updater ownership accepts the selected canonical install root"
else
  fail "updater ownership rejected its selected install root"
fi

INSTALL_ROOT="$other_root"
LAST_ERROR=""
if verify_host_updater_ownership_with; then
  fail "updater ownership accepted a different selected install root"
elif [[ "$LAST_ERROR" == *"belongs to a different Proto Fleet installation"* ]]; then
  pass "updater ownership rejects a different selected install root"
else
  fail "updater ownership mismatch did not produce an actionable error"
fi

INSTALL_ROOT="$owned_root"
for malformed_environment in \
  'PROTO_FLEET_INSTALL_ROOT=/tmp/unquoted' \
  'PROTO_FLEET_INSTALL_ROOT="/tmp/bad\qescape"' \
  $'PROTO_FLEET_INSTALL_ROOT="/tmp/first"\nPROTO_FLEET_INSTALL_ROOT="/tmp/second"' \
  'PROTO_FLEET_DOWNLOAD_BASE_URL="https://example.invalid"'; do
  printf '%s\n' "$malformed_environment" > "$HOST_UPDATER_ENV_PATH"
  LAST_ERROR=""
  if verify_host_updater_ownership_with; then
    fail "updater ownership accepted malformed or ownerless configuration"
  elif [[ "$LAST_ERROR" == *"Could not read a valid updater install root"* ]]; then
    pass "updater ownership fails closed on malformed or ownerless configuration"
  else
    fail "malformed updater ownership did not produce an actionable error"
  fi
done

# Restore a valid ownership file before exercising the unrelated binary
# cleanup helpers below.
printf 'PROTO_FLEET_INSTALL_ROOT="%s"\n' "$escaped_owned_root" \
  > "$HOST_UPDATER_ENV_PATH"

service_mutation_log="$TEST_TMP/service-mutations"
if (
  INSTALL_ROOT="$other_root"
  HOST_UPDATER_PRESENT=false
  HOST_UPDATER_PRIVILEGE=()
  id() { printf '0\n'; }
  systemctl() { printf '%s\n' "$*" >> "$service_mutation_log"; }
  if prepare_host_updater_removal; then
    exit 1
  fi
  [[ ! -e "$service_mutation_log" ]]
); then
  pass "ownership mismatch aborts before updater service mutation"
else
  fail "ownership mismatch reached updater service mutation"
fi

if (
  INSTALL_ROOT="$owned_root"
  HOST_UPDATER_PRESENT=false
  HOST_UPDATER_PRIVILEGE=()
  HOST_UPDATER_STATE_DIR="$TEST_TMP/missing-flock-state"
  HOST_UPDATER_RUNTIME_DIR="$TEST_TMP/missing-flock-runtime"
  HOST_UPDATER_LOCK_PATH="$HOST_UPDATER_STATE_DIR/updater.lock"
  HOST_UPDATER_SYSTEMD_RUNTIME_DIR="$TEST_TMP/missing-flock-systemd"
  missing_flock_log="$TEST_TMP/missing-flock-service.log"
  mkdir -p "$HOST_UPDATER_STATE_DIR" "$HOST_UPDATER_SYSTEMD_RUNTIME_DIR" "$TEST_TMP/no-tools"
  : > "$HOST_UPDATER_LOCK_PATH"
  id() { printf '0\n'; }
  cat() { /bin/cat "$@"; }
  systemctl() { printf '%s\n' "$*" >> "$missing_flock_log"; }
  PATH="$TEST_TMP/no-tools"
  LAST_ERROR=""
  ! prepare_host_updater_removal \
    && [[ "$LAST_ERROR" == *"flock is unavailable"* ]] \
    && [[ ! -e "$missing_flock_log" ]]
); then
  pass "missing flock aborts before updater service mutation"
else
  fail "missing flock left the updater service changed"
fi

if (
  INSTALL_ROOT="$owned_root"
  HOST_UPDATER_PRESENT=false
  HOST_UPDATER_PRIVILEGE=()
  HOST_UPDATER_STATE_DIR="$TEST_TMP/not-found-active-state"
  HOST_UPDATER_RUNTIME_DIR="$TEST_TMP/not-found-active-runtime"
  HOST_UPDATER_LOCK_PATH="$HOST_UPDATER_STATE_DIR/updater.lock"
  HOST_UPDATER_SYSTEMD_RUNTIME_DIR="$TEST_TMP/systemd-runtime"
  mkdir -p "$HOST_UPDATER_STATE_DIR" "$HOST_UPDATER_SYSTEMD_RUNTIME_DIR"
  : > "$HOST_UPDATER_LOCK_PATH"
  service_state=active
  lifecycle_log="$TEST_TMP/not-found-active.log"
  id() { printf '0\n'; }
  systemctl() {
    printf 'systemctl %s\n' "$*" >> "$lifecycle_log"
    case "$*" in
      *'--property=LoadState --value'*) printf 'not-found\n' ;;
      *'--property=ActiveState --value'*) printf '%s\n' "$service_state" ;;
      'stop proto-fleet-updater.service') service_state=inactive ;;
      *) return 1 ;;
    esac
  }
  flock() {
    printf 'flock %s\n' "$*" >> "$lifecycle_log"
    return 0
  }
  prepare_host_updater_removal \
    && grep -q '^systemctl stop proto-fleet-updater.service$' "$lifecycle_log" \
    && ! grep -q '^systemctl disable ' "$lifecycle_log" \
    && grep -q "^flock -n $HOST_UPDATER_LOCK_PATH true$" "$lifecycle_log" \
    && [ "$service_state" = inactive ]
); then
  pass "not-found updater unit still stops its active process and verifies the lifetime lock"
else
  fail "not-found updater unit was treated as proof that its process stopped"
fi

if (
  INSTALL_ROOT="$owned_root"
  HOST_UPDATER_PRESENT=false
  HOST_UPDATER_PRIVILEGE=()
  HOST_UPDATER_STATE_DIR="$TEST_TMP/held-lock-state"
  HOST_UPDATER_RUNTIME_DIR="$TEST_TMP/held-lock-runtime"
  HOST_UPDATER_LOCK_PATH="$HOST_UPDATER_STATE_DIR/updater.lock"
  HOST_UPDATER_SYSTEMD_RUNTIME_DIR="$TEST_TMP/held-lock-systemd"
  mkdir -p "$HOST_UPDATER_STATE_DIR" "$HOST_UPDATER_SYSTEMD_RUNTIME_DIR"
  : > "$HOST_UPDATER_LOCK_PATH"
  id() { printf '0\n'; }
  systemctl() {
    case "$*" in
      *'--property=LoadState --value'*) printf 'not-found\n' ;;
      *'--property=ActiveState --value'*) printf 'inactive\n' ;;
      *) return 1 ;;
    esac
  }
  flock() { return 1; }
  LAST_ERROR=""
  ! prepare_host_updater_removal \
    && [[ "$LAST_ERROR" == *"still holds its lifetime lock"* ]]
); then
  pass "uninstaller fails closed while any updater process holds the lifetime lock"
else
  fail "uninstaller can remove state while the updater lifetime lock is held"
fi

if (
  INSTALL_ROOT="$owned_root"
  HOST_UPDATER_PRESENT=false
  HOST_UPDATER_PRIVILEGE=()
  HOST_UPDATER_RESTORE_PENDING=false
  HOST_UPDATER_REMOVAL_STARTED=false
  HOST_UPDATER_STATE_DIR="$TEST_TMP/restore-state"
  HOST_UPDATER_RUNTIME_DIR="$TEST_TMP/restore-runtime"
  HOST_UPDATER_LOCK_PATH="$HOST_UPDATER_STATE_DIR/updater.lock"
  HOST_UPDATER_SYSTEMD_RUNTIME_DIR="$TEST_TMP/restore-systemd"
  mkdir -p "$HOST_UPDATER_STATE_DIR" "$HOST_UPDATER_SYSTEMD_RUNTIME_DIR"
  : > "$HOST_UPDATER_LOCK_PATH"
  service_state=active
  mock_unit_file_state=enabled
  lifecycle_log="$TEST_TMP/restore-service.log"
  id() { printf '0\n'; }
  systemctl() {
    printf 'systemctl %s\n' "$*" >> "$lifecycle_log"
    case "$*" in
      *'--property=LoadState --value'*) printf 'loaded\n' ;;
      *'--property=ActiveState --value'*) printf '%s\n' "$service_state" ;;
      *'--property=UnitFileState --value'*) printf '%s\n' "$mock_unit_file_state" ;;
      'stop proto-fleet-updater.service') service_state=inactive ;;
      'disable proto-fleet-updater.service') mock_unit_file_state=disabled ;;
      'enable proto-fleet-updater.service') mock_unit_file_state=enabled ;;
      'start proto-fleet-updater.service') service_state=active ;;
      *) return 1 ;;
    esac
  }
  flock() { return 1; }
  ! prepare_host_updater_removal \
    && [[ "$HOST_UPDATER_RESTORE_PENDING" == true ]] \
    && restore_host_updater_service_if_needed \
    && [[ "$service_state" == active ]] \
    && [[ "$mock_unit_file_state" == enabled ]] \
    && grep -q '^systemctl enable proto-fleet-updater.service$' "$lifecycle_log" \
    && grep -q '^systemctl start proto-fleet-updater.service$' "$lifecycle_log"
); then
  pass "failed lock verification restores the updater's active and enabled state"
else
  fail "failed lock verification stranded the updater disabled"
fi

canonical="$TEST_TMP/canonical/proto-fleet-updater"
legacy="$TEST_TMP/legacy/proto-fleet-updater"
mkdir -p "$(dirname "$canonical")" "$(dirname "$legacy")"
HOST_UPDATER_BINARY_PATHS=("$canonical" "$legacy")

# A sibling by itself must make the updater discoverable so uninstall does not
# skip privileged cleanup merely because the active binary is already gone.
: > "${legacy}.restore"
if host_updater_binary_artifacts_present; then
  pass "orphaned self-update sibling is detected"
else
  fail "orphaned self-update sibling was not detected"
fi
rm -f "${legacy}.restore"

# Populate every exact artifact, including a dangling symlink, and nearby
# lookalikes that are outside the updater's closed naming contract.
while IFS= read -r path; do
  if [[ "$path" == *.handoff.tmp ]]; then
    ln -s "$TEST_TMP/missing-target" "$path"
  else
    : > "$path"
  fi
done < <(host_updater_binary_artifact_paths)
canonical_lookalike="${canonical}.previous.keep"
legacy_lookalike="${legacy}.candidate-extra"
: > "$canonical_lookalike"
: > "$legacy_lookalike"

if remove_host_updater_binary_artifacts_with \
  && ! host_updater_binary_artifacts_present \
  && [[ -f "$canonical_lookalike" ]] \
  && [[ -f "$legacy_lookalike" ]]; then
  pass "exact updater artifacts are removed and lookalikes are preserved"
else
  fail "updater cleanup removed the wrong paths or left an exact artifact"
fi

state_remove_line=$(awk '
  /^remove_host_updater\(\)/ { in_remove=1 }
  in_remove && /"\$HOST_UPDATER_STATE_DIR"/ { print NR; exit }
' "$UNINSTALL_SCRIPT")
staging_verify_line=$(awk '
  /^remove_host_updater\(\)/ { in_remove=1 }
  in_remove && /if host_updater_staging_artifacts_present; then/ { print NR; exit }
' "$UNINSTALL_SCRIPT")
daemon_reload_line=$(awk '
  /^remove_host_updater\(\)/ { in_remove=1 }
  in_remove && /systemctl daemon-reload/ { print NR; exit }
' "$UNINSTALL_SCRIPT")
env_remove_line=$(awk '
  /^remove_host_updater\(\)/ { in_remove=1 }
  in_remove && /rm -f -- "\$HOST_UPDATER_ENV_PATH"/ { print NR; exit }
' "$UNINSTALL_SCRIPT")
if [[ -n "$state_remove_line" && -n "$staging_verify_line" \
    && -n "$daemon_reload_line" && -n "$env_remove_line" ]] \
  && [[ "$state_remove_line" -lt "$staging_verify_line" ]] \
  && [[ "$staging_verify_line" -lt "$daemon_reload_line" ]] \
  && [[ "$daemon_reload_line" -lt "$env_remove_line" ]]; then
  pass "updater ownership proof is retained until state and service cleanup succeed"
else
  fail "updater cleanup can erase its ownership proof before a retry is safe"
fi

if [[ "$FAILURES" -ne 0 ]]; then
  echo "$FAILURES failure(s)"
  exit 1
fi
echo "all uninstall updater cleanup checks passed"
