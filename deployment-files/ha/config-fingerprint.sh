#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

verbose=false

if [ "${1:-}" = "--verbose" ]; then
  verbose=true
fi

[ -f "$project_root/.env" ] || die "missing .env"

files=(
  ".env"
  "ssl/cert.pem"
  "ssl/key.pem"
  "server/asicrs-config.yaml"
  "server/proto-plugin"
  "server/antminer-plugin"
  "server/asicrs-plugin"
)

manifest=""
for rel in "${files[@]}"; do
  path="$project_root/$rel"
  if [ -f "$path" ]; then
    digest=$(sha256_file "$path")
    manifest="${manifest}${digest}  ${rel}"$'\n'
  fi
done

fingerprint=$(printf '%s' "$manifest" | LC_ALL=C sort | sha256_stream)

if [ "$verbose" = "true" ]; then
  printf '%s' "$manifest" | LC_ALL=C sort >&2
fi

printf '%s\n' "$fingerprint"
