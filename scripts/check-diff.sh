#!/usr/bin/env bash
set -euo pipefail

base="${1:-${CHECK_CHANGED_BASE:-origin/main}}"
merge_base="$(git merge-base "$base" HEAD)"
git diff --check "$merge_base"...HEAD
git diff --check
git diff --cached --check
