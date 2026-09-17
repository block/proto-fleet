#!/usr/bin/env bash
set -euo pipefail

base=origin/main
if [ "$#" -gt 0 ]; then
  if [ "$#" -ne 2 ] || [ "$1" != "--base" ]; then
    echo "usage: $0 [--base REF]" >&2
    exit 2
  fi
  base="$2"
fi

if ! git rev-parse --verify --quiet "$base^{commit}" >/dev/null; then
  echo "Base ref '$base' does not resolve. Fetch it or pass --base REF." >&2
  exit 2
fi

export CHECK_CHANGED_BASE="$base"
exec lefthook run changed-checks
