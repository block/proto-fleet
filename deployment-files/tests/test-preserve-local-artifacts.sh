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
  inspect) printf '%s\n' "${ARTIFACT_MOUNTS:-}" ;;
  stop) ;;
  cp) [ "${ARTIFACT_COPY_FAIL:-false}" != true ]; cp -R "$ARTIFACT_SOURCE/." "$3" ;;
  *) exit 1 ;;
esac
DOCKER
chmod +x "$scratch/bin/docker"
export PATH="$scratch/bin:$PATH"
run() { "$root/scripts/preserve-local-artifacts.sh" old-api "$scratch/install/artifacts"; }
run
cmp "$scratch/source/firmware/payload" "$scratch/install/artifacts/firmware/payload"
[ -d "$scratch/install/artifacts/command-artifacts" ]
[ -d "$scratch/install/artifacts/logs" ]
[ ! -e "$scratch/install/artifacts/fleetd" ]
run # Interrupted after copy: same retained container is safe to retry.
artifact_root=$(cd "$scratch/install/artifacts" && pwd -P)
export ARTIFACT_MOUNTS="/app/firmware $artifact_root/firmware
/app/command-artifacts $artifact_root/command-artifacts
/app/logs $artifact_root/logs"
: > "$ARTIFACT_CALLS"
run
! grep -q '^stop\|^cp' "$ARTIFACT_CALLS"
unset ARTIFACT_MOUNTS
printf 'conflicting\n' > "$scratch/install/artifacts/firmware/payload"
if run; then echo 'accepted conflicting artifact destination' >&2; exit 1; fi
[ "$(cat "$scratch/install/artifacts/firmware/payload")" = conflicting ]
export ARTIFACT_MOUNTS='/app/firmware /other/location'
if run; then echo 'accepted unexpected mount' >&2; exit 1; fi
unset ARTIFACT_MOUNTS
rm -rf "$scratch/install/artifacts"
export ARTIFACT_COPY_FAIL=true
if run; then echo 'accepted failed Docker copy' >&2; exit 1; fi
[ ! -e "$scratch/install/artifacts" ]
unset ARTIFACT_COPY_FAIL
ln -s /tmp "$scratch/source/firmware/unsafe"
if run; then echo 'accepted artifact symlink' >&2; exit 1; fi
[ ! -e "$scratch/install/artifacts" ]
printf 'Local artifact preservation checks passed.\n'
