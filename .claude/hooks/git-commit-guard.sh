#!/usr/bin/env bash
# PreToolUse hook (invoked from .claude/settings.json) that refuses
# `git commit` on protected branches. Reads the harness JSON payload from
# stdin without depending on jq, so the guard works even when Hermit
# hasn't been activated.
#
# Fail-closed: if the current branch is empty (detached HEAD), the guard
# treats it as protected and blocks the commit.

input=$(cat)

# Detect a top-level `git commit` invocation. Handles both compact and
# pretty JSON encoding (with or without a space after the colon).
case "$input" in
  *'"command":"git commit"'*|*'"command":"git commit '*|*'"command": "git commit"'*|*'"command": "git commit '*) ;;
  *) exit 0 ;;
esac

branch=$(git branch --show-current 2>/dev/null)
case "$branch" in
  main|master|"")
    printf 'Refusing git commit on protected branch %s. Cut a feature branch first: git switch -c <name>\n' \
      "${branch:-<detached HEAD>}" >&2
    exit 2
    ;;
esac

exit 0
