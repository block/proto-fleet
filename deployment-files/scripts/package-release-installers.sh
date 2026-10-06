#!/usr/bin/env bash
# Stage standalone release assets; never upload the source-default installers.
set -euo pipefail
repository="${1:?usage: package-release-installers.sh OWNER/REPO OUTPUT_DIR}"
output="${2:?output directory is required}"
if [[ ! "$repository" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$ ]] \
  || [[ "$repository" == *-/* || "$repository" == *..* ]]; then
  echo 'Invalid release repository: expected GitHub owner/repo.' >&2
  exit 1
fi
source_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
mkdir -p "$output"
output=$(cd "$output" && pwd -P)
for source in install.sh fleetnode/install-fleet-node.sh; do
  target="$output/${source##*/}"
  [ "$source_root/$source" != "$target" ] || { echo 'Output must be a staging directory.' >&2; exit 1; }
  [ ! -L "$target" ] || { echo 'Staged installer must not be a symlink.' >&2; exit 1; }
  [ "$(grep -c '^PACKAGED_RELEASE_REPOSITORY=' "$source_root/$source")" = 1 ] || {
    echo "Missing or duplicate repository placeholder in $source" >&2
    exit 1
  }
  sed "s|^PACKAGED_RELEASE_REPOSITORY=.*|PACKAGED_RELEASE_REPOSITORY=\"$repository\"|" "$source_root/$source" > "$target"
  grep -Fxq "PACKAGED_RELEASE_REPOSITORY=\"$repository\"" "$target"
  bash -n "$target"
  chmod 755 "$target"
done
