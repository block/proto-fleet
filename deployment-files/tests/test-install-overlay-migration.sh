#!/usr/bin/env bash
# Focused tests for the installer's legacy optional-overlay migration. Docker
# is stubbed so these cases exercise discovery, target validation, and label
# parsing without touching the host daemon.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INSTALL_SCRIPT="$REPO_ROOT/deployment-files/install.sh"
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
TEST_TMP_PREFIX="${TEST_TMP_PARENT%/}/proto-fleet-install-overlay"
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

# install.sh is intentionally standalone because it is executed directly from
# curl before a release bundle exists. Extract its testable helper definitions
# instead of adding a production-only source-mode escape hatch.
sed '/^# END INSTALLER TESTABLE HELPERS$/q' \
  "$INSTALL_SCRIPT" > "$TEST_TMP/install-functions.sh"
# shellcheck source=/dev/null
source "$TEST_TMP/install-functions.sh"

FAILURES=0

fail() {
  echo "FAIL: $*" >&2
  FAILURES=$((FAILURES + 1))
}

pass() {
  echo "ok: $*"
}

make_install() {
  local root="$1"
  mkdir -p "$root/deployment/server/start"
  : > "$root/deployment/docker-compose.yaml"
  : > "$root/deployment/docker-compose.alerts.yaml"
  : > "$root/deployment/docker-compose.system-monitoring.yaml"
  : > "$root/deployment/docker-compose.tracing.yaml"
}

# Defaults consumed by the docker test double. Individual cases override only
# the state they need.
FAKE_IDS="fleet-container"
FAKE_DIRECT_IDS=""
FAKE_SUDO_IDS=""
FAKE_MOUNT_PATH=""
FAKE_CONFIG_FILES=""
FAKE_INSPECT_FAILURE=0
FAKE_UID=1000
FAKE_SELECTED_DOCKER_ID="selected-daemon"
FAKE_SERVICE_DOCKER_ID="service-daemon"
DOCKER_CALL_LOG="$TEST_TMP/docker-calls"

docker() {
  local ids="$FAKE_IDS"
  case "${FAKE_DOCKER_CONTEXT:-direct}" in
    direct) [ -z "$FAKE_DIRECT_IDS" ] || ids="$FAKE_DIRECT_IDS" ;;
    sudo) [ -z "$FAKE_SUDO_IDS" ] || ids="$FAKE_SUDO_IDS" ;;
  esac
  printf '%s docker %s\n' "${FAKE_DOCKER_CONTEXT:-direct}" "$*" >> "$DOCKER_CALL_LOG"
  case "${1:-}" in
    ps)
      printf '%s\n' "$ids"
      ;;
    inspect)
      [ "$FAKE_INSPECT_FAILURE" != 1 ] || return 1
      case "${3:-}" in
        *com.docker.compose.service*) printf 'fleet-api\n' ;;
        *com.docker.compose.project.config_files*) printf '%s\n' "$FAKE_CONFIG_FILES" ;;
        *'.Destination "/var/lib/fleet/start"'*) printf '%s\n' "$FAKE_MOUNT_PATH" ;;
        *) return 1 ;;
      esac
      ;;
    info)
      if [ "${DOCKER_HOST:-}" = "$UPDATER_DOCKER_HOST" ] \
        && [ -z "${DOCKER_CONTEXT+x}${DOCKER_CONFIG+x}${DOCKER_TLS+x}${DOCKER_TLS_VERIFY+x}${DOCKER_CERT_PATH+x}" ]; then
        printf '%s\n' "$FAKE_SERVICE_DOCKER_ID"
      else
        printf '%s\n' "$FAKE_SELECTED_DOCKER_ID"
      fi
      ;;
    version) return 0 ;;
    *) return 1 ;;
  esac
}

sudo() {
  printf 'sudo %s\n' "$*" >> "$DOCKER_CALL_LOG"
  [ "${1:-}" != "-n" ] || shift
  FAKE_DOCKER_CONTEXT=sudo "$@"
}

id() {
  if [ "${1:-}" = "-u" ]; then
    printf '%s\n' "$FAKE_UID"
    return 0
  fi
  command id "$@"
}

