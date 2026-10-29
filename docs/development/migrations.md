# Database migrations

Use golang-migrate and the existing `schema_migrations(version, dirty)` row.
Shared and private SQL use one sequence: shared baseline 1000, private baseline
1001, then ordinary migrations. Baselines run only on empty databases.

## Existing databases

### Pool configuration library import

The foundation uses shared migration 1045 and private checkpoint 1046, after
main's hosted-history summaries migration. Both apply in one transaction.
Development databases that applied the earlier unmerged pool stack at 1042–1054
or the unconsolidated foundation at 1047 have a different history. Use a separate
fresh development database or reconcile that history explicitly before running
this sequence; do not force the version or run it against those databases unchanged.

After migrating to checkpoint 1046 or later, stop application writers and run
`fleet-db-transition import-pool-configurations --organization-id=<internal-id>`
with the normal database settings and `SERVICE_MASTER_KEY`. The command stages
operator pools, current hosted configurations and revisions referenced by
hashboard intent in the neutral configuration store. Equivalent ordered stacks
merge only within the same organization/account owner, including password values
and worker settings. Operator pools with an omitted password retain an empty
password; missing device credentials are not treated as empty.

The JSON report contains source-to-configuration mappings and unresolved entries,
never credentials. A nonzero exit with unresolved entries means valid entries were
imported but the remaining configurations need review. Repeating the same source
versions is idempotent. Legacy rows and revisions remain intact. This command does
not establish hardware assignments, switch library readers, or send device writes.

### Baseline reconciliation

`fleet-db-transition apply` reconciles recognized source schemas without replaying
the fresh-install baseline. It validates the source catalog and grants, applies
missing changes, checks protected data and the target schema, and updates the
existing version row last in the same transaction under the migration advisory
lock. A failure before commit rolls back the transaction; repeating it against
a validated completed target is a no-op.

Reconciliation requires application and scheduled database writers to be stopped.
Unknown or dirty schemas and internal-to-public switches are refused. Application
rollback does not reverse committed database changes.

## Hosted write-fence upgrade

Private migrations 1008 through 1019 require a stopped-application maintenance
window. Stop every Fleet API replica and scheduled database writer before
running the target release's `fleet-db-transition migrate` command. Keep them
stopped until that command succeeds and `fleet-db-transition check --state=startup`
accepts the completed checkpoint, then start the target release.

Migration 1008 validates the queue's epoch constraint under an exclusive table
lock. Migration 1009 builds the pending-order index in its checkpoint
transaction. Migration 1010 adds the MDKv2 RPC endpoint fields without changing
queue or attempt evidence. Migration 1011 first locks and checks that the
attempt table is empty, then makes queue claims optional for direct attempts.
Migration 1012 adds captured physical identity keys and a partial GIN index on
outstanding attempts. Hosted ships as one feature, so these checkpoints do not
represent supported intermediate runtimes. Upgrades from internal nightlies
that recorded attempts before checkpoint 1011 are unsupported; migration 1011
refuses them without advancing checkpoint 1010. Migration 1012 repeats the
empty-history guard; it refuses any history rather than reconstructing
identities from an unfinished implementation. Migration 1013 separates node
heartbeats from locked route rows and backfills existing node timestamps. The
mining-asset view reads live node heartbeats for state and metrics eligibility.
Migration 1014 retains discovery serial/MAC fields for pairing; existing rows
receive empty defaults without inferring identity from previously paired devices.
Migration 1018 records ownership-independent host curtailment authority on attempts
without changing existing attempt history.
Migration 1019 adds current module identity confirmation to the internal container
inventory. Existing modules remain unconfirmed until a fresh controller report.
Deploy the server, Fleet Nodes, and Proto plugins from the same release before
using module actions. Earlier development builds of the unmerged V5 feature do
not send identity confirmation and must be rebuilt; a shared V5 version number
does not make those intermediate builds compatible. Historical identity alone
never enables controls.
Stop API replicas, plugins, Fleet Node dispatchers and scheduled writers
together for these migrations. The existing queue retains historical rows, so
miner count alone does not bound upgrade time. Rehearse on a recent database
backup and size the maintenance window from its queue row count, relation
sizes, observed lock wait, and migration duration. The 4,000-device upgrade
regression verifies data and checkpoint correctness; it does not establish a
production downtime budget.

