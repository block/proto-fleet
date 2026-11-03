# Hosted reporting evidence hold

Fleet preserves newly admitted hosted monitoring samples separately from chart
history. The hold is provisional: it records the normalized observations Fleet
received, not certified contractual uptime, pool connectivity, metered energy,
commercial coverage, or complete polling coverage. Observations rejected before
normalization and sources outside that stream are not retained. Reporting
requirements and the proposed retention policy remain tracked in
[issue 330](https://github.com/btc-mining/proto-fleet-internal/issues/330).

## What is preserved

Migration `001055_internal_hosted_reporting_evidence` starts the hold when it is
applied. It drains source writers before recording the capture epoch. It does not
copy existing chart samples, import averages, or infer past ownership from
current allocations. A later correction can contain an older source time; its
capture time still belongs to the new hold.

The existing hosted-history admission rules check each metric's source window
against its allocation and site before writing a normalized sample. The hold
copies those admitted samples transactionally. If the hold write fails, the
source transaction fails too; the observation is not silently accepted without
its evidence.

The hold contains:

- `hosted_account_reporting_evidence_epoch`: the prospective capture boundary.
- `hosted_account_reporting_allocation_snapshot`: one immutable identity snapshot
  per captured allocation interval, including organization, client, subclient,
  site, asset, device identifier and allocation start. It omits an allocation end
  that may only become known later.
- `hosted_account_reporting_evidence`: ordered events containing the normalized
  value, nullable completeness, metric and source identity, source window,
  original row time, collection time, capture time and transaction identity.
- `hosted_account_reporting_capture_status`: an internal operations view of
  active allocation intervals, with one row per expected source/metric stream,
  including streams with no captured observation.

The metric codes retain the chart stream's meanings: `1` is hashrate in H/s,
`2` is power in W, `3` is temperature in degrees C and `4` is operational state.
State values use the existing hosted-history mappings.
`NULL` values remain missing observations; they are not converted to zero. Source codes are
`1` for miner metrics, `2` for miner state, `3` for module metrics and `4` for
module state, `5` for hashboard metrics and `6` for hashboard state.
New allocations use hashboard streams; module streams retain their historical identities.

Source time, collection time and capture time have different meanings. Some
source rows provide only one observation timestamp, so their stored collection
time is the existing normalization's fallback. Capture time is the database time
at which Fleet retained the event. It does not establish when the controller
measured it or certify an unavailable original receipt time.

## Corrections and retention

Events record `INSERT`, `UPDATE` and `DELETE` of normalized chart samples. An
update keeps both the before and after images. The normal source correction path
often deletes the prior normalized samples and inserts replacements; transaction
identity and ordered event IDs preserve that sequence and the original values.
`transaction_id` is a cluster-local correlation value, not a durable unique key.
A logical dump/restore can reuse transaction IDs from retained events. Downstream
reporting must use event IDs for event identity and carry the database/restore
generation when correlating transactions across a restore. Capture timestamps
help locate the sequence in time, but differ between images in one transaction
and are not themselves a unique transaction-group key.
A retry may repeat that sequence without changing the observation's value.
Topology-only hashboard stamps do not re-normalize measurements or add evidence
events. Updates to measurement values, source timestamps and allocation identity
still follow the correction path, including missing values and compressed history.
Event IDs order appends within a transaction; they do not establish global
commit order or gap-free coverage. Rolled-back transactions can consume IDs.

A normalized-sample deletion is not proof of a raw source deletion, a contractual
retraction or an outage. Timescale chunk expiration does not fire row deletion
triggers, so it removes the chart samples without manufacturing deletion events
in the hold. Already retained events survive independently.

Retained snapshots, events and the epoch reject ordinary `UPDATE`, `DELETE` and
`TRUNCATE`. These guards protect application operations; a database administrator
can still alter the schema, drop tables or restore a backup. They do not replace
backup controls or access restrictions. There is no automatic retention or
compression job for this interim hold. The down migration refuses to discard a
populated hold.

## Inspect capture

Use the existing host database operations access. The status view is internal;
it does not grant a hosted-account user direct database access or create a new
client export.

```sql
SELECT *
FROM hosted_account_reporting_capture_status
ORDER BY organization_id, interval_id, source_id, metric;
```

Each allocation exposes hashrate, power, temperature and state independently.
A recent state observation does not advance metric freshness. Streams with no
observation have unavailable capture/source fields; `last_observation_value`
retains missing values and `last_observation_complete` retains the nullable
metric completeness flag. State and temperature have no completeness flag.
Inspect the latest event operation separately from the latest observation: a
delete must not count as fresh monitoring. Compare each stream's source and
capture times with poll health and ingestion errors; capture alone does not
establish contiguous coverage.

Inspect storage growth before and after a representative capture interval:

```sql
SELECT clock_timestamp() AS measured_at,
       pg_total_relation_size(
         'hosted_account_reporting_evidence') AS evidence_bytes,
       pg_total_relation_size(
         'hosted_account_reporting_allocation_snapshot') AS allocation_bytes;
```

Measure event growth and ingestion latency on the actual hosted estate, including
corrections.

## Local capture cost

`TestStoredContainerRollupsReportingEvidenceRepresentativeScale` compares the
same allocated 96-module CT1 stream with the hold disabled and enabled. Run it
from `server/` with the normal test database settings:

```sh
DB_PASSWORD="${DB_PASSWORD:-fleet}" go test ./internal/infrastructure/timescaledb \
  -run '^TestStoredContainerRollupsReportingEvidenceRepresentativeScale$' -count=1 -v
```

Both modes use the same narrowed normalization path; only the evidence capture
trigger is removed for the disabled mode and restored for the enabled mode.
One local Docker run with 96 modules and 288 allocated hashboards used 20 polls
per mode. It averaged 122 ms with capture disabled and 139 ms with capture enabled
(p95: 141 ms and 145 ms). Capture retained 1,152 events per poll: three normalized
metrics and one state observation per board, without topology-stamping
corrections. Topology stamping updates placement references without re-normalizing
the measurements; genuine measurement corrections still retain their images.
Storage averaged approximately 294 bytes/event including indexes and allocation
snapshots. A 288-board metric correction still added 1,728 events in 70 ms;
inspecting all 1,152 status streams took 5.5 ms. This sequential fixture excludes
WAL, backups, concurrent ingestion and production storage. Measure those costs
and rehearse restore on the actual estate before setting the storage budget.

## Production rollout

`fleetd` applies every current migration during startup. Builds containing 1055
must remain out of hosted release artifacts and deployment promotion until the
operations owner has approved the storage/backup budget, alert routing and restore
rehearsal. Applying 1055 starts capture immediately; installing a new binary is
not a preparation-only step. Record that approval in the release/deployment record.

Rehearse the upgrade on a representative restored backup to size the maintenance
window and PostgreSQL lock capacity. The seven telemetry/historical table locks
include their existing chunks, and the hashboard trigger is reinstalled on every
chunk. A fresh-database test does not measure this footprint.

Apply the migration with telemetry and allocation writers
stopped, following the repository's normal migration procedure. Resume writers
only after the migration commits. Record the epoch, check capture on an allocated
miner and hashboard, inspect source-ingestion errors, and measure storage growth.
Verify backup and restore behavior on a disposable restored database before
using retained evidence operationally.


A pre-1055 binary rejects the 1055 database checkpoint at startup. The down
migration is available only while the hold is empty. Once capture has populated
it, returning to that binary requires the coordinated pre-upgrade database
backup and its matching application build; that restore loses observations
written after the backup. Keep writers stopped during a coordinated rollback.
Prefer fixing forward while preserving the ledger.

## Storage alerts required before release

Use a host-operations alert, routed to the named owner, to sample this query at
least every five minutes. Retain the samples in the deployment's monitoring
system so growth is measured across evaluations, rather than inferred from row
counts or the development benchmark:

```sql
SELECT clock_timestamp() AS measured_at,
       pg_total_relation_size('hosted_account_reporting_evidence')
       + pg_total_relation_size('hosted_account_reporting_allocation_snapshot')
       AS evidence_bytes;
```

Set warning and critical thresholds to 80% and 90% of the approved evidence
storage budget, sustained for ten minutes. Also warn when the measured 24-hour
growth rate projects reaching that budget within seven days; choose a larger
lead time when the capacity or backup process requires it. Treat missing samples
or query errors for fifteen minutes as an alert. Record the budget, projected
growth, contact and a delivered test notification in the deployment record.
These are release prerequisites; this runbook does not provision a deployment's
budget or notification destination. The existing system-monitoring disk warning
and critical alerts remain required for the database filesystem, including WAL
and other tables, which this query does not include.

At the measured 1,152 events/poll and 294 bytes/event, a ten-second example cadence
is roughly 2.9 GB/day per fully allocated CT1 before WAL, backups and corrections.
Use the deployment's actual cadence and measured growth to set the budget.
When an alert fires, add capacity or pause capture using the procedure below;
never delete retained evidence to recover space.

## Pause and resume capture

This is a database-owner maintenance operation, not an application feature flag.
It preserves existing evidence and the original epoch, but creates an explicit
gap. Record a conservative gap interval in the deployment's reporting/restore
record, starting when writers are stopped for pause and ending only after resume
has committed and writers restart. Reports spanning that interval must treat
capture as unavailable. The epoch and status view do not encode pause windows;
old status rows do not establish continuity, and resume does not backfill the gap.

Stop telemetry, allocation and scheduled writers and wait for active writes to
drain. Record the current checkpoint, epoch and last event ID. Pause in one
transaction; abort on a lock timeout and investigate before retrying:

```sql
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE device_metrics, miner_state_snapshots, container_module_metrics,
 container_module_diagnostic_observation, hashboard_metrics,
 hashboard_diagnostic_observation, hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;
SELECT started_at AS hold_started_at,
       (SELECT max(event_id) FROM hosted_account_reporting_evidence) AS last_event_id
FROM hosted_account_reporting_evidence_epoch WHERE singleton;
DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample;
COMMIT;
```

Restart writers only after commit. Chart normalization continues, while the
retained ledger stops growing. Use `DROP TRIGGER`, not `DISABLE TRIGGER`:
TimescaleDB can enable a disabled parent's trigger on newly created chunks.
Do not alter the immutable tables, reset the epoch or run the down migration.
The capture trigger remains absent across ordinary application restarts because
1055 has already been applied; verify it after any later schema upgrade.

Before resuming, the owner must approve the revised storage/backup budget and
verify the alerts. Stop and drain writers again, then restore capture atomically:

```sql
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE device_metrics, miner_state_snapshots, container_module_metrics,
 container_module_diagnostic_observation, hashboard_metrics,
 hashboard_diagnostic_observation, hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;
CREATE TRIGGER record_hosted_reporting_evidence
 AFTER INSERT OR UPDATE OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION record_hosted_reporting_evidence();
COMMIT;
```

Restart writers and verify a new miner or hashboard observation appends evidence,
the original epoch is unchanged, and the storage-growth monitor is receiving
samples. Record the gap's end; do not certify uninterrupted coverage across it.
