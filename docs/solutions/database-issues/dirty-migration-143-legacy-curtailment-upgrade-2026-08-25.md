---
title: v0.3.0-rc.1 upgrade leaves migration 143 dirty when curtailment rows exist
date: 2026-08-25
category: docs/solutions/database-issues
module: server/migrations
problem_type: database_issue
component: database
symptoms:
  - "Fleet API restart-loops during a v0.3.0-rc.1 upgrade"
  - "failed to run migrations: Dirty database version 143. Fix and force version"
  - "The first migration attempt reports: curtailment authorization envelopes require empty response-profile and event tables"
root_cause: invalid_rollout_assumption
resolution_type: migration_bridge
severity: high
related_components:
  - curtailment
  - updater
  - release_process
tags: [golang-migrate, migrations, dirty-database, curtailment, upgrade, release-candidate, backward-compatibility]
---

# Dirty migration 143 during the v0.3.0 upgrade

## Problem

A real server upgrading from `v0.2.10-rc.15` to `v0.3.0-rc.1` stopped at:

```text
failed to run migrations: failed to run migrations:
Dirty database version 143. Fix and force version.
```

The database contained one response profile and nine curtailment events. The
first statement in `000143_curtailment_authorization_envelopes.up.sql`
deliberately rejects either table being nonempty:

```text
curtailment authorization envelopes require empty response-profile and event tables
```

Golang-migrate marks a migration dirty before executing it. The guard runs before
migration 143 changes either table, so the observed database was schema version
142 plus a `schema_migrations` row at `143/dirty`; neither
`authorization_envelope_jsonb` column existed.

## Root cause

PR #953 assumed there were no legacy curtailment records to preserve. That was a
rollout assumption, not a schema invariant, and it was false on a real
installation. The migration correctly failed closed, but there was no path for a
legitimate v0.2.x database to cross its precondition.

This affects both supported starting points:

- `v0.2.9` is schema version 130. Running all pending migrations reaches the
  version-143 guard with the old rows still present.
- `v0.2.10-rc.15` is schema version 142 and reaches the same guard immediately.

RC.1 additionally leaves those installations at `143/dirty` after the first
attempt.

## Current upgrade path

Current startup calls `runCurrentMigrations` with `migrations.Current` from
`server/migrations/current/`. It does **not** call
`runMigrationsWithCompatibilityBridges`. The legacy bridge remains embedded and
covered by legacy integration tests; its presence in the source tree does not
make current binaries capable of recovering `143/dirty` or directly upgrading a
version-130/142 database. Current startup rejects dirty and pre-baseline versions.

