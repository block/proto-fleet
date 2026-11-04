# Database migrations

Use golang-migrate and the existing `schema_migrations(version, dirty)` row.
Shared and private SQL use one sequence: shared baseline 1000, private baseline
1001, then ordinary migrations. Baselines run only on empty databases.

## Existing databases

### Hashboard allocation feature databases

Hashboard allocation uses private migrations 1049–1052, after the hosted-history,
pool-configuration, and client-ticketing migrations on main. A development database that applied
the earlier, unmerged hashboard sequences at 1044–1046 or 1047–1049 has a different schema at
the same version. Preserve that database and use a fresh development database,
or reconcile its schema explicitly before using this sequence. Do not force its
version or replay the renumbered migrations against it.

### Pool configuration library import

The foundation uses shared migration 1045 and private checkpoint 1046, after
main's hosted-history summaries migration. Reviewed execution adds private
migration 1047. Equipment adoption and readback use shared migration 1053 and
private checkpoint 1054. Each shared/private pair applies in one transaction;
the separate files preserve the public/private schema boundary.
Development databases that applied the earlier unmerged pool stack at 1042–1054
or the unconsolidated foundation at 1047 have a different history. Use a separate
fresh development database or reconcile that history explicitly before running
this sequence; do not force the version or run it against those databases unchanged.

After migrating to checkpoint 1054 or later, stop application writers and run
`fleet-db-transition import-pool-configurations --organization-id=<internal-id>`
with the normal database settings, `SERVICE_MASTER_KEY`, and the same trusted
plugin binaries used by Fleet (`--plugins-plugins-dir` / `PLUGINS_DIR`). The command stages
operator pools, current hosted configurations and revisions referenced by
hashboard intent in the neutral configuration store. Equivalent ordered stacks
merge only within the same organization/account owner, including password values
and worker settings. Operator pools with an omitted password retain an empty
password; missing device credentials are not treated as empty.

For unmanaged paired miners and containers with mining groups disabled, the
command reads complete private firmware settings through the plugins before
opening the import transaction. It sorts the slots by priority and normalizes
only the exact saved worker-name suffix, preserving the rendered username.
Complete identical stacks share a configuration within their current owner.
Ownership, physical identity, and worker settings are rechecked before adoption.
The readback stays in memory; only encrypted credentials are persisted.

Offline devices, masked passwords, mixed container ownership, and Fleet Node
devices that cannot be reached through this offline command appear in the
unresolved report. Their settings remain untouched. Resolve those entries before
cutover; a URL/username library match is not a substitute for complete readback.

The JSON report contains source-to-configuration mappings and unresolved entries,
never credentials. A nonzero exit with unresolved entries means valid entries were
imported but the remaining configurations need review. Repeating the same source
versions is idempotent. Legacy rows and revisions remain intact.

The command also adopts existing explicit hashboard intent when the exact
installation is still current and the configuration owner still owns its module.
Identical stacks retain a single configuration within that owner. Reallocated or
removed hardware is reported for review; credentials are never copied to a new
owner. Unfinished mining-group operations and unresolved writes that could affect
pools block adoption only for their devices. Those devices appear in the unresolved
report before firmware reads and are rechecked at commit. Unrelated LED, log,
cooling, and power-target attempts do not block pool adoption. Unclassified
operations remain conflicts. Library staging and unaffected equipment continue.

Adopted assignments have an `adopted_at` timestamp and no application operation:
migration does not send hardware writes or claim that settings were verified.
Any later application still requires a reviewed operation. Repeating the import
cannot replace newer intent or revive retired assignments. Rollback of shared migration
1053 refuses assignments without reviewed operations rather than inventing history.
Reported pools without complete, authoritative credentials remain unmanaged;
URL/username matches do not establish an assignment.

Pool update claims retain their physical attempt IDs. Interrupted `running`
attempts require the existing offline device-write recovery procedure; neither
age nor a server restart authorizes another dispatch. Once an attempt is
explicitly resolved, or its completed call has lacked result bookkeeping for a
minute, the result becomes **Update interrupted**. This permits a new reviewed,
password-confirmed retry without claiming the previous settings were verified.
The one-minute delay allows normal result bookkeeping to finish; only the
attempt's terminal state establishes that its physical work has settled.

### Pool backend cutover (checkpoint 1056)

