# Database migrations

Use the existing `schema_migrations(version, dirty)` row and golang-migrate.
Shared/private identifies SQL ownership, not separate version counters.

## Ordinary changes

Allocate numbers internally from current main using
`cd server && just db-migration-new shared_<name>` or
`just db-migration-new internal_<name>`. Put paired up/down SQL in
`server/migrations/current/`; shared baseline 1000 and private baseline 1001
are the starting point. Private up files declare an earlier shared dependency:

```sql
-- requires-shared: 1000
```

Promote shared files to public unchanged and in order. Public numbering skips
private numbers; no public placeholders or private credentials are needed.
Private changes to shared tables remain private. Test shared changes against
both applications. Merged/released SQL is immutable; corrections get new numbers.
Keep legacy SQL and the migration-143 bridge for existing-installation waypoints.

## Private release checkpoints

Every internal release ends at a private migration number. When the last change
is shared, add an ordinary private migration containing `SELECT 1;` and its
`requires-shared` declaration. Public releases contain only shared files.
This lets the same version row identify which application's history completed.

The stock runner executes fresh baselines and subsequent migrations. A small
PostgreSQL driver adapter keeps internal shared steps dirty until a private
checkpoint completes, including between-file interruptions. Startup accepts only
clean versions present in this application's history: private checkpoints for
internal, shared versions for public. Unknown, dirty, future and wrong-application
versions refuse. Never clear dirty state or force a number to switch repositories.

Checkpoints establish migration provenance, not application compatibility or a
proof against manual schema edits. Existing checksum, runtime, HA and application
health checks remain required. Restore tests must validate each release's schema
and behavior. No per-release catalog snapshots or admission-history updates are
required. Exact catalog assertions are frozen inputs to the one-time transition.

## Checks and upgrades

CI enforces paired files, unique/increasing numbers, immutable history, private
dependencies and private release tips. Internal CI checks public's shared files
are a byte-identical prefix; public CI reads only its own repository.

```sh
python3 scripts/check-migration-history_test.py
python3 scripts/check-migration-history.py --base-ref origin/main
```

Add `--public-only` in public or `--public-ref <fetched-public-commit>` for internal
promotion checks. Baseline SQL is for empty databases. Existing installations
follow [the offline adoption procedure](baseline-upgrade.md). Repository switches
are explicit exceptions; neither ordinary updates nor rollback run down migrations.
