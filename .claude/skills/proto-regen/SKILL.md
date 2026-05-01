---
name: proto-regen
description: Use when editing any `.proto` file under `proto/` or `server/sdk/v1/pb/`, or modifying root/server `buf.yaml`, `buf.gen.yaml`, or `buf.lock`. Protobuf changes require regenerating Go, TypeScript, and SDK code via `just gen`; without it, server handlers, client API hooks, SDK consumers, and plugins compile against stale shapes.
---

# proto-regen

## What to do

1. After the proto edit, run `just gen` from the repo root. This drives
   `buf generate`, the SDK proto generation, and sqlc/Go generation.
2. Group the regenerated files by language (Go server, Go SDK, Python SDK,
   TypeScript clients) and confirm they match the proto edit. Unrelated diffs
   usually mean the branch was already stale.
3. Stage proto sources and regenerated output together — splitting them
   breaks `git bisect` and reviews.
4. If the change adds or removes a service method or a field with explicit
   presence, also check that server handlers in `server/internal/handlers/`
   and client API hooks under `client/src/{app}/api/` are updated, and that
   no call sites still reference removed fields.

## What to avoid

- Do not run `buf` or `protoc` ad-hoc; `just gen` is the canonical entry
  point and chains formatting/lint.
