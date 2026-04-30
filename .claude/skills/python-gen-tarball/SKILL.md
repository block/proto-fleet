---
name: python-gen-tarball
description: Use whenever the user changes any source under `packages/proto-python-gen/` (Python source, `setup.sh`, `requirements.txt`, `bin/`) or edits `scripts/pip-config.sh` (which gets bundled). The Python proto generator ships as a versioned tarball that Hermit consumes; if source changes but the tarball isn't rebuilt and committed, downstream consumers silently use the stale generator.
---

# python-gen-tarball

The tarball is the distribution unit. Local edits run against the venv, so
tests pass — but Hermit-bootstrapped environments and CI extract the
committed `proto-python-gen-<version>.tar.gz` and keep using the old code
until it's rebuilt. This is a quiet failure mode that's easy to miss.

## What to do

1. Decide whether to bump `version` in `packages/proto-python-gen/justfile`:
   - Behavior change or new feature → bump.
   - Pure refactor → optional. Ask if unclear.
2. Build:
   ```
   cd packages/proto-python-gen && just package
   ```
   Produces `proto-python-gen-<version>.tar.gz`.
3. Stage the tarball alongside the source change. A source-only commit will
   fail to bootstrap fresh environments.
4. If the version was bumped, search for hard-coded references and update:
   ```
   rg "proto-python-gen-[0-9]" -g '!*.tar.gz'
   ```

## What to avoid

- `just gen` does not rebuild this tarball. Only `just package` inside
  `packages/proto-python-gen/` does.
