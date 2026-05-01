---
name: hermit-tooling
description: Use when a shell command in this repo fails with "command not found" for `just`, `buf`, `golangci-lint`, `goimports`, `lefthook`, `ruff`, or any other developer tool. The repo pins its toolchain via Hermit (`.hermit/`); the tools exist on disk but only resolve after the Hermit environment is activated.
---

# hermit-tooling

This repo uses Hermit to pin every developer tool (`just`, `buf`,
`golangci-lint`, `goimports`, `ruff`, `lefthook`, etc.) to a specific
version. The binaries live under `.hermit/` and only join `PATH` after the
environment is activated.

## What to do

1. If a tool resolves to "command not found", activate Hermit:
   ```
   source bin/activate-hermit
   ```
   Verify with `which just` (or whatever was missing) — it should resolve
   under the repo's `.hermit/` path.
2. If activation itself fails, fall back to running the tool via its full
   path under `.hermit/python/bin/` etc. — do not install system-wide.
3. For tools missing from `.hermit/` entirely, check `bin/` for activation
   shims; CONTRIBUTING.md "Hermit Setup" covers prerequisites.

## What to avoid

- Don't `brew install`, `apt install`, `npm install -g`, or `pip install`
  to satisfy a missing tool. Hermit pins versions deliberately and a
  system-wide install will mask drift.
- Don't add `--no-verify` to commits because lefthook is missing —
  activate Hermit instead.
