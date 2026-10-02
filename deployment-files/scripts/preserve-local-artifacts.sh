#!/bin/bash
# One-time adoption of the three application-owned paths from a legacy container.
# Run before Compose removes the container. Never merge conflicting storage.
set -euo pipefail

[ "$#" = 2 ] || { echo "Usage: preserve-local-artifacts.sh CONTAINER ARTIFACT_ROOT" >&2; exit 1; }
container="$1"
root="$2"
[ ! -L "$root" ] || { echo "Artifact root must not be a symlink." >&2; exit 1; }
parent=$(cd "$(dirname "$root")" && pwd -P)
root="$parent/$(basename "$root")"
mounts=$(docker inspect --format '{{range .Mounts}}{{println .Destination .Source}}{{end}}' "$container")
matched=0
while read -r destination source; do
    case "$destination" in
        /app/firmware|/app/command-artifacts|/app/logs)
            [ "$source" = "$root/${destination##*/}" ] || {
                echo "Existing artifact mount requires operator review: $destination" >&2; exit 1;
            }
            matched=$((matched + 1))
            ;;
        /app|/app/firmware/*|/app/command-artifacts/*|/app/logs/*)
            echo "Existing application mount requires operator review: $destination" >&2; exit 1 ;;
    esac
done <<< "$mounts"
if [ "$matched" = 3 ]; then
    for name in firmware command-artifacts logs; do
        [ -d "$root/$name" ] && [ ! -L "$root/$name" ] || {
            echo "Retained artifact directory is missing or unsafe: $root/$name" >&2; exit 1;
        }
    done
    exit 0
fi
[ "$matched" = 0 ] || { echo "Partially adopted artifact mounts require operator review." >&2; exit 1; }
[ ! -e "$root" ] || [ -d "$root" ] || { echo "Artifact destination is not a directory." >&2; exit 1; }
scratch=$(mktemp -d "$parent/.fleet-artifacts.XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT
chmod 700 "$scratch"
mkdir "$scratch/app" "$scratch/artifacts"
chmod 700 "$scratch/artifacts"
docker stop "$container" >/dev/null
# Copying /app also handles legitimately absent artifact directories without
# confusing a Docker failure with an empty directory. No old binary is retained.
docker cp "$container:/app/." "$scratch/app"
for name in firmware command-artifacts logs; do
    if [ -e "$scratch/app/$name" ] || [ -L "$scratch/app/$name" ]; then
        [ -d "$scratch/app/$name" ] && [ ! -L "$scratch/app/$name" ] || {
            echo "Artifact path is not a directory: $name" >&2; exit 1;
        }
        [ -z "$(find "$scratch/app/$name" ! -type d ! -type f -print -quit)" ] || {
            echo "Artifact directory contains unsupported entries: $name" >&2; exit 1;
        }
        mv "$scratch/app/$name" "$scratch/artifacts/$name"
    else
        mkdir "$scratch/artifacts/$name"
    fi
done
if [ -e "$root" ]; then
    # A retry after the copy committed must prove the existing destination is
    # identical. An unrelated or modified destination is never merged.
    [ -z "$(find "$root" ! -type d ! -type f -print -quit)" ] || {
        echo "Artifact destination contains unsupported entries." >&2; exit 1;
    }
    diff -r "$scratch/artifacts" "$root" >/dev/null || {
        echo "Artifact destination conflicts with the retained container: $root" >&2; exit 1;
    }
else
    mv "$scratch/artifacts" "$root"
fi
echo "Preserved local artifacts in $root. The old container remains stopped until replacement."
