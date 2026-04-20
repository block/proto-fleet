#!/usr/bin/env bash
set -euo pipefail

collect_existing_files() {
  local file=""

  # Lefthook includes deleted and renamed-away paths in {staged_files}.
  # Filter to paths that still exist before passing them to fixers or linters.
  # Reject symlinks to prevent auto-fix tools from following staged symlinks
  # that point outside the repository (arbitrary file-write via malicious branch).
  for file in "$@"; do
    if [[ -f "$file" && ! -L "$file" ]]; then
      printf "%s\0" "$file"
    fi
  done
}

require_fully_staged() {
  local label="$1"
  shift

  # Callers should pass only the paths they will mutate. The current hook globs
  # are disjoint, so these parallel checks do not contend over shared files.
  local partial_files=()
  local file=""
  for file in "$@"; do
    if [[ -n "$file" ]] && ! git diff --quiet -- "$file"; then
      partial_files+=("$file")
    fi
  done

  if ((${#partial_files[@]} > 0)); then
    printf "%s cannot auto-fix partially staged files:\n" "$label" >&2
    printf "  %s\n" "${partial_files[@]}" >&2
    printf "Fully stage those files, or commit them without the auto-fix hooks.\n" >&2
    exit 1
  fi
}
