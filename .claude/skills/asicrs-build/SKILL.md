---
name: asicrs-build
description: Use when editing files under `plugin/asicrs/`, `sdk/rust/`, or `server/sdk/v1/pb/` (which the asicrs build also consumes). The ASIC-rs plugin is a Rust binary built via Docker and cached against an `.asicrs-platform` marker; source changes that bypass `just _asicrs-build` or `just rebuild-plugin asicrs` will leave the loaded plugin stale.
---

# asicrs-build

`just build-plugins` and `just test-contract` rebuild ASIC-rs only when its
freshness check (mtime against the `BIN` plus a `find -newer` over
`plugin/asicrs sdk/rust server/sdk/v1/pb`) trips. That check is robust for
typical edits but skips when the binary mtime is newer than the source —
which is true after a clean checkout or when switching branches.

## What to do

1. After editing any source under `plugin/asicrs/`, `sdk/rust/`, or
   `server/sdk/v1/pb/`, force a rebuild:
   - Local development: `just rebuild-plugin asicrs`
   - Docker dev runtime (Linux ARM64): same command — it sets
     `_asicrs-build-docker` after removing the platform marker.
2. Run `just test-contract` if the change affects miner-driver behavior.
3. If switching between native and Docker runtimes, expect a full rebuild
   the first time — the `.asicrs-platform` marker is what disambiguates.

## What to avoid

- Don't hand-edit `server/plugins/asicrs-config.yaml` to make tests pass —
  the contract harness rewrites it per suite, and your edits will be
  overwritten.
