---
name: lefthook-bypass-guard
description: Use when about to run `git commit --no-verify`, `git commit -n`, `git push --no-verify`, or any command that bypasses the `lefthook` hooks configured in `lefthook.yml`. Pre-commit hooks (formatting, lint, ruff, buf-lint) and pre-push hooks (typecheck, golangci-lint) catch real problems; bypassing them shifts the failure to CI and produces noisier PRs.
---

# lefthook-bypass-guard

`lefthook.yml` runs:

- pre-commit: client format, server/plugin goimports, proto buf-lint, python ruff
- pre-push: client tsc, server golangci-lint, plugin golangci-lint

The hooks are fast (parallel) and `stage_fixed: true` for formatters means
they auto-stage their fixes. There is essentially no legitimate reason to
bypass them in normal development.

## What to do

1. If a hook fails, read the error and fix the underlying issue — that's
   the entire point.
2. If a hook is broken (not the user's code, the hook itself), surface that
   distinction to the user and offer to fix the hook config rather than
   bypassing.
3. If lefthook isn't installed (`command not found`), apply the
   `hermit-tooling` skill — `bin/activate-hermit` first, then
   `just install-hooks`.

## What to avoid

- Do not run `git commit --no-verify` or `-n` or `git push --no-verify`
  unless the user has explicitly asked for it and acknowledged the
  tradeoff.
- Do not edit `lefthook-local.yml` (or suggest it) to permanently disable
  a check.