For a database from the **public `block/proto-fleet` lineage**, the explicit
intermediate release is
[`block/proto-fleet v0.3.2-beta.1`](https://github.com/block/proto-fleet/releases/tag/v0.3.2-beta.1).
This published prerelease ends at clean public schema 152 and its
[`ConnectAndMigrate`](https://github.com/block/proto-fleet/blob/71f14c2c7c82dc38a9c1435f014f978393141880/server/internal/infrastructure/db/database_connection.go)
invokes the migration-143 bridge before continuing the legacy migration sequence.
Its published release metadata and tag were verified on 2026-10-27; the annotated
tag resolves to `71f14c2c7c82dc38a9c1435f014f978393141880`. This is a specific
historical upgrade waypoint, not a recommendation to operate that prerelease
indefinitely.

With every application and scheduled database writer stopped, take and rehearse
a coordinated backup, then use that public release to cross the legacy boundary.
Require `152/clean`, both envelope columns and preserved row counts. Stop the
waypoint application again before using the target internal release's offline
transition commands (with the usual database connection flags/environment):

```sh
fleet-db-transition check --state=source --source=public --source-version=152
fleet-db-transition apply --source=public --source-version=152
fleet-db-transition migrate
fleet-db-transition check --state=startup
```

The current internal baseline's `public-152` recipe checks the exact supported
catalog and grants before adoption. A matching version alone is insufficient;
if that check rejects the schema, preserve the database and investigate the
reported mismatch. See [database migrations](../../development/migrations.md)
for the target release's maintenance and checkpoint requirements.

Do not run this public waypoint against an internal database. Public and internal
version numbers can describe different schemas. Current internal reconciliation
recognizes internal sources 153 and 158, not arbitrary earlier internal versions;
no published internal release that both runs the bridge and ends at one of those
qualified catalogs was verified for this procedure. An earlier internal database
needs a separately qualified internal waypoint/reconciliation decision. Retaining
the bridge does not establish that path.

## Historical bridge resolution

Migration 143 shipped and remains immutable. The legacy migration runner added a
narrow compatibility bridge around its version boundary:

1. Recover an RC.1 database already at `143/dirty` if neither envelope column
   exists.
2. For older databases, run an up-only migration source capped at version 142;
   it cannot downgrade a database concurrently advanced past the boundary.
3. Acquire golang-migrate's PostgreSQL advisory lock, then lock
   `schema_migrations` and inspect the two curtailment tables.
4. If no rows exist, leave migration 143 to run unchanged (or reset an empty
   RC.1 dirty marker to 142 first).
5. If rows exist, transactionally apply the exact final schema owned by migration
   143, preserve every row, write conservative version-1 envelopes, and mark
   version 143 clean.
6. Continue normal golang-migrate execution for later migrations.

Legacy executable scopes are not changed. Their synthetic envelope is
organization-wide, which is deliberately conservative: the new runtime requires
live organization-wide permission before a legacy profile or topology event can
be reused. Saving a profile through the new API replaces it with a precise
current envelope. Facility-fan events keep their persisted fan-site snapshot;
legacy profiles containing fans require organization-wide site-read permission.

The advisory lock serializes the legacy bridge with other legacy migration runners,
preventing another binary from overwriting the bridge's clean
version marker. Schema inspection resolves `schema_migrations` through the
connection's active search path rather than assuming `public`. The bridge also
refuses to guess if either envelope column
already exists, because that indicates a partial/manual schema state requiring
operator inspection.

## Legacy bridge verification

Database integration tests construct and upgrade all relevant states with one
legacy response profile and nine legacy events (matching the incident):

| Starting state | Expected result |
| --- | --- |
| v0.2.9 / schema 130 clean | migrate to 142, bridge rows, finish at 143 clean |
| v0.2.10-rc.15 / schema 142 clean | bridge rows, finish at 143 clean |
| v0.3.0-rc.1 incident / schema 143 dirty, columns absent | bridge rows, finish at 143 clean |
| schema 143 dirty, no rows or columns | reset to 142, run immutable 143 normally |
| concurrent migration runner holds the advisory lock | bridge waits, then completes safely |
| database already advanced beyond 142 | capped up-only source does not downgrade it |
| non-public active schema | bridge resolves and upgrades that schema |

For every row-preserving case, the legacy tests assert that row counts survive,
both columns contain conservative envelopes, version 143 is clean, and
golang-migrate accepts the resulting schema. These tests exercise the retained
legacy runner explicitly; they do not exercise the current startup path. The
original bridge change passed full server tests and server lint.

## Operational recovery for RC.1

Before recovery, stop Fleet API and take a PostgreSQL dump. Confirm:

```sql
SELECT version, dirty FROM schema_migrations;
-- 143 | true

SELECT count(*) FROM curtailment_response_profile;
SELECT count(*) FROM curtailment_event;

SELECT table_name
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name IN ('curtailment_response_profile', 'curtailment_event')
  AND column_name = 'authorization_envelope_jsonb';
-- zero rows
```

For the public lineage, use the verified waypoint above: its bridge handles the
recognized `143/dirty` state directly after proving both columns are absent.
Do not manually clear the dirty marker as a prerequisite for current startup.
If either column exists, the bridge refuses the partial/manual state; preserve
the database for inspection. Never force version 143 clean while the columns are
absent: the runtime requires them. Restoring a backup is a coordinated application
and database rollback, not a version-row edit.

## Prevention

- Validate every release migration against a copy of an actual previous-release
  database containing representative product data, not only fresh test DBs.
- Treat "there are no rows" as a rollout procedure requiring a verified preflight,
  not as an undocumented migration assumption.
- Once a migration ships in an RC, keep it immutable. Add a version-bound bridge
  or a later migration rather than changing its SQL in place.
- When a guard can intentionally stop an upgrade, ship operator diagnostics and
  a supported forward path in the same release.
