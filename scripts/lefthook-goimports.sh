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

require_fully_staged "goimports" "${existing_files[@]}"
goimports -w "${existing_files[@]}"
