# Adopting the compacted migration baseline

This is an offline operator procedure. Do not start the new application on an
existing legacy database until reconciliation succeeds. The command never runs
baseline SQL against existing data, clears an unknown dirty state, or runs a down
migration. `schema_migrations` retains its original two columns and one row.

## Release qualification

Before publishing a baseline release, qualify its exact PostgreSQL, TimescaleDB
and toolkit versions, fresh installation and restored previous-release fixtures.
The checked-in catalog assertions were derived from isolated legacy replay and
compacted SQL, not a production dump. Synthetic fixtures do not prove real login,
Node reconnection or artifact recovery. A release remains unqualified until a
coordinated restore and those acceptance checks succeed on the intended runtime.

The public baseline is 1000. The internal release adds its private baseline and
ends at 1001. Ordinary subsequent migrations use one coordinated sequence and
retain prior-release schema admissions. Equal version numbers are not evidence
of repository compatibility.

## Prepare and back up

1. Verify the exact installed and target release archives, manifests, checksums
   and supported runtime. Record the installed migration version/dirty state and
   schema check. Keep this evidence and all dumps outside Git with restricted
   permissions. Do not expose credentials in command arguments or logs.
2. Stop application writers on every host and disable automatic replacement.
   For HA, close ingress and use the existing maintenance stop barrier. Discover
   the current database writer independently of the active Fleet host.
3. Record affected Timescale job schedules and pause scheduled writers, waiting
   for running jobs to finish. Preserve their original settings for restoration.
4. Capture a full database backup, PostgreSQL globals, configuration, TLS/auth/
   encryption/Node keys, both hosts' artifact trees, the exact source runtime and
   source bundle. `pg_dump` alone omits cluster globals and local files. Preserve
   firmware, command artifacts and logs from old standalone container layers
   before any intermediate upgrade removes the container.
5. Rehearse the complete restore in isolation using the runtime's supported
   Timescale pre/post-restore hooks. Prove existing login, permissions, Node keys
   and artifact bytes survive. Never restore one member of a live Patroni cluster
   independently.

## Supported starting states

Clean public legacy 152/153 can reconcile directly. Public 1000 can be explicitly
converted with the internal command. Internal installations use their private release procedure. Internal-to-public,
unknown/dirty states, future versions and unqualified later repository switches
refuse before mutation.

For older public installations (including 130, 142, recognized dirty 143 and 149),
first use a verified legacy release waypoint ending at 152, such as public
`v0.3.2-beta.3`, against a restored copy. The immutable legacy SQL and narrowly
recognized migration-143 repair remain in the source. The baseline command does
not apply arbitrary old history automatically.

Before crossing migration 136, retain a protected full export of
`notification_active`, including rows with oversized labels or the separator
character that the legacy migration removes. Before crossing 133, record all
policy schedules so the operator can restore intentional settings. Keep these
exports with the recovery set. Do not claim older-waypoint qualification without
verifying these cases and the retained curtailment authorization envelopes.

## Reconcile

Use the target release's `server/fleet-db-transition` host binary with the existing
protected `DB_*` environment (including `DB_DSN` when configured). For example,
on a qualified public-152 source:

```sh
server/fleet-db-transition check --state source --source public --source-version 152
server/fleet-db-transition apply --source public --source-version 152
server/fleet-db-transition check --state target
```

Choose the actual source application and recorded version, not the target's
number. The command validates the complete expected catalog and required grants,
locks the normal migration advisory key, preserves protected identities and
existing grants/schedules, applies only the reviewed missing changes, validates
the destination, and updates the existing row last in the same transaction.
A validated completed target makes repeated `apply` a no-op.

After success, check the target through a new connection. In HA also check the
local standby, including its replay and runtime, before either target application
starts. Restore the recorded job settings. Validate existing-password login,
custom and built-in permissions, denied operations, Node reconnection without
re-enrollment, device pairings, and usable artifact bytes before reopening.
Standalone replacement retains the old container until its artifact copy and
read-only startup admission succeed.

## Interrupted execution and recovery

An error before commit rolls back reconciliation SQL and the version update.
A lost commit acknowledgement is uncertain: keep applications fenced, establish
that the original command has ended, rediscover the writer and run fresh source/
target checks. Retry only an exact intact source; an exact target is complete;
a mixed or dirty state requires investigation. Do not force the version or clear
pending cloud/SSM receipts to make a retry possible.

After commit, an application failure requires qualified forward completion or a
coordinated restore of the matching database, globals, runtime, configuration and
artifacts. Reverting an image cannot undo the migration. Any writes after the
backup require an explicit recovery-point decision. Production rollout is outside
this procedure's development qualification.