This backend release is the intermediate change between pool backend PR3 and
UI PR4. Checkpoint 1056 adds durable, installation-scoped board execution intent
and a maintenance gate. Successfully applied board effects survive a failed
operation or a later rejected request. Pending work overlays those retained
settings only while it is applying. Adoption records provenance, without a
physical attempt or a fabricated successful operation. Observation still compares
URLs/usernames; passwords are preserved privately and are not read back to prove
application.

Before maintenance, existing UI endpoints remain available. After completion,
normal reconciliation, interruption and offline recovery use canonical board
intent and immutable execution snapshots. Legacy storage remains read-only for
history. Legacy pool library writes, board submissions and raw queued pool
commands are rejected. PR4 owns the UI switch and final endpoint removal; deploy
that switch together with final completion. Do not complete this gate with the
old UI or external pool callers still active.

The maintenance command exposes identifiers and counts, never credentials:

```sh
fleet-db-transition pool-cutover --organization-id=<internal-id> --action=inspect
```

Inventory covers legacy intents, retained bindings (including binding-only
archived revisions), canonical snapshots, queued/processing commands, unresolved
physical attempts, pending canonical operations, and devices lacking complete
adoption. External callers cannot be discovered from database contents: inventory
API keys, integrations and installed CLI versions separately. Production volume
and conversion latency have not been measured by this change. Rehearse with a
representative database and record counts, elapsed time and unresolved reasons
before scheduling maintenance. An empty inventory can use the same gate without
inventing legacy execution state.

Use this order for each organization, with a coordinated database backup:

1. Stop new pool admission. Drain queued/processing pool and lifecycle commands
   and unfinished mining-group work while dispatchers can still finish it.
   Resolve running/unknown physical attempts through the existing
   [device-write recovery procedure](device-write-recovery.md); age, restarts and
   a failed UI result are not proof that hardware writes ended.
   Inspect before freezing. Attempted board configurations without matching
   retained effective state are also unresolved, including older failures that
   may have partially applied. Explicitly verify a reviewed configuration or
   retire the assignment before maintenance; the gate never infers success from
   a failed operation or silently substitutes an older verified configuration.
2. Stop all application writers and dispatchers, including standby processes,
   integrations, schedules and Fleet Node command delivery. With writers stopped,
   run `import-pool-configurations` with the encryption key and trusted plugins
   described above, **before setting the persistent freeze**. It creates controller
   snapshots and rewrites retained bindings, preserving group IDs and configuration
   evidence. Equal library settings may share a configuration, but retained
   controller groups keep distinct execution identities. Board assignment adoption
   joins import mappings in one SQL batch. Ambiguous group membership is unresolved,
   not guessed. Source-table locks protect each import transaction.
3. Save the JSON report, rerun inspection, and resolve every remaining gap. The
   importer can commit eligible records and then exit nonzero: **this is partial
   progress, not rollback and not permission to resume ordinary writers**. A clean
   import report alone is insufficient: empty device readback and containers in
   mining-group mode may be skipped by collection and still appear in inspection.
   Fleet Node devices cannot be collected offline. These cases, missing credentials,
   removed/reallocated boards, and ambiguous controller membership need an explicit
   resolution before freezing. If repair requires normal assignment or recovery,
   bring up only the required services under controlled admission, perform the
   reviewed repair and let it settle, then stop writers again and rerun import and
   inspection. Do not invent credentials, adopt a previous owner's configuration,
   or manufacture a successful write to clear a gap. Known-empty passwords are
   valid; unknown passwords require complete authoritative settings. There is no blanket
   ignore-unresolved switch or implicit unmanaged acceptance in this command.
4. Only after import and inspection are clear, freeze legacy writers:
   `fleet-db-transition pool-cutover --organization-id=<internal-id> --action=freeze`.
   Freeze uses the same persisted completeness check as completion, under the
   source locks. Any remaining or newly introduced gap rejects freeze without
   setting its marker, leaving normal assignment and audited recovery available.
   Database guards serialize with already-admitted legacy writers and reject new
   writes, including attempts to requeue terminal raw pool commands. Normal
   canonical writes are also paused until completion. Do not rely on fixing
   migration gaps after freezing.
5. Repeat inspection, then run
   `fleet-db-transition pool-cutover --organization-id=<internal-id> --action=complete`.
   The transaction rechecks completeness under the source locks and writes the
   completion marker only with zero gaps. Repeating completion is safe.