An interrupted or failed migration rolls back to its last completed checkpoint.
After a lost commit acknowledgement, reconnect and inspect the checkpoint
before retrying. Do not force a version, delete attempt history, or run a down
migration to recover availability. Migration 1008 refuses rollback once
physical-outcome or ownership evidence exists; preserve the database and fix
forward.

If migration 1011 or 1012 finds attempt history from an intermediate internal
nightly, stop and preserve the database. That upgrade path is unsupported; do
not delete attempts, rewrite snapshots/statuses, or force a version to proceed.
Disposable development databases can be recreated. A database with real
operation history requires an explicit evidence-preserving migration decision.

After migration, require clean checkpoint 1019 and preserved queue history.
Verify that `idx_device_write_attempt_outstanding_identity_keys` is valid.
Downgrade 1012 refuses any retained attempt whose status is not `complete`,
including audited `resolved` attempts, or any deletion permits; downgrade 1011
refuses any direct-attempt history, including completed attempts. Preserve that
evidence and fix forward. Downgrade 1018 refuses any retained curtailment attempt,
including completed or resolved attempts, and pending or processing curtailment
commands.

The new `(created_at, id)` pending index keeps deterministic dequeue ordering.
The older pending index remains part of the shared baseline catalog contract;
removing it requires a separate shared migration and baseline decision.

## Hosted account allocation rollback

Migration 1028 retains allocation rows after recall. Their revisions reject stale
selections even when no client currently owns the asset. Its down migration
refuses any retained row; do not delete allocation history or recalled-row
revision witnesses to make rollback succeed. Preserve the database and fix
forward, or restore a rehearsed, coordinated application and database backup.
Disposable development databases can be recreated.

Normal application rollback and `fleet-db-transition` do not run down migrations.
The checkpoint runner commits migration SQL and its clean checkpoint together;
a failed migration rolls back to the previous completed checkpoint. After a
lost commit acknowledgement, reconnect and inspect the checkpoint before retrying.
If unsupported manual rollback tooling reports a dirty version, stop writers and
preserve the database. Inspect the actual schema and retained allocation rows
before choosing an evidence-preserving recovery; do not force a version or clear
the dirty marker based only on the reported version.

## Hosted chart summaries (1044)