# Explicit settings are authoritative, including false, and avoid Docker
# inspection entirely.
if (
  root="$TEST_TMP/explicit"
  make_install "$root"
  printf '%s\n' \
    'ENABLE_BETA_ALERTS=true' \
    'export ENABLE_BETA_ALERTS: "false" # says "disabled"; last value wins' \
    'ENABLE_SYSTEM_MONITORING: false' \
    $'ENABLE_TRACING: TRUE # explicit\r' > "$root/deployment/.env"
  : > "$DOCKER_CALL_LOG"
  capture_previous_run_options "$root" 1 || exit 1
  [ "$PREVIOUS_BETA_ALERTS" = 0 ] \
    && [ "$PREVIOUS_SYSTEM_MONITORING" = 0 ] \
    && [ "$PREVIOUS_TRACING" = 0 ] \
    && [ ! -s "$DOCKER_CALL_LOG" ]
); then
  pass "complete explicit ENABLE_* state bypasses inference"
else
  fail "complete explicit ENABLE_* state should remain authoritative"
fi

# Only absent keys are inferred. Active alerts/system overlays must not
# override explicit false values while a missing tracing key is migrated.
if (
  root="$TEST_TMP/partial"
  make_install "$root"
  printf '%s\n' \
    'ENABLE_BETA_ALERTS: false' \
    'ENABLE_SYSTEM_MONITORING=false' > "$root/deployment/.env"
  : > "$DOCKER_CALL_LOG"
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml,$root/deployment/docker-compose.alerts.yaml,$root/deployment/docker-compose.system-monitoring.yaml,$root/deployment/docker-compose.tracing.yaml"
  capture_previous_run_options "$root" 1 || exit 1
  [ "$PREVIOUS_BETA_ALERTS" = 0 ] \
    && [ "$PREVIOUS_SYSTEM_MONITORING" = 0 ] \
    && [ "$PREVIOUS_TRACING" = 1 ]
); then
  pass "inference fills only missing overlay settings"
else
  fail "inference overrode explicit false or missed active tracing"
fi

# A preseeded .env alone is not evidence of a legacy deployment. Fresh
# automation may write configuration before install, and must not require an
# old container merely because optional overlay keys are absent.
if (
  root="$TEST_TMP/fresh-preseeded"
  mkdir -p "$root/deployment"
  printf 'DB_PASSWORD=preseeded\n' > "$root/deployment/.env"
  : > "$DOCKER_CALL_LOG"
  capture_previous_run_options "$root" 0 || exit 1
  [ "$PREVIOUS_BETA_ALERTS" = 0 ] \
    && [ "$PREVIOUS_SYSTEM_MONITORING" = 0 ] \
    && [ "$PREVIOUS_TRACING" = 0 ] \
    && [ ! -s "$DOCKER_CALL_LOG" ]
); then
  pass "fresh preseeded env does not trigger legacy inference"
else
  fail "fresh preseeded env should retain disabled overlay defaults"
fi

# A base-only config label is a complete, trustworthy signal that all missing
# optional overlays were disabled.
if (
  root="$TEST_TMP/base-only"
  make_install "$root"
  : > "$root/deployment/.env"
  : > "$DOCKER_CALL_LOG"
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml"
  capture_previous_run_options "$root" 1 || exit 1
  [ "$PREVIOUS_BETA_ALERTS" = 0 ] \
    && [ "$PREVIOUS_SYSTEM_MONITORING" = 0 ] \
    && [ "$PREVIOUS_TRACING" = 0 ]
); then
  pass "base-only Compose label infers disabled overlays"
else
  fail "base-only Compose label should infer all overlays disabled"
fi

# The documented legacy CLI path can enable every overlay without persisting
# any ENABLE_* keys. Recover all three selections from the old Compose model.
if (
  root="$TEST_TMP/all-overlays"
  make_install "$root"
  : > "$root/deployment/.env"
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml,$root/deployment/docker-compose.alerts.yaml,$root/deployment/docker-compose.system-monitoring.yaml,$root/deployment/docker-compose.tracing.yaml"
  capture_previous_run_options "$root" 1 || exit 1
  [ "$PREVIOUS_BETA_ALERTS" = 1 ] \
    && [ "$PREVIOUS_SYSTEM_MONITORING" = 1 ] \
    && [ "$PREVIOUS_TRACING" = 1 ]
); then
  pass "all legacy overlays are recovered from the Compose model"
else
  fail "active legacy overlays should all be migrated"
fi

# Discovery is label-based and tied to the selected install's recorded base
# Compose file rather than a substring container-name match.
if (
  root="$TEST_TMP/labeled"
  make_install "$root"
  : > "$root/deployment/.env"
  : > "$DOCKER_CALL_LOG"
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml,$root/deployment/docker-compose.alerts.yaml"
  capture_previous_run_options "$root" 1 || exit 1
  grep -q -- '--filter label=com.docker.compose.service=fleet-api' "$DOCKER_CALL_LOG" \
    && ! grep -q -- '--filter name=' "$DOCKER_CALL_LOG"
); then
  pass "container discovery uses exact Compose service labels"
