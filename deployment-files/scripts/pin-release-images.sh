#!/bin/bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
    echo "Usage: pin-release-images.sh DEPLOYMENT_ROOT RELEASE_TAG" >&2
    exit 1
fi

deployment_root="$1"
release_tag="$2"

if [[ ! "$release_tag" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]] || [ "${#release_tag}" -gt 128 ]; then
    echo "Error: release tag must be a valid Docker tag of at most 128 characters." >&2
    exit 1
fi
if [ "$release_tag" = "latest" ]; then
    echo "Error: release bundles must not use the shared latest image tag." >&2
    exit 1
fi

pin_reference() {
    local file="$1" source="$2" target="$3" count temporary line pinned_line

    if [ ! -f "$file" ]; then
        echo "Error: release image pinning requires $file." >&2
        return 1
    fi
    count=$(grep -Foc "$source" "$file" || true)
    if [ "$count" -ne 1 ]; then
        echo "Error: expected exactly one $source reference in $file; found $count." >&2
        return 1
    fi

    temporary=$(mktemp "${file}.tmp.XXXXXX") || return 1
    if ! while IFS= read -r line || [ -n "$line" ]; do
        printf '%s\n' "${line//$source/$target}"
    done < "$file" > "$temporary"; then
        rm -f "$temporary"
        return 1
    fi
    if ! cat "$temporary" > "$file"; then
        rm -f "$temporary"
        return 1
    fi
    rm -f "$temporary"
}

for repository in proto-fleet-api proto-fleet-client proto-fleet-timescaledb; do
    pin_reference \
        "$deployment_root/docker-compose.yaml" \
        "${repository}:latest" \
        "${repository}:${release_tag}"
done
pin_reference \
    "$deployment_root/ha/compose.yaml" \
    "proto-fleet-timescaledb-ha:latest" \
    "proto-fleet-timescaledb-ha:${release_tag}"
