# Database migrations

Use golang-migrate and the existing `schema_migrations(version, dirty)` row.
Shared and private SQL use one sequence: shared baseline 1000, private baseline
1001, then ordinary migrations. Baselines run only on empty databases; existing
installations require explicit reconciliation.

## Existing databases

`fleet-db-transition` reconciles qualified existing schemas without replaying
baseline SQL. The public release accepts clean public versions 152/153 and
advances them to 1000. Older versions require a compatible legacy release first;
internal transitions belong to the internal release.

Reconciliation holds the migration advisory lock, validates the source catalog
and grants, applies missing changes, and verifies protected data, grants,
schedules and the target schema. The existing version row advances last in the
same transaction. A validated completed target is a no-op on repeat execution.
Failure before commit rolls back; a lost commit acknowledgement leaves the
outcome uncertain until fresh source/target checks establish it. Application
rollback does not undo database changes.

## Author and promote

1. Allocate numbers from internal main with
   `cd server && just db-migration-new shared_<name>` or
   `just db-migration-new internal_<name>`. Both create up/down files in
   `server/migrations/current/`.
2. Each private up file declares its required earlier shared version:
   `-- requires-shared: 1000`.
3. Test shared changes in both applications, then promote them to public unchanged
   and in order. Public skips private numbers; private changes to shared tables
   stay private.
4. Never edit, delete or renumber merged/released SQL. Correct it with a new pair.
   Retain legacy SQL and the migration-143 bridge for upgrade waypoints.

## Release checkpoints

Every internal release ends at a private number. After a shared-only change, add
an ordinary private `SELECT 1;` migration with its shared prerequisite. Public
releases contain only shared files.

The internal runner keeps shared steps dirty until the private checkpoint
completes. Startup accepts only clean versions in the application's history:
private for internal, shared for public. Unknown, dirty, future and wrong-application
versions refuse. Never force a version or clear dirty state to switch repositories;
updates and rollback never automatically run down migrations.

Checkpoints identify completed migration history, not application compatibility
or manual schema drift. Keep checksum, runtime, HA, health and restored-data
checks. Exact catalog assertions cover baseline adoption only; subsequent
releases need no catalog snapshots.

## CI

CI checks paired files, unique/increasing numbers, immutable history, private
prerequisites and private release tips. Internal CI checks that public shared SQL
is a byte-identical prefix; public CI needs no private credentials.

```sh
python3 scripts/check-migration-history_test.py
python3 scripts/check-migration-history.py --base-ref origin/main
```

Add `--public-only` in public, or `--public-ref <fetched-public-commit>` for
internal promotion checks.