else
  fail "container discovery used a loose name match"
fi

# An existing deployment with missing settings must fail closed if the
# container disappeared; the installer calls this before extracting files.
if (
  root="$TEST_TMP/no-container"
  make_install "$root"
  : > "$root/deployment/.env"
  FAKE_IDS=""
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml"
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/no-container.err" \
    && grep -q 'No existing fleet-api container matches' "$TEST_TMP/no-container.err"
); then
  pass "missing legacy source fails closed"
else
  fail "missing legacy source should abort migration"
fi

# A transient Docker inspection error is not evidence that overlays were off.
# Stop before extraction and let the operator retry instead of persisting false.
if (
  root="$TEST_TMP/inspect-failure"
  make_install "$root"
  : > "$root/deployment/.env"
  FAKE_INSPECT_FAILURE=1
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/inspect-failure.err" \
    && grep -q 'Could not inspect Docker' "$TEST_TMP/inspect-failure.err"
); then
  pass "Docker inspection failures fail closed"
else
  fail "Docker inspection failure should abort migration"
fi

# Multiple containers recording the same selected root are ambiguous; do not
# let Docker result ordering choose one.
if (
  root="$TEST_TMP/ambiguous"
  make_install "$root"
  : > "$root/deployment/.env"
  FAKE_IDS=$'first\nsecond'
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml"
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/ambiguous.err" \
    && grep -q 'Multiple fleet-api containers match' "$TEST_TMP/ambiguous.err"
); then
  pass "ambiguous matching containers fail closed"
else
  fail "ambiguous matching containers should abort migration"
fi

# The config-files label must include the selected deployment's base file;
# otherwise a stale or unrelated container cannot authorize migration.
if (
  root="$TEST_TMP/wrong-label"
  make_install "$root"
  : > "$root/deployment/.env"
  FAKE_IDS="fleet-container"
  FAKE_CONFIG_FILES="/other/deployment/docker-compose.yaml,/other/deployment/docker-compose.alerts.yaml"
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/wrong-label.err" \
    && grep -q 'No existing fleet-api container matches' "$TEST_TMP/wrong-label.err"
); then
  pass "foreign Compose config-files label is rejected"
else
  fail "foreign Compose config-files label should abort migration"
fi

# Invalid explicit settings are neither preserved as valid state nor silently
# overwritten with an inferred value.
if (
  root="$TEST_TMP/invalid-env"
  make_install "$root"
  printf '%s\n' \
    'ENABLE_BETA_ALERTS=maybe' \
    'ENABLE_SYSTEM_MONITORING=false' \
    'ENABLE_TRACING=false' > "$root/deployment/.env"
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/invalid-env.err" \
    && grep -q 'must be true or false' "$TEST_TMP/invalid-env.err"
); then
  pass "invalid explicit overlay value is rejected"
else
  fail "invalid explicit overlay value should abort migration"
fi

# Preserve explicit false rather than silently enabling its dependency when
# the old label and persisted state disagree.
if (
  root="$TEST_TMP/inconsistent"
  make_install "$root"
  printf '%s\n' \
    'ENABLE_BETA_ALERTS=false' \
    'ENABLE_TRACING=false' > "$root/deployment/.env"
  FAKE_IDS="fleet-container"
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml,$root/deployment/docker-compose.alerts.yaml,$root/deployment/docker-compose.system-monitoring.yaml"
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/inconsistent.err" \
    && grep -q 'system monitoring without beta alerts' "$TEST_TMP/inconsistent.err"
); then
  pass "inconsistent explicit and inferred dependencies fail closed"
else
  fail "inconsistent explicit and inferred dependencies should abort migration"
fi

# The caller can reuse the privilege context that discovered the deployment;
# every migration probe must stay on that same Docker daemon.
if (
  root="$TEST_TMP/privileged"
  make_install "$root"
  : > "$root/deployment/.env"
  : > "$DOCKER_CALL_LOG"
  FAKE_CONFIG_FILES="$root/deployment/docker-compose.yaml"
  capture_previous_run_options "$root" 1 sudo -n || exit 1
  grep -q '^sudo -n docker ps -a ' "$DOCKER_CALL_LOG" \
    && grep -q '^sudo -n docker inspect ' "$DOCKER_CALL_LOG" \
    && grep -q '^sudo docker ps -a ' "$DOCKER_CALL_LOG" \
    && grep -q '^sudo docker inspect ' "$DOCKER_CALL_LOG" \
    && ! grep -q '^direct docker ' "$DOCKER_CALL_LOG"
); then
  pass "overlay migration reuses the detected Docker privilege"
