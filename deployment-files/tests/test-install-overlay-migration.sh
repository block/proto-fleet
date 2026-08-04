#!/usr/bin/env bash
# Focused tests for the installer's legacy optional-overlay migration. Docker
# is stubbed so these cases exercise discovery, target validation, and label
# parsing without touching the host daemon.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INSTALL_SCRIPT="$REPO_ROOT/deployment-files/install.sh"
TEST_TMP=$(mktemp -d)
TEST_TMP=$(cd "$TEST_TMP" && pwd -P)
trap 'rm -rf "$TEST_TMP"' EXIT

# install.sh is intentionally standalone because it is executed directly from
# curl before a release bundle exists. Extract just its discovery/migration
# definitions instead of adding a production-only source-mode escape hatch.
sed '/^# END INSTALL DISCOVERY AND LEGACY MIGRATION HELPERS$/q' \
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
FAKE_CONFIG_FILES=""
FAKE_INSPECT_FAILURE=0
DOCKER_CALL_LOG="$TEST_TMP/docker-calls"

docker() {
  printf '%s\n' "$*" >> "$DOCKER_CALL_LOG"
  case "${1:-}" in
    ps)
      printf '%s\n' "$FAKE_IDS"
      ;;
    inspect)
      [ "$FAKE_INSPECT_FAILURE" != 1 ] || return 1
      case "${3:-}" in
        *com.docker.compose.service*) printf 'fleet-api\n' ;;
        *com.docker.compose.project.config_files*) printf '%s\n' "$FAKE_CONFIG_FILES" ;;
        *) return 1 ;;
      esac
      ;;
    *) return 1 ;;
  esac
}

sudo() {
  printf 'sudo %s\n' "$*" >> "$DOCKER_CALL_LOG"
  [ "${1:-}" != "-n" ] || shift
  "$@"
}

# Explicit settings are authoritative, including false, and avoid Docker
# inspection entirely.
if (
  root="$TEST_TMP/explicit"
  make_install "$root"
  printf '%s\n' \
    'ENABLE_BETA_ALERTS=true' \
    'export ENABLE_BETA_ALERTS: "false" # last value wins' \
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
  grep -q -- '--filter label=com.docker.compose.service=fleet-api' "$DOCKER_CALL_LOG"
  ! grep -q -- '--filter name=' "$DOCKER_CALL_LOG"
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
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/no-container.err"
  grep -q 'No existing fleet-api container matches' "$TEST_TMP/no-container.err"
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
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/inspect-failure.err"
  grep -q 'Could not inspect Docker' "$TEST_TMP/inspect-failure.err"
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
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/ambiguous.err"
  grep -q 'Multiple fleet-api containers match' "$TEST_TMP/ambiguous.err"
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
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/wrong-label.err"
  grep -q 'No existing fleet-api container matches' "$TEST_TMP/wrong-label.err"
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
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/invalid-env.err"
  grep -q 'must be true or false' "$TEST_TMP/invalid-env.err"
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
  ! capture_previous_run_options "$root" 1 2> "$TEST_TMP/inconsistent.err"
  grep -q 'system monitoring without beta alerts' "$TEST_TMP/inconsistent.err"
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
  grep -q '^sudo -n docker ps -a ' "$DOCKER_CALL_LOG"
  grep -q '^sudo -n docker inspect ' "$DOCKER_CALL_LOG"
); then
  pass "overlay migration reuses the detected Docker privilege"
else
  fail "overlay migration did not use the selected Docker privilege"
fi

if [ "$FAILURES" -ne 0 ]; then
  echo "$FAILURES failure(s)"
  exit 1
fi
echo "all installer overlay migration checks passed"
