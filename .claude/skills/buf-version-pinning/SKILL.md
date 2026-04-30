---
name: buf-version-pinning
description: Use when editing `buf.yaml`, `buf.lock`, or any `buf.gen.yaml` (root, server, or SDK). Generator plugin versions in `buf.gen.yaml` are tightly coupled to the generated code consumers (Connect-RPC client, sqlc, SDK shapes); a version bump can produce a large unrelated diff and break downstream code that relies on specific output shapes.
---

# buf-version-pinning

`buf.gen.yaml` declares which protoc plugins (and which versions) generate
the Go, TypeScript, and Python output. Bumping a plugin version regenerates
*every* file the plugin produces, even for protos that didn't change. This
is normal but easy to confuse with bugs.

## What to do

1. When the diff is a `buf.gen.yaml` plugin version bump, expect a wide
   regen sweep — surface that to the user before they assume the diff is
   wrong.
2. After the change, run `just gen` and group the diff by language so the
   user can see scope at a glance.
3. If `buf.lock` changed, confirm `buf.yaml` deps changed correspondingly
   (or that `buf dep update` was run intentionally).
4. Check whether any consumers of the generated output rely on a specific
   plugin version (e.g. a Connect generator API change) before merging.

## What to avoid

- Don't pin a plugin version to "make the diff smaller". The repo prefers
  current pinned versions across all `buf.gen.yaml` files.
- Don't hand-edit `buf.lock`.