else
  fail "overlay migration did not use the selected Docker privilege"
fi

# --install-dir chooses a destination but must not suppress root-daemon
# discovery. The caller needs this state before extraction so it can reject a
# mixed-privilege upgrade even when every legacy overlay value is explicit.
if (
  root="$TEST_TMP/explicit-root-daemon"
  make_install "$root"
  : > "$DOCKER_CALL_LOG"
  FAKE_IDS=""
  FAKE_DIRECT_IDS=""
  FAKE_SUDO_IDS="root-fleet"
  FAKE_MOUNT_PATH="$root/deployment/server/start"
  detect_previous_install \
    && [ "$PREVIOUS_INSTALL_DIR" = "$root" ] \
    && [ "$PREVIOUS_INSTALL_NEEDS_SUDO" = 1 ] \
    && grep -q '^sudo docker ps -a ' "$DOCKER_CALL_LOG"
); then
  pass "explicit install paths retain root-daemon ownership discovery"
else
  fail "explicit install path discovery missed the root-managed deployment"
fi

# The final selected path must remain tied to the Docker-discovered install.
# Canonical aliases are accepted, but a typo or attempted relocation fails
# before any extraction can target the shared default Compose project.
if (
  root="$TEST_TMP/path-alias"
  mkdir -p "$root/existing"
  ln -s "$root/existing" "$root/alias"
  resolved=$(resolve_selected_install_path "$root/alias/" "$root/existing") \
    && [ "$resolved" = "$(cd "$root/existing" && pwd -P)" ]
); then
  pass "canonical aliases of the discovered install are accepted"
else
  fail "canonical install path comparison rejected an equivalent alias"
fi

if (
  root="$TEST_TMP/path-mismatch"
  mkdir -p "$root/existing" "$root/other"
  ! resolve_selected_install_path "$root/other" "$root/existing" \
    > /dev/null 2> "$TEST_TMP/path-mismatch.err" \
    && grep -q 'Relocation is not supported' "$TEST_TMP/path-mismatch.err" \
    && ! resolve_selected_install_path "$root/typo" "$root/existing" \
      > /dev/null 2> "$TEST_TMP/path-typo.err" \
    && grep -q 'does not resolve to that installation' "$TEST_TMP/path-typo.err"
); then
  pass "relocation and path typos are rejected when an install was discovered"
else
  fail "mismatched install path should fail closed"
fi

if (
  root="$TEST_TMP/fresh-path"
  mkdir -p "$root"
  FAKE_UID=$(command id -u)
  resolved=$(resolve_selected_install_path "$root/not-created/child" "") \
    && [ "$resolved" = "$(cd "$root/not-created/child" && pwd -P)" ] \
    && [ -d "$root/not-created/child" ] \
    && [ ! -e "$root/not-created/child/deployment" ]
); then
  pass "fresh install paths are created privately and canonicalized"
else
  fail "fresh path resolution did not establish a trusted destination"
fi

if (
  FAKE_UID=$(command id -u)
  ! resolve_selected_install_path / "" > /dev/null 2> "$TEST_TMP/path-root.err" \
    && grep -q 'dangerous installation root' "$TEST_TMP/path-root.err" \
    && ! resolve_selected_install_path /etc/proto-fleet-review-test "" \
      > /dev/null 2> "$TEST_TMP/path-etc.err" \
    && grep -q 'protected system path' "$TEST_TMP/path-etc.err"
); then
  pass "fresh installs reject dangerous roots and protected system paths"
else
  fail "fresh install path validation accepted a dangerous system target"
fi

if (
  root="$TEST_TMP/fresh-symlink"
  target="$TEST_TMP/fresh-symlink-target"
  mkdir -p "$target"
  printf 'unchanged\n' > "$target/sentinel"
  ln -s "$target" "$root"
  FAKE_UID=$(command id -u)
  ! resolve_selected_install_path "$root" "" \
    > /dev/null 2> "$TEST_TMP/path-symlink.err" \
    && grep -q 'must not be a symlink' "$TEST_TMP/path-symlink.err" \
    && [ "$(cat "$target/sentinel")" = unchanged ] \
    && [ ! -e "$target/deployment" ]
); then
  pass "fresh installs reject a symlinked destination without touching its target"
else
  fail "fresh install path validation followed a symlinked destination"
fi

if (
  link="$TEST_TMP/protected-ancestor-link"
  protected_target="/etc/ssl/proto-fleet-review-$$"
  [ -d /etc/ssl ]
  ln -s /etc "$link"
  FAKE_UID=$(command id -u)
  ! resolve_selected_install_path "$link/ssl/proto-fleet-review-$$" "" \
    > /dev/null 2> "$TEST_TMP/path-protected-alias.err" \
    && grep -q 'protected system path' "$TEST_TMP/path-protected-alias.err" \
    && [ ! -e "$protected_target" ]
); then
  pass "fresh installs reject protected paths reached through an ancestor symlink"
