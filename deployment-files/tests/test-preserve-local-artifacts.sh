#!/bin/bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin" "$scratch/source/firmware" "$scratch/install"
printf 'firmware bytes\n' > "$scratch/source/firmware/payload"
printf 'discarded binary\n' > "$scratch/source/fleetd"
export ARTIFACT_SOURCE="$scratch/source" ARTIFACT_CALLS="$scratch/calls"
cat > "$scratch/bin/docker" <<'DOCKER'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$ARTIFACT_CALLS"
case "$1" in
  inspect)
    if [ "$3" = '{{.State.Running}}' ]; then
      printf '%s\n' "${ARTIFACT_RUNNING:-true}"
    else
      printf '%s\n' "${ARTIFACT_MOUNTS:-}"
    fi ;;
  start)
    [ -z "$(find "$(dirname "$ARTIFACT_CALLS")/install" -name '.fleet-artifacts.*' -print -quit)" ]
    [ "${ARTIFACT_START_FAIL:-false}" != true ] ;;
  stop) ;;
  cp) [ "${ARTIFACT_COPY_FAIL:-false}" != true ]; cp -R "$ARTIFACT_SOURCE/." "$3" ;;
  *) exit 1 ;;
esac
DOCKER
cat > "$scratch/bin/mv" <<'MOVE'
#!/bin/bash
if [ "${ARTIFACT_COMMIT_FAIL:-false}" = true ] && [[ "$1" == */artifacts ]]; then
    /bin/mv "$@"
    exit 1 # The rename committed, but its caller observed failure.
fi
exec /bin/mv "$@"
MOVE
chmod +x "$scratch/bin/docker" "$scratch/bin/mv"
export PATH="$scratch/bin:$PATH"
run() { "$root/scripts/preserve-local-artifacts.sh" old-api "$scratch/install/artifacts"; }
run
cmp "$scratch/source/firmware/payload" "$scratch/install/artifacts/firmware/payload"
[ -d "$scratch/install/artifacts/command-artifacts" ]
[ -d "$scratch/install/artifacts/logs" ]
[ ! -e "$scratch/install/artifacts/fleetd" ]
run # Interrupted after copy: same retained container is safe to retry.
! grep -q '^start' "$ARTIFACT_CALLS"
artifact_root=$(cd "$scratch/install/artifacts" && pwd -P)
export ARTIFACT_MOUNTS="/app/firmware $artifact_root/firmware
/app/command-artifacts $artifact_root/command-artifacts
/app/logs $artifact_root/logs"
: > "$ARTIFACT_CALLS"
run
! grep -q '^stop\|^cp' "$ARTIFACT_CALLS"
unset ARTIFACT_MOUNTS
printf 'conflicting\n' > "$scratch/install/artifacts/firmware/payload"
: > "$ARTIFACT_CALLS"
if run; then echo 'accepted conflicting artifact destination' >&2; exit 1; fi
grep -qx 'start old-api' "$ARTIFACT_CALLS"
[ "$(cat "$scratch/install/artifacts/firmware/payload")" = conflicting ]
export ARTIFACT_MOUNTS='/app/firmware /other/location'
if run; then echo 'accepted unexpected mount' >&2; exit 1; fi
unset ARTIFACT_MOUNTS
rm -rf "$scratch/install/artifacts"
export ARTIFACT_COPY_FAIL=true
: > "$ARTIFACT_CALLS"
if run; then echo 'accepted failed Docker copy' >&2; exit 1; fi
[ ! -e "$scratch/install/artifacts" ]
grep -qx 'start old-api' "$ARTIFACT_CALLS"
: > "$ARTIFACT_CALLS"
if ARTIFACT_RUNNING=false run; then echo 'accepted failed copy from stopped container' >&2; exit 1; fi
! grep -q '^start' "$ARTIFACT_CALLS"
if ARTIFACT_START_FAIL=true run 2> "$scratch/error"; then echo 'accepted failed restart' >&2; exit 1; fi
grep -q 'Could not restart retained container' "$scratch/error"
unset ARTIFACT_COPY_FAIL
: > "$ARTIFACT_CALLS"
if ARTIFACT_COMMIT_FAIL=true run; then echo 'accepted uncertain copy commit' >&2; exit 1; fi
! grep -q '^start' "$ARTIFACT_CALLS"
cmp "$scratch/source/firmware/payload" "$scratch/install/artifacts/firmware/payload"
rm -rf "$scratch/install/artifacts"
: > "$ARTIFACT_CALLS"
ln -s /tmp "$scratch/source/firmware/unsafe"
if run; then echo 'accepted artifact symlink' >&2; exit 1; fi
[ ! -e "$scratch/install/artifacts" ]
grep -qx 'start old-api' "$ARTIFACT_CALLS"
printf 'Local artifact preservation checks passed.\n'
