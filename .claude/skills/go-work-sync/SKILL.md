---
name: go-work-sync
description: Use when editing any `go.mod` or `go.sum` under `server/`, `plugin/`, or `tests/`, or when modifying root `go.work` / `go.work.sum`. Module dependency changes require running `go work sync` from the repo root so workspace lock data stays consistent.
---

# go-work-sync

The repo uses a root Go workspace so the server, Go plugins, and contract
tests resolve local modules consistently. Updating dependencies in a member
module can change workspace lock data; `go work sync` reconciles it.

## What to do

1. After dependency edits, run `go work sync` from the repo root.
2. Commit any resulting `go.work` / `go.work.sum` changes alongside the
   module changes that triggered them.