else
  fail "fresh install path validation wrote through a protected ancestor alias"
fi

if (
  root="$TEST_TMP/root-direct-foreign-owner"
  mkdir -p "$root"
  FAKE_UID=0
  unset SUDO_UID
  ! resolve_selected_install_path "$root" "" \
    > /dev/null 2> "$TEST_TMP/path-owner.err" \
    && grep -q 'owned by unrelated UID' "$TEST_TMP/path-owner.err"
); then
  pass "direct root installs reject paths owned by another user"
else
  fail "direct root install trusted an unrelated path owner"
fi

if (
  root="$TEST_TMP/sudo-admin-owner/new-install"
  FAKE_UID=0
  SUDO_UID=$(command id -u)
  resolved=$(resolve_selected_install_path "$root" "") \
    && [ "$resolved" = "$(cd "$root" && pwd -P)" ]
); then
  pass "sudo installs trust only the invoking administrator UID"
else
  fail "sudo install rejected the invoking administrator's path"
fi

if (
  root="$TEST_TMP/unrecognized-deployment"
  mkdir -p "$root/deployment"
  FAKE_UID=$(command id -u)
  ! resolve_selected_install_path "$root" "" \
    > /dev/null 2> "$TEST_TMP/path-existing-deployment.err" \
    && grep -q 'purported fresh install already contains' \
      "$TEST_TMP/path-existing-deployment.err"
); then
  pass "fresh installs reject an unrecognized existing deployment tree"
else
  fail "fresh install path validation accepted an existing deployment tree"
fi

if ! validate_install_path_metadata /srv/foreign 2000 755 1000 1 \
    > /dev/null 2>&1 \
  && validate_install_path_metadata /tmp 0 1777 1000 0 \
  && ! validate_install_path_metadata /tmp 0 1777 1000 1 \
    > /dev/null 2>&1; then
  pass "path metadata permits only the trusted owner and sticky shared ancestors"
else
  fail "path metadata trust rules accepted an unrelated or writable install root"
fi

# If the root-daemon probe was blocked, an explicit on-disk deployment must
# not become writable merely because all persisted overlay values are already
# complete and no later migration lookup would touch Docker.
if (
  root="$TEST_TMP/sudo-blocked-existing"
  make_install "$root"
  printf '%s\n' \
    'ENABLE_BETA_ALERTS=false' \
    'ENABLE_SYSTEM_MONITORING=false' \
    'ENABLE_TRACING=false' > "$root/deployment/.env"
  FAKE_UID=1000
  ! guard_selected_install_when_sudo_blocked "$root" 1 v1.2.3 \
    > /dev/null 2> "$TEST_TMP/sudo-blocked-existing.err" \
    && grep -q 'Re-run the installer as root' "$TEST_TMP/sudo-blocked-existing.err"
); then
  pass "blocked root-daemon discovery rejects an existing selected deployment"
else
  fail "blocked root-daemon discovery must fail before replacing an existing deployment"
fi

if (
  root="$TEST_TMP/sudo-blocked-fresh"
  mkdir -p "$root"
  FAKE_UID=1000
  guard_selected_install_when_sudo_blocked "$root" 1 v1.2.3 \
    > /dev/null 2> "$TEST_TMP/sudo-blocked-fresh.err" \
    && grep -q "couldn't check whether a" "$TEST_TMP/sudo-blocked-fresh.err"
); then
  pass "blocked root-daemon discovery still permits a fresh selected path with warning"
else
  fail "fresh installs should remain available when the root daemon cannot be probed"
fi

# Keep the fail-closed check ahead of every operation that can inspect,
# disable, or replace the existing deployment.
guard_line=$(grep -n '^if ! guard_selected_install_when_sudo_blocked' "$INSTALL_SCRIPT" | cut -d: -f1)
capture_line=$(grep -n '^if ! capture_previous_run_options' "$INSTALL_SCRIPT" | cut -d: -f1)
extract_line=$(grep -n '^extract_and_cd ' "$INSTALL_SCRIPT" | cut -d: -f1)
if [ -n "$guard_line" ] && [ "$guard_line" -lt "$capture_line" ] && [ "$guard_line" -lt "$extract_line" ]; then
  pass "blocked root-daemon guard runs before migration and extraction"
else
  fail "blocked root-daemon guard must precede deployment mutation"
fi

