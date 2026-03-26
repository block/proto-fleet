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

require_fully_staged "Client formatting hooks" "${existing_files[@]}"

# Lefthook passes repo-relative paths; strip client/ prefix for npm exec.
eslint_files=()
prettier_files=()

for file in "${existing_files[@]}"; do
  relative_file="${file#client/}"
  case "${file##*.}" in
    ts|tsx|js|jsx)
      eslint_files+=("$relative_file")
      prettier_files+=("$relative_file")
      ;;
    css|md)
      prettier_files+=("$relative_file")
      ;;
  esac
done

if ((${#eslint_files[@]} > 0)); then
  (
    cd client
    npm exec --no -- eslint --fix -- "${eslint_files[@]}"
  )
fi

if ((${#prettier_files[@]} > 0)); then
  (
    cd client
    npm exec --no -- prettier --write -- "${prettier_files[@]}"
  )
fi
