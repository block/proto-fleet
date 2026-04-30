#!/usr/bin/env bash
# Pre-commit guard: refuse commits on `main`, `master`, or detached HEAD.
# Cut a feature branch first and commit there; the merge to main happens
# via PR.
#
# Exemption: skips the check during in-progress git operations (rebase,
# cherry-pick, merge, revert). Detached HEAD is normal during conflict
# resolution in those flows; blocking would strand contributors mid-flow.

set -euo pipefail

git_dir=$(git rev-parse --git-dir)

# Allow commits while a multi-step git operation is in progress.
for state in rebase-merge rebase-apply CHERRY_PICK_HEAD MERGE_HEAD REVERT_HEAD; do
  if [ -e "$git_dir/$state" ]; then
    exit 0
  fi
done

branch=$(git branch --show-current)

case "$branch" in
  main|master|"")
    label="${branch:-<detached HEAD>}"
    printf "Refusing commit on protected branch '%s'.\n" "$label" >&2
    printf "Cut a feature branch first: git switch -c <name>\n" >&2
    exit 1
    ;;
esac
