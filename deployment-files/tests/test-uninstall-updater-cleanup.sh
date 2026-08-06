#!/usr/bin/env bash
# Focused regression checks for exact host-updater binary cleanup. Production
# paths are redirected into a temporary directory; no host files are touched.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
UNINSTALL_SCRIPT="$REPO_ROOT/deployment-files/uninstall.sh"
TEST_TMP=$(mktemp -d)
TEST_TMP=$(cd "$TEST_TMP" && pwd -P)
trap 'rm -rf "$TEST_TMP"' EXIT

# uninstall.sh is a standalone executable, so extract only the constants and
# helpers under test instead of sourcing its interactive main path.
sed -n \
  -e '/^HOST_UPDATER_BINARY_PATHS=(/,/^)/p' \
  -e '/^HOST_UPDATER_SELF_UPDATE_SUFFIXES=(/,/^)/p' \
  -e '/^host_updater_binary_artifact_paths()/,/^}/p' \
  -e '/^host_updater_binary_artifacts_present()/,/^}/p' \
  -e '/^remove_host_updater_binary_artifacts_with()/,/^}/p' \
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

if [[ "$FAILURES" -ne 0 ]]; then
  echo "$FAILURES failure(s)"
  exit 1
fi
echo "all uninstall updater cleanup checks passed"
