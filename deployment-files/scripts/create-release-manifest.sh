#!/bin/bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
    echo "Usage: create-release-manifest.sh DEPLOYMENT_ROOT" >&2
    exit 1
fi

deployment_root="$1"

if [ ! -d "$deployment_root" ]; then
    echo "Error: deployment root must be an existing directory: $deployment_root" >&2
    exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
    sha256_cmd=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
    sha256_cmd=(shasum -a 256)
else
    echo "Error: SHA-256 support requires sha256sum or shasum." >&2
    exit 1
fi

(
    cd "$deployment_root"

    unsupported_entries=$(find . ! -type f ! -type d -print)
    if [ -n "$unsupported_entries" ]; then
        echo "Deployment bundle contains unsupported non-regular entries:" >&2
        while IFS= read -r unsupported_entry; do
            printf '  %s\n' "$unsupported_entry" >&2
        done <<< "$unsupported_entries"
        exit 1
    fi

    # Keep this scope aligned with find_release_entries in run-fleet.sh. These
    # paths are operator-owned or generated runtime state and are validated or
    # fingerprinted separately during upgrade preflight.
    find_immutable_release_files() {
        find . -type f \
            ! -path './.env' \
            ! -path './.update-preflight-complete' \
            ! -path './.update-preflight-complete.tmp.*' \
            ! -path './.fleet-startup-complete' \
            ! -path './.fleet-startup-complete.tmp.*' \
            ! -path './.docker-daemon-id' \
            ! -path './.docker-daemon-id.tmp.*' \
            ! -path './client/nginx.conf' \
            ! -path './ssl/*' \
            ! -path './server/influx_config/.env' \
            ! -path './ha/node.env' \
            ! -path './deployment-manifest.sha256' \
            "$@"
    }

    first_immutable_file=$(find_immutable_release_files -print -quit)
    if [ -z "$first_immutable_file" ]; then
        echo "Error: deployment bundle contains no immutable release files." >&2
        exit 1
    fi

    find_immutable_release_files -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 "${sha256_cmd[@]}" \
        > deployment-manifest.sha256
)
