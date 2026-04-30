#!/usr/bin/env bash
# Branch-protection guard: refuse commits on `main`, `master`, or detached
# HEAD. Cut a feature branch first; merges to main happen via PR.
#
# Wired into both `pre-commit` (covers `git commit` and `git merge`) and
# `prepare-commit-msg` (covers `git cherry-pick` and `git revert`, which
# bypass pre-commit) in `lefthook.yml`.
#
# Exemption: skips the check during a paused rebase. Detached HEAD is
# normal during rebase conflict resolution; blocking would strand
# contributors mid-flow. Cherry-pick / merge / revert happen on attached
# branches, so the regular branch check correctly handles those — we
# specifically don't exempt them here, since `git merge` / `git cherry-pick`
# / `git revert` directly on `main` are exactly what this guard exists to
# prevent.

set -euo pipefail

git_dir=$(git rev-parse --git-dir)

# Allow commits while a rebase is paused (typically detached HEAD).
for state in rebase-merge rebase-apply; do
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