# A root caller may carry Docker selectors that point at a custom or rootless
# daemon. The system-service probe must discard them and explicitly select the
# local rootful socket so neither inherited variables nor a persisted current
# context can redirect updater operations.
if (
  FAKE_UID=0
  export DOCKER_HOST='unix:///run/user/1000/docker.sock'
  export DOCKER_CONTEXT='rootless'
  export DOCKER_CONFIG="$TEST_TMP/docker-config"
  export DOCKER_TLS=1
  export DOCKER_TLS_VERIFY=1
  export DOCKER_CERT_PATH="$TEST_TMP/certs"
  current_id=$(docker info --format '{{.ID}}')
  service_id=$(service_docker_id_with)
  [ "$current_id" = "$FAKE_SELECTED_DOCKER_ID" ] \
    && [ "$service_id" = "$FAKE_SERVICE_DOCKER_ID" ] \
    && [ "$current_id" != "$service_id" ]
); then
  pass "system-service Docker probe pins the local rootful socket"
else
  fail "system-service Docker probe was not pinned to the local rootful socket"
fi

if grep -Fq 'Environment=DOCKER_HOST=unix:///var/run/docker.sock' \
    "$REPO_ROOT/deployment-files/updater/proto-fleet-updater.service" \
  && grep -Fq \
    'UnsetEnvironment=DOCKER_CONTEXT DOCKER_CONFIG DOCKER_TLS DOCKER_TLS_VERIFY DOCKER_CERT_PATH' \
    "$REPO_ROOT/deployment-files/updater/proto-fleet-updater.service" \
  && ! grep -Eq '^UnsetEnvironment=.*DOCKER_HOST' \
    "$REPO_ROOT/deployment-files/updater/proto-fleet-updater.service"; then
  pass "systemd unit pins the same local rootful Docker socket"
else
  fail "systemd unit does not pin the local rootful Docker socket"
fi

# The two files copied into privileged host locations are extracted directly
# from the verified archive into its private download tree. A mutable copy in
# the final deployment is never used as the bootstrap source.
if (
  payload="$TEST_TMP/bootstrap-payload"
  archive="$TEST_TMP/bootstrap.tar.gz"
  destination="$TEST_TMP/private-bootstrap"
  mkdir -p "$payload/deployment/updater"
  printf 'verified updater\n' > "$payload/deployment/updater/proto-fleet-updater"
  chmod +x "$payload/deployment/updater/proto-fleet-updater"
  printf 'verified unit\n' > "$payload/deployment/updater/proto-fleet-updater.service"
  tar -czf "$archive" -C "$payload" deployment
  extract_updater_bootstrap "$archive" "$destination" \
    && [ "$(cat "$destination/proto-fleet-updater")" = 'verified updater' ] \
    && [ "$(cat "$destination/proto-fleet-updater.service")" = 'verified unit' ] \
    && [ ! -L "$destination/proto-fleet-updater" ] \
    && [ ! -L "$destination/proto-fleet-updater.service" ]
); then
  pass "privileged updater inputs are staged from the verified archive"
else
  fail "verified updater bootstrap extraction failed"
fi

if [ "$(grep -c 'tar --no-same-owner -xzvf' "$INSTALL_SCRIPT")" -eq 2 ] \
  && grep -Fq 'install -m 0755 "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater"' "$INSTALL_SCRIPT" \
  && grep -Fq 'install -m 0644 "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater.service"' "$INSTALL_SCRIPT"; then
  pass "deployment extraction normalizes archive ownership and privileged copies use private sources"
else
  fail "installer still trusts archive ownership or mutable deployment bootstrap files"
fi

prepare_line=$(awk '/^if ! prepare_existing_updater_service; then$/ { print NR; exit }' "$INSTALL_SCRIPT")
extract_line=$(awk '/^extract_and_cd "\$TAR_PATH" "\$INSTALL_DIR"$/ { print NR; exit }' "$INSTALL_SCRIPT")
if [ -n "$prepare_line" ] && [ -n "$extract_line" ] && [ "$prepare_line" -lt "$extract_line" ]; then
  pass "existing updater is drained before deployment extraction"
else
  fail "deployment extraction can run before updater shutdown"
fi

