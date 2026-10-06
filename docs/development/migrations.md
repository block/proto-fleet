# Database migrations

Use golang-migrate and the existing `schema_migrations(version, dirty)` row.
Fresh installations start at baseline 1000, followed by ordinary migrations.
The baseline runs only on empty databases; existing installations require
explicit reconciliation.

## Existing databases

`fleet-db-transition` reconciles qualified existing schemas without replaying
baseline SQL. The public release accepts clean public versions 152/153 and
advances them to 1000. Older versions require a compatible legacy release first.

Reconciliation holds the migration advisory lock, validates the source catalog
and grants, applies missing changes, and verifies protected data, grants,
schedules and the target schema. The existing version row advances last in the
same transaction. A validated completed target is a no-op on repeat execution.
Failure before commit rolls back; a lost commit acknowledgement leaves the
outcome uncertain until fresh source/target checks establish it. Application
rollback does not undo database changes.

## Adding migrations

Coordinate schema changes and final migration numbers with maintainers. The
`cd server && just db-migration-new shared_<name>` command creates paired up/down
files in `server/migrations/current/`. Numbers increase but need not be contiguous.

Merged or released SQL is immutable: corrections use a new migration pair.
Legacy SQL and the migration-143 bridge remain available for upgrade waypoints.
The runner owns transaction boundaries: all files up to the next completed
checkpoint share one transaction. Their SQL must work together without an
intervening commit and must not contain transaction-control statements such as
`BEGIN` or `COMMIT`.

## Startup checks

Startup accepts only clean versions in this application's migration history.
Unknown, dirty and unsupported versions are rejected. Updates and rollback never
automatically run down migrations; changing version metadata alone does not
make a database compatible.

Pending SQL and its completed checkpoint commit atomically. An interrupted or
failed transaction leaves the previous checkpoint intact; retrying runs the
uncommitted SQL again. A lost commit acknowledgement requires a fresh connection
to establish which checkpoint committed. Previously dirty databases remain
blocked.

Checkpoints identify completed migration history, not application compatibility
or manual schema drift. Keep checksum, runtime, HA, health and restored-data
checks. Exact catalog assertions cover baseline adoption only; subsequent
releases need no catalog snapshots.

## CI

CI checks paired files, unique/increasing numbers, immutable history and the
`shared` migration naming convention.

```sh
python3 scripts/check-migration-history_test.py
python3 scripts/check-migration-history.py --base-ref origin/main --public-only
```