6. Start the coordinated PR4 UI/backend and migrated integrations. Verify
   configuration reads and a deliberately reviewed update, then confirm the
   completed marker and expected controller identities.

This uses a controlled writer freeze, not online per-device conversion. The
source-table SHARE locks remain deliberately in place to exclude inventory,
ownership and queue races during conversion. Lock acquisition times out after
five seconds; import transactions are bounded to two minutes and gate transactions
to thirty seconds. Firmware collection happens before the import transaction.
A timeout rolls back that transaction; earlier committed imports remain. If
measured volume exceeds these bounds, redesign/rehearse batching before deployment
rather than relaxing locks on a live fleet.

If import or freeze readiness fails, resolve gaps using step 3 before retrying;
no freeze is created on rejection. If completion fails after a successful freeze,
keep writers stopped and investigate the reported change. To abandon maintenance,
restore the coordinated pre-maintenance backup before restarting the old deployment.
After new canonical writes, rollback requires a coordinated restore or a forward fix;
schema down refuses to erase effective intent or the maintenance marker.

Backend consumer changes in this release:

| Consumer | Behavior |
| --- | --- |
| Fleet CLI `pools` commands | Use `PoolConfigurationService`; assignment/edit/retry use review then confirmation. Structured pool stacks/targets use `--json`; confirmation passwords use `--password-stdin`. |
| Foreman import | The UI imports groups, racks, assignments, and names with pool import disabled, and explains that pools need separate configuration. Foreman does not return pool passwords; explicit API requests to import pools still fail before creating entities. |
| Legacy pool-ID/raw pool update and worker-name pool rewrite | Available before maintenance; rejected after freeze. Use a reviewed canonical configuration update after cutover. |
| Existing UI and legacy RPC definitions | Retained for the deployment transition; PR4 switches callers and removes endpoints. |

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
   The command also updates `server/migrations/LATEST`, a single line containing
   the highest migration's full name without `.up.sql` or `.down.sql`. Commit it
   with the pair. Branches that add different migration names change the same line.
   Git reports a merge conflict even if those branches chose the same number.
   To resolve the conflict, give the unmerged, undeployed pair a number higher
   than the highest on current main, then update `LATEST` to match.
   Never renumber merged or deployed SQL.
   Migration hygiene rejects missing or stale markers, duplicate versions and
   mismatched up/down pairs. Public promotion does not use this internal allocation
   marker. The marker is developer metadata; the database runner does not read it.
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

### Migration 1057 collision recovery

Device-write outcomes merged first at 1057. Topology reference planning then
merged a second up/down pair at the same version. This duplicate prevents both
the migration source and cloud release metadata from loading; a later corrective
migration alone cannot run.

Keep device-write outcomes at 1057 and move the topology reference pair to 1058
with both SQL files byte-for-byte unchanged. The history checker permits only
this exact rename, pinned by SHA-256 checksums. Fresh databases and databases at
1056 apply both migrations in order. A database at the device-write 1057 checkpoint
keeps its recorded outcomes and applies the cleanup fix at 1058.

A database that applied the standalone topology-reference branch at 1057 has a
different history and requires separate reconciliation. Do not force its version
or assume the rename supplies the missing device-write column. Staging was still
at 1052 before this repair; deployment and the cleanup trial remain separate steps.

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

## Provisional hosted reporting evidence (1055)

Stop telemetry, allocation and scheduled database writers before applying
migration 1055. It installs a transactional hold on new normalized hosted-history
samples and records its capture epoch without backfilling existing data.
Rehearse the upgrade on a representative restored backup to size the maintenance
window and lock capacity: the seven telemetry/historical table locks include all
existing chunks, and the hashboard metrics trigger is reinstalled on each chunk.
Fresh-database tests do not qualify that upgrade footprint.

A pre-1055 binary rejects the 1055 checkpoint. Once capture begins, the populated
hold refuses downgrade; returning to an older build requires the coordinated
pre-upgrade database backup and matching application, losing post-backup writes.

The hold is provisional and has no automatic deletion policy. Production rollout
needs an operations owner and a reviewed interim storage/backup budget. Since
`fleetd` applies current migrations on startup, keep builds containing 1055 out
of hosted releases and promotion until that approval and the restore rehearsal
are recorded. The down migration refuses to discard a populated hold; prefer fixing forward and retain
coordinated backups. Inspect capture and storage using the
[reporting evidence runbook](hosted-reporting-evidence.md).
