---
title: Duplicate golang-migrate version after merging main into a long-lived branch
date: 2026-05-12
category: docs/solutions/database-issues
module: server/migrations
problem_type: database_issue
component: database
symptoms:
  - "golang-migrate aborts at startup: 'duplicate migration file: 000048_rename_agent_to_fleetnode.down.sql'"
  - Every DB-backed test in Server Checks and E2E CI suites fails identically before its test body runs
  - Two files share the same numeric prefix under `server/migrations/` after merging main
  - Branch tests pass locally pre-merge; failure surfaces only after merging main in
  - `git merge` reports no textual conflict because the colliding files have different names
root_cause: missing_tooling
resolution_type: migration
severity: high
related_components:
  - testing_framework
  - tooling
tags: [golang-migrate, migrations, merge-conflict, ci-failure, version-numbering, multi-site, postgres]
---

# Duplicate golang-migrate version after merging main into a long-lived branch

## Problem

Merging `origin/main` into a long-lived feature branch produced two `golang-migrate` files at version `000048`. `golang-migrate` rejects duplicate version numbers at startup, so every DB-backed test in CI failed to bring up the schema and exited before running. The PR was otherwise merge-ready.

## Symptoms

- CI error from the migrator: `duplicate migration file: 000048_rename_agent_to_fleetnode.down.sql`
- Two files share the `000048` prefix after the merge:
  - `server/migrations/000048_add_site_id_to_device_set_rack.up.sql` / `.down.sql` (from branch `issue-196`)
  - `server/migrations/000048_rename_agent_to_fleetnode.up.sql` / `.down.sql` (landed on `main` via the agent→fleet_node rename refactor in commit `a2db45bb`)
- Every DB-backed test in Server Checks and E2E suites fails before running its test body — the migrator refuses to load the directory.
- Pre-merge, tests on the branch were green; the failure mode is a property of the *merged* tree, not either parent.

## What Didn't Work

- Re-running tests on the branch without re-merging main would not reproduce — the branch alone had a valid, monotonic sequence ending at `000048`.
- `git log` on either branch in isolation looked clean: each branch picked `000048` as the next available number when forked.
- `git merge` reported no textual conflict — the two files have different names, so Git happily kept both.
- This collision had been latent across multiple branches: `issue-195` (branch `maputo`) authored its own `000048_add_migration_banner_dismissed_at` on May 7, separately from the agent-rename PR that eventually took `000048` on `main` and from this branch's rack-site-id migration. Parallel branches were independently racing for the same slot, with no tooling to detect it. (session history)

## Solution

Rename the branch's migration to the next free version and verify nothing references the old number:

```bash
git mv server/migrations/000048_add_site_id_to_device_set_rack.up.sql \
       server/migrations/000049_add_site_id_to_device_set_rack.up.sql
git mv server/migrations/000048_add_site_id_to_device_set_rack.down.sql \
       server/migrations/000049_add_site_id_to_device_set_rack.down.sql
```

Grep the tree for the old version string (`000048` in this case) to catch any in-code references — sqlc embeds, test fixtures, docs in `docs/plans/`, etc. In this incident there were none.

Run `just lint`, `go vet`, and the targeted DB-backed tests before pushing.

Shipped as commit `82128d94`: *fix(multi-site): bump rack site_id migration to 000049*.

## Why This Works

`golang-migrate` requires unique numeric versions and applies them in monotonic order. Renaming to the next free integer restores uniqueness without changing semantics. In this case the rack `site_id` migration is purely additive (new column + FK + index) and commutes with the agent→fleet_node rename, so executing it at `000049` instead of `000048` produces an identical final schema.

The rename-the-branch-migration approach is the right move (rather than touching main's migration) for two reasons: main's migration is already part of shared history, and our [migration-immutability rule](../../../AGENTS.md) forbids editing shipped migrations.

## Prevention

1. **Before picking a version on a long-lived branch**, sync main and check the current max:
   ```bash
   git fetch origin main && git ls-tree --name-only origin/main server/migrations/ | sort | tail
   ```
2. **After every merge from main**, scan the merged tree for duplicate version prefixes:
   ```bash
   ls server/migrations/ | cut -c1-6 | sort | uniq -d
   ```
   Any output is a hard fail — fix before pushing.
3. **Add a lefthook pre-push hook (and CI guard)** that runs the `uniq -d` check above and exits non-zero on collision. Cheap, deterministic, would have caught this before the merge commit landed. The repo already uses lefthook for `block-protected-branches`; this is the same shape.
4. **When two PRs touch `server/migrations/` concurrently**, the later-to-merge PR owns the renumber. Call this out explicitly in PR descriptions when there are parallel migration-touching tracks (e.g. multi-site PRs A/B/C/D).

## Related

- `AGENTS.md` rule #3 — "Migrations are immutable after deploy." The skill `migration-immutability` codifies the same rule. This doc complements that rule: immutability covers "don't edit applied migrations", this doc covers "don't collide on version number when merging".
- Branch: `issue-196` · PR: #214 · Fix commit: `82128d94`
