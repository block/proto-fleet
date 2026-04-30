#!/usr/bin/env bash
# Pre-commit guard: refuse commits on `main`, `master`, or detached HEAD.
# Cut a feature branch first and commit there; the merge to main happens
# via PR.

set -euo pipefail

branch=$(git branch --show-current)

case "$branch" in
  main|master|"")
    label="${branch:-<detached HEAD>}"
    printf "Refusing commit on protected branch '%s'.\n" "$label" >&2
    printf "Cut a feature branch first: git switch -c <name>\n" >&2
    exit 1
    ;;
esac