# The replacement updater is validated first, then stopped while run-fleet
# owns the deployment tree, and restored only after that manual run returns.
# Arming the EXIT cleanup before the installer function returns covers signals
# and other failures during run-fleet as well as its ordinary nonzero result.
validation_restart_line=$(awk '/^  if ! restart_updater_service_with/ { print NR; exit }' "$INSTALL_SCRIPT")
quiesce_line=$(awk '/^  if ! quiesce_updater_service_for_manual_run_with/ { print NR; exit }' "$INSTALL_SCRIPT")
production_socket_line=$(awk '/^      "\$GITHUB_RELEASES_URL" "\$UPDATER_SOCKET_PATH"/ { print NR; exit }' "$INSTALL_SCRIPT")
restart_arm_line=$(awk '/^  UPDATER_RESTART_ON_EXIT=1$/ { print NR; exit }' "$INSTALL_SCRIPT")
run_fleet_line=$(awk '/^if \.\/run-fleet\.sh / { print NR; exit }' "$INSTALL_SCRIPT")
post_run_restart_line=$(awk -v start="$run_fleet_line" 'NR > start && /^  if restart_updater_service_with/ { print NR; exit }' "$INSTALL_SCRIPT")
if [ -n "$validation_restart_line" ] \
  && [ "$validation_restart_line" -lt "$quiesce_line" ] \
  && [ "$quiesce_line" -lt "$production_socket_line" ] \
  && [ "$production_socket_line" -lt "$restart_arm_line" ] \
  && [ "$quiesce_line" -lt "$restart_arm_line" ] \
  && [ "$restart_arm_line" -lt "$run_fleet_line" ] \
  && [ "$run_fleet_line" -lt "$post_run_restart_line" ]; then
  pass "host updater stays quiesced for the complete manual deployment run"
else
  fail "host updater lifecycle does not serialize the manual deployment run"
fi

if (
  lifecycle_log="$TEST_TMP/updater-lifecycle-calls"
  : > "$lifecycle_log"
  systemctl() {
    printf '%s\n' "$*" >> "$lifecycle_log"
    case "${1:-}" in
      restart|stop|is-active) return 0 ;;
      show) printf 'inactive\n' ;;
      *) return 1 ;;
    esac
  }
  curl() {
    printf 'curl %s\n' "$*" >> "$lifecycle_log"
    return 0
  }
  restart_updater_service_with /run/proto-fleet-updater/test-validation.sock \
    && quiesce_updater_service_for_manual_run_with \
    && [ "$(sed -n '1p' "$lifecycle_log")" = 'restart proto-fleet-updater.service' ] \
    && grep -q -- '--unix-socket /run/proto-fleet-updater/test-validation.sock' "$lifecycle_log" \
    && grep -q '^stop proto-fleet-updater.service$' "$lifecycle_log" \
    && grep -q '^show --property=ActiveState --value proto-fleet-updater.service$' "$lifecycle_log"
); then
  pass "validated updater is stopped and verified before manual deployment"
else
  fail "updater validation and quiesce helpers do not enforce the lifecycle boundary"
fi

if (
  updater_env="$TEST_TMP/updater.env"
  write_updater_environment_file "$updater_env" '/srv/proto fleet' \
    'https://github.com/block/proto-fleet/releases' \
    /run/proto-fleet-updater/installer-validation.sock \
    && grep -q 'PROTO_FLEET_INSTALL_ROOT="/srv/proto fleet"' "$updater_env" \
    && grep -q 'PROTO_FLEET_UPDATER_SOCKET_PATH="/run/proto-fleet-updater/installer-validation.sock"' "$updater_env" \
    && write_updater_environment_file "$updater_env" '/srv/proto fleet' \
      'https://github.com/block/proto-fleet/releases' \
      /run/proto-fleet-updater/updater.sock \
    && grep -q 'PROTO_FLEET_UPDATER_SOCKET_PATH="/run/proto-fleet-updater/updater.sock"' "$updater_env" \
    && ! grep -q 'installer-validation.sock' "$updater_env"
); then
  pass "updater validation uses a private socket name before production activation"
else
  fail "updater validation socket can leak into the production environment"
fi

if (
  restore_log="$TEST_TMP/updater-exit-restore"
  DOWNLOAD_DIR="$TEST_TMP/updater-exit-download"
  mkdir -p "$DOWNLOAD_DIR"
  UPDATER_DISABLE_ON_EXIT=0
  UPDATER_RESTART_ON_EXIT=1
  restart_updater_service_with() {
    printf '%s\n' "$1" > "$restore_log"
    return 0
  }
  (trap installer_exit_cleanup EXIT; exit 7)
  cleanup_status=$?
  [ "$cleanup_status" -eq 7 ] \
    && [ "$(cat "$restore_log")" = '/run/proto-fleet-updater/updater.sock' ] \
    && [ ! -e "$DOWNLOAD_DIR" ]
); then
  pass "interrupted manual deployment restores the production updater socket"
else
  fail "installer exit cleanup did not restore the updater after interruption"
fi

