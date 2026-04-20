#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lefthook-lib.sh"

if (("$#" == 0)); then
  exit 0
fi

existing_files=()
while IFS= read -r -d '' file; do
  existing_files+=("$file")
done < <(collect_existing_files "$@")

if ((${#existing_files[@]} == 0)); then
  exit 0
fi

# No require_fully_staged — buf lint is read-only (no stage_fixed).

snapshot_dir="$(mktemp -d)"
trap 'rm -rf "$snapshot_dir"' EXIT

# Build the lint input from the index so staged proto and buf config are checked
# exactly as they will be committed, even if the working tree is dirty.
git ls-files -z -- buf.yaml buf.lock proto | git checkout-index -f -z --stdin --prefix="${snapshot_dir}/"

buf_paths=()
has_buf_config=false
for file in "${existing_files[@]}"; do
  if [[ "$file" =~ ^proto/.*\.proto$ ]]; then
    buf_paths+=("--path" "$file")
  elif [[ "$file" == "buf.yaml" || "$file" == "buf.lock" ]]; then
    has_buf_config=true
  fi
done

if ((${#buf_paths[@]} == 0)) && [[ "$has_buf_config" != "true" ]]; then
  exit 0
fi

(
  cd "$snapshot_dir"
  if ((${#buf_paths[@]} > 0)); then
    buf lint . "${buf_paths[@]}"
  else
    buf lint .
  fi
)
