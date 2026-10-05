# Adopting the compacted migration baseline

Reconcile existing databases offline before starting the baseline release.
Never replay fresh-install baseline SQL, force a version, clear dirty state or
run down migrations. The existing `schema_migrations` table stays unchanged.

**Release gate:** qualify fresh installs and restored source databases on the
exact PostgreSQL, TimescaleDB and toolkit versions. Catalog assertions and
synthetic fixtures do not prove real login, Node reconnection or recovery.
Complete the acceptance and restore checks below before release. See
[migration policy](migrations.md) for subsequent releases.

## Prepare

1. Verify source/target archives, manifests, checksums and runtime. Record the
   installed version, dirty state and schema check. Keep evidence and backups
   restricted and outside Git; keep credentials out of arguments and logs.
2. Stop all application writers and automatic replacements. In HA, close ingress
   and use the maintenance stop barrier. Discover the database writer separately
   from the active Fleet host. Record Timescale job settings, pause scheduled
   writers and wait for running jobs to finish.
3. Back up the database, PostgreSQL globals, configuration, TLS/auth/encryption/Node
   keys, both hosts' artifacts, source runtime and release bundle. Preserve
   firmware, command artifacts and logs from standalone container layers before
   an intermediate upgrade removes them; `pg_dump` does not cover local files or
   globals.
4. Rehearse restoring the complete set in isolation with the runtime's Timescale
   pre/post-restore hooks. Verify login, permissions, Node keys and artifact bytes.
   Never restore one member of a live Patroni cluster independently.

## Supported sources

Clean public 152/153 reconcile directly to shared baseline 1000. Conversion from
public 152/153/1000 to internal requires the private release's command and
procedure. Internal-to-public, unknown/dirty/future states and unqualified later
repository switches refuse.

Older public installations (including 130, 142, recognized dirty 143 and 149)
first need a verified legacy waypoint ending at 152, such as `v0.3.2-beta.3`.
Rehearse it on a restore; retained SQL and the narrow migration-143 repair do not
qualify an upgrade by themselves. Before crossing:

- **133:** record policy schedules for restoration.
- **136:** export all `notification_active` rows, including oversized labels and
  separator characters that this migration removes.

Keep these exports with the recovery set and verify retained curtailment
authorization envelopes during waypoint qualification.

## Reconcile and verify

Use the qualified target release's host binary with protected `DB_*` environment
variables (`DB_DSN` when configured). Supply the actual source and recorded
version; for public 152:

```sh
server/fleet-db-transition check --state source --source public --source-version 152
server/fleet-db-transition apply --source public --source-version 152
server/fleet-db-transition check --state target
```

Under the normal migration advisory lock, `apply` validates the source catalog
and grants, applies missing changes, checks protected data, grants, schedules and
target schema, then updates the version row last in the same transaction.
Repeating it against a validated completed target is a no-op.

Check the target through a new connection; in HA, also verify the local standby's
replay and runtime before either application starts. Restore job settings and
verify existing-password login, built-in/custom permissions, denied operations,
Node reconnection without re-enrollment, pairings and usable artifacts before
reopening. Standalone replacement retains the old container until artifact copy
and startup admission succeed. Later releases use `fleet-db-transition migrate`
(the stock runner).

## Recovery

- **Failure before commit:** reconciliation SQL and the version update roll back.
- **Lost commit acknowledgement:** keep applications fenced, prove the command
  has ended, rediscover the writer and run fresh source/target checks. Retry only
  an exact intact source; an exact target is complete. Investigate mixed/dirty
  states. Never clear pending cloud/SSM receipts or blindly resend a mutation.
- **Interrupted ordinary migration:** keep writers stopped. Restore the complete
  backup or use a reviewed repair that verifies executed and remaining SQL.
  Internal shared steps stay dirty until a private checkpoint; both applications
  refuse startup. Baseline reconciliation does not repair future dirty migrations.
- **Application failure after commit:** complete the qualified target deployment
  or restore the matching database, globals, runtime, configuration and artifacts.
  An older image cannot undo migrations. Decide explicitly how to handle writes
  made after the backup.

This procedure covers development qualification, not production rollout.