if (
  systemctl() {
    case "${1:-}" in
      stop) return 0 ;;
      show) printf 'active\n' ;;
      *) return 1 ;;
    esac
  }
  ! quiesce_updater_service_for_manual_run_with \
    2> "$TEST_TMP/updater-remained-active.err" \
    && grep -q 'remained active' "$TEST_TMP/updater-remained-active.err"
); then
  pass "manual deployment rejects an updater that did not stop"
else
  fail "manual deployment must not proceed while the updater remains active"
fi

# A systemctl/query failure must never be interpreted as proof that the
# privileged updater is inactive. Conversely, a genuinely absent unit and a
# verified inactive+disabled unit are safe terminal states.
systemctl() {
  case "$FAKE_SYSTEMCTL_MODE:$*" in
    missing:*--property=LoadState*) printf 'not-found\n' ;;
    safe:*--property=LoadState*) printf 'loaded\n' ;;
    safe:*'disable --now'*) return 0 ;;
    safe:*--property=ActiveState*) printf 'inactive\n' ;;
    safe:*--property=UnitFileState*) printf 'disabled\n' ;;
    unsafe:*--property=LoadState*) printf 'loaded\n' ;;
    unsafe:*'disable --now'*) return 1 ;;
    unsafe:*--property=ActiveState*) printf 'active\n' ;;
    unsafe:*--property=UnitFileState*) printf 'enabled\n' ;;
    query-failure:*) return 1 ;;
    *) return 1 ;;
  esac
}

if (
  UPDATER_CLEANUP_FAILED=0
  FAKE_SYSTEMCTL_MODE=missing
  disable_updater_service_with \
    && [ "$UPDATER_CLEANUP_FAILED" = 0 ]
); then
  pass "missing updater unit is already a safe fallback state"
else
  fail "missing updater unit should not make installer fallback fatal"
fi

if (
  UPDATER_CLEANUP_FAILED=0
  FAKE_SYSTEMCTL_MODE=missing
  # Model a non-root, non-interactive install where sudo -n would require a
  # password. The authoritative unprivileged LoadState result must make sudo
  # unnecessary when the unit is absent.
  sudo() { return 1; }
  disable_updater_service_with sudo -n \
    && [ "$UPDATER_CLEANUP_FAILED" = 0 ]
); then
  pass "missing updater unit bypasses unavailable non-interactive sudo"
else
  fail "missing updater unit should not be rechecked through sudo -n"
fi

if (
  UPDATER_CLEANUP_FAILED=0
  FAKE_SYSTEMCTL_MODE=safe
  disable_updater_service_with \
    && [ "$UPDATER_CLEANUP_FAILED" = 0 ]
); then
  pass "updater fallback verifies inactive and disabled service state"
else
  fail "verified inactive and disabled updater should be a safe fallback"
fi

if (
  UPDATER_CLEANUP_FAILED=0
  FAKE_SYSTEMCTL_MODE=safe
  sudo() { return 1; }
  ! disable_updater_service_with sudo -n \
    2> "$TEST_TMP/systemctl-sudo-required.err" \
    && [ "$UPDATER_CLEANUP_FAILED" = 1 ] \
    && grep -q 'Could not stop and disable' "$TEST_TMP/systemctl-sudo-required.err"
); then
  pass "known updater unit still fails closed when privilege is unavailable"
else
  fail "known updater cleanup must not trust an unavailable privilege wrapper"
fi

if (
  UPDATER_CLEANUP_FAILED=0
  FAKE_SYSTEMCTL_MODE=query-failure
  ! disable_updater_service_with 2> "$TEST_TMP/systemctl-query.err" \
    && [ "$UPDATER_CLEANUP_FAILED" = 1 ] \
    && grep -q 'Could not inspect the host updater' "$TEST_TMP/systemctl-query.err"
); then
  pass "updater fallback fails closed when systemctl inspection fails"
else
  fail "systemctl inspection failure should make updater fallback fatal"
fi

if (
  UPDATER_CLEANUP_FAILED=0
  FAKE_SYSTEMCTL_MODE=unsafe
  ! disable_updater_service_with 2> "$TEST_TMP/systemctl-unsafe.err" \
    && [ "$UPDATER_CLEANUP_FAILED" = 1 ] \
    && grep -q 'Could not stop and disable' "$TEST_TMP/systemctl-unsafe.err"
); then
  pass "updater fallback rejects an active or enabled final state"
else
  fail "active or enabled updater state should make fallback fatal"
fi

if [ "$FAILURES" -ne 0 ]; then
  echo "$FAILURES failure(s)"
  exit 1
fi
echo "all installer overlay migration checks passed"
