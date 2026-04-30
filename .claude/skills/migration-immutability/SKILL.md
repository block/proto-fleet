---
name: migration-immutability
description: Use when modifying an existing `server/migrations/*.sql` file already on `main`. Deployed migrations are immutable — `golang-migrate` skips already-applied versions, so editing one in place silently desyncs schemas across environments. Corrective changes must go in a new migration.
---

# migration-immutability

Once a migration has been applied in any environment, its content is
effectively frozen. `golang-migrate` sees "already at version N" and skips
re-running, so an in-place edit produces silent schema drift between
databases that ran the old version and those that ran the new one.

## When this skill applies

The user is editing a migration file already on `main`. Check via
`git log --diff-filter=A -- <path>`: if the *creation* commit is on `main`,
the migration is considered deployed.

## When this skill does NOT apply

- The user is **creating** a new migration. New migrations are how schema
  changes are supposed to land.
- The user is iterating on a migration **inside an unmerged feature branch**.
  Per project convention for net-new branch work, rebasing edits into the
  original migration is fine; immutability only kicks in after merge.

## What to do

1. Surface the immutability rule before the edit.
2. Propose creating a new migration with `cd server && just db-migration-new
   <name>` for the corrective change. Write both up and down migrations.
3. Run `just gen` afterward if any sqlc queries reference the changed schema.

## What to avoid

- Do not edit deployed migrations in place to "fix" a bug — fix is a new migration.
- Do not delete or renumber existing migrations.