Stop application and scheduled database writers while applying migration 1044.
Retained hosted history begins at this migration's boundary; earlier authorized
raw history remains readable while retained. See [hosted-account charts](ct1-polling-metrics.md#hosted-account-charts)
for retention, request limits and refresh recovery.

Rehearse the upgrade on a representative restored backup to size the maintenance
window and lock capacity. The table locks and trigger installation include existing
telemetry chunks, including up to a year of six-hour miner state chunks; fresh
database tests do not measure that footprint.

Prefer fixing forward. The 1044 down migration drops captured samples and both
summaries, including populated history. Once source retention has expired, that
history cannot be reconstructed. Application rollback does not require running it.

After deploying, and again after the first day, check:

- Every policy job runs and succeeds. Expect hourly refresh every 30 minutes and
  daily refresh every six hours:

  ```sql
  SELECT COALESCE(a.view_name, j.hypertable_name) AS history_source,
         j.proc_name, s.last_run_status,
         s.last_successful_finish, s.total_failures
  FROM timescaledb_information.jobs j
  JOIN timescaledb_information.job_stats s USING (job_id)
  LEFT JOIN timescaledb_information.continuous_aggregates a
    ON a.materialization_hypertable_schema = j.hypertable_schema
   AND a.materialization_hypertable_name = j.hypertable_name
  WHERE COALESCE(a.view_name, j.hypertable_name) LIKE 'hosted_account_history_%'
  ORDER BY 1, 2;
  ```

- Capture keeps up with open allocations. Sources 1–2 are miner metrics and
  state, sources 3–4 module metrics and state. Each source should report close
  to the open miner (kind 5) or module (kind 6) interval count:

  ```sql
  SELECT source_id, metric, count(DISTINCT interval_id) AS intervals, max(time) AS newest
  FROM hosted_account_history_sample
  WHERE time > now() - INTERVAL '1 hour'
  GROUP BY 1, 2 ORDER BY 1, 2;

  SELECT kind, count(*) FROM hosted_account_asset_interval
  WHERE ended_at IS NULL AND kind IN (5, 6) GROUP BY kind;
  ```

- Materialization advances. Expect the hourly watermark within about
  2.5 hours of now and the daily watermark within about 54 hours:

  ```sql
  SELECT * FROM hosted_account_history_watermarks('-infinity'::timestamptz);
  ```

## Hosted allocation history upgrade (1031–1033)

Stop Fleet API replicas, plugins, Fleet Node dispatchers and scheduled database
writers before upgrading. Take a coordinated application/database backup and
rehearse its restore before running the target release's
`fleet-db-transition migrate`. Include retained allocations, telemetry and
ownership history in the backup; restoring only allocation rows cannot restore
historical authorization. Migration 1031 locks device placement and allocations
while it establishes one history epoch and seeds current assignments from that
boundary. Earlier ownership is deliberately unknown. Migration 1032 builds
`idx_container_module_metrics_module_collected_at`; rehearse its lock wait and
build time on a representative backup to size the maintenance window.

Each private checkpoint commits separately. If 1032 fails after 1031 commits,
1031's epoch, intervals and triggers remain established at clean checkpoint 1031;
1032's index creation rolls back. Keep writers stopped, reconnect and inspect
`SELECT version, dirty FROM schema_migrations` before retrying. Correct the
reported cause and rerun the same release's `fleet-db-transition migrate` to
finish the target release. A lost acknowledgement may mean 1032 already committed: inspect the
checkpoint first. Do not force a version, clear dirty state, delete intervals or
reseed the epoch to make the retry succeed. Unexpected dirty state or catalog
drift requires evidence-preserving inspection before recovery.

Before restarting writers, require clean checkpoint 1033 and a successful
`fleet-db-transition check --state=startup`. Verify:

- Exactly one row exists in `hosted_account_history_epoch`, with the original
  upgrade boundary preserved across retries.
- Every allocated asset whose device is not deleted has exactly one open row in
  `hosted_account_asset_interval`, matching its organization, kind, asset,
  device, client, subclient, site and device identifier. Recalled allocations
  have no open interval; closed history remains intact. Intervals seeded during
  upgrade start at the epoch, never before it.
- `idx_hosted_account_asset_interval_open` and
  `idx_container_module_metrics_module_collected_at` exist and are valid in
  `pg_index`, and both allocation/placement history triggers are enabled.
- Customer history begins at the established boundary; data from before that
  boundary or another account/site does not appear. Confirm current inventory
  and allocation counts against the pre-upgrade snapshot.

Migration 1031's down guard refuses rollback whenever any interval exists,
including closed/recalled history. Preserve the database and fix forward, or
restore the rehearsed coordinated application/database backup with writers
stopped. Do not delete intervals to bypass the guard. Normal application
rollback does not reverse these migrations.

Migration 1033 orders compressed module history by collection time followed by
source time. Its upgrade recompresses retained compressed chunks one at a time;
rehearse storage, WAL volume and elapsed time on a representative backup, and
schedule the upgrade for a maintenance window because recompression locks chunks
until the checkpoint commits. A failure rolls
back that migration and preserves the previous clean checkpoint and compression
layout. Retry after correcting the underlying failure; do not roll back 1031.
Its down migration restores the prior compression order without discarding
telemetry or allocation history. After upgrading, verify that
`timescaledb_information.compression_settings` lists `collected_at` first and
`time` second for `container_module_metrics`; inspect an actual compressed query
plan for collection-bound batch filtering.

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
   Retain legacy SQL and the migration-143 bridge as historical fixtures and
   evidence for explicit upgrade waypoints. Current startup reads only
   `server/migrations/current/`; it does not invoke the legacy bridge. See the
   [migration-143 recovery procedure](../solutions/database-issues/dirty-migration-143-legacy-curtailment-upgrade-2026-08-25.md)
   for the verified public-release waypoint and its lineage restrictions.

### Transactions and large-table indexes

Every current migration file executes inside an explicit PostgreSQL transaction
owned by `checkpointDriver` in
`server/internal/infrastructure/db/database_connection.go`. Internal migrations
commit at private checkpoints; an earlier shared file and its following private
checkpoint can share that transaction. Locks acquired by a shared file remain
held until that checkpoint commits. A failure rolls back the uncommitted SQL.

`CREATE INDEX CONCURRENTLY` and `DROP INDEX CONCURRENTLY` cannot execute inside
these transactions, so they are unavailable through the current migration runner,
including `fleet-db-transition migrate`. Putting either command in its own file
does not change this. Migration 000072 used the old runner's concurrent-index
behavior; its historical comment does not describe the current runner.

For a large `curtailment_event` or `curtailment_target` table, use an ordinary
`CREATE INDEX` in a new migration and plan a stopped-writer maintenance window.
Rehearse on a representative backup with retained event and target history;
measure relation sizes, lock wait, index build time, free storage and WAL volume.
Size downtime from those measurements, not current miner count. Stop API replicas,
Fleet Nodes, plugins and scheduled writers, run the target release's
`fleet-db-transition migrate`, and verify the clean checkpoint, startup check and
`pg_index.indisvalid` before restarting writers.

If a deployment requires an online concurrent build, it needs a separately
reviewed operational procedure that runs standalone SQL outside this runner,
handles interrupted/invalid indexes, and verifies the exact index definition
before the subsequent migration adopts it. There is no nontransactional migration
mode. Do not add transaction-control statements to migration files or force a
checkpoint to skip an index build.

Current curtailment field and index semantics are maintained in the
[curtailment schema notes](../architecture/curtailment-schema.md); comments in
immutable legacy migrations describe their original implementation.

### Migration 1026 collision recovery

PRs #229 and #232 both merged migration 1026. The resulting duplicate prevents
the migration source from loading, so a later corrective migration cannot run.
Fleet Node details (#229, merged first) remains at 1026. Container telemetry
compression (#232) moves to 1027 with both SQL files byte-for-byte unchanged.
The history checker permits only this exact rename, pinned by SHA-256 checksums,
including when checking against the broken merged history.

Fresh databases apply both migrations in order. Databases at the clean 1026
checkpoint from #229 apply compression at 1027. A database migrated from the
standalone #232 branch may instead have compression recorded at 1026 without
Fleet Node details; that history requires separate reconciliation before using
this sequence. Do not force its version or assume this rename repairs it.

## Constraint and index names

Use explicit lowercase snake_case names in new and unmerged migrations:

- Unique constraints: `uq_<table>_<columns_or_purpose>`.
- Indexes, including standalone unique or partial indexes:
  `idx_<table>_<columns_or_purpose>`.

Name a unique constraint with `CONSTRAINT`; PostgreSQL gives its supporting
index the same name. Do not create a second index for the same uniqueness rule.

```sql
ALTER TABLE hosted_account_key
    ADD CONSTRAINT uq_hosted_account_key_org_api_key
    UNIQUE (organization_id, api_key_id);

CREATE INDEX idx_hosted_account_key_descendant_account
    ON hosted_account_key_descendant (organization_id, account_id);
```

Existing names vary. Naming cleanup does not justify editing merged or released
migrations; any intentional rename of a deployed object needs a new migration.

## Release checkpoints

Every internal release ends at a private number. After a shared-only change, add
an ordinary private `SELECT 1;` migration with its shared prerequisite. Public
releases contain only shared files.

Migration SQL and its completed private checkpoint commit in one transaction.
Interruption or failure rolls back to the previous checkpoint; retrying runs the
uncommitted SQL again. A lost commit acknowledgement requires a fresh connection
to establish which checkpoint committed. Previously dirty databases remain blocked.
Migration files must not contain transaction-control statements such as `BEGIN`
or `COMMIT`. Startup accepts only clean versions in the application's history:
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
