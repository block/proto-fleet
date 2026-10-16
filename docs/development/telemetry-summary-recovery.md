# Telemetry summary recovery and retention

Migration 1027 protects miner, CT1 container, tank, module, and hashboard history
after summary-job interruptions. Twelve summary streams cover hourly/daily
measurements for all five equipment types plus miner status. Miner and container
streams share `device_metrics`; tank, module, and hashboard streams each have a
separate raw table. Historical controller ingestion (#179) remains separate.

## Coverage and late data

`telemetry_summary_progress` stores a contiguous verified interval
`[coverage_start, completed_through)` for each aggregate. A successful recent
refresh or the newest materialized bucket does not prove older coverage.
Catch-up force-refreshes a bounded interval before advancing its checkpoint.
Hourly batches initially contain 24 hourly buckets; daily batches contain one
UTC day. Interval arithmetic uses fixed hours, including across daylight saving
time changes. A failed or interrupted refresh leaves its checkpoint unchanged,
even if Timescale committed materialized data before the acknowledgement failed.

Hourly buckets become final after 24 hours; daily buckets after 168 hours.
Independent recent tasks continue during catch-up, refreshing hourly summaries
every 30 minutes and daily summaries every 6 hours. They visit newest buckets
first, then work backward through the late-data window. Recent refreshes use
Timescale invalidations; catch-up forces recomputation. Both exclude the current
incomplete bucket and retain the former one-hour/hourly and one-day/daily end
offsets. Existing real-time queries can include the raw tail.

`telemetry_summary_write_gate.frozen_through` is the source's published write
boundary. Before each raw INSERT/UPDATE statement, a trigger takes a shared
transaction maintenance lock, reads the gate once, and holds its shared row lock
until the writer commits. Each row then uses the cached boundary: stale rows are
skipped, including duplicates and cached board observations, without aborting
fresh readings in the same container transaction. Updates cannot remove a frozen
old row by changing its timestamp.

Before a catch-up refresh, maintenance commits retention and retry bookkeeping,
then advances the gate to that batch's end. Publishing waits for earlier writers
to commit; the worker commits the new gate before refreshing. Consequently, the
refresh includes every accepted earlier write, and later writers cannot change
its frozen interval. Even a writer whose transaction began before an hourly
boundary follows the stored gate. The gate has no separate wall-clock horizon:
its bounds come from batches computed using each tier's configured `lateness`.
A failed refresh can leave the gate ahead of its checkpoint; raw stays pinned
until recovery succeeds. Recent refreshes do not certify or advance the gate.

Historical imports, raw DELETEs, manual chunk deletion, and independent
refresh/deletion jobs require maintenance coordination. Never refill or refresh
buckets whose complete raw history has expired: that can replace retained
summaries with an incomplete subset. The development seeder uses the coordinated
import path described below.

## Scheduling and expiration

Four `maintain_telemetry_source` jobs replace the twelve independent refresh jobs
and four retention jobs. Each source has exactly one registered canonical job.
Its worker attempts due retention first, then one due refresh quantum. A durable
work queue orders tasks by due time. Before refresh commits internally, the worker
moves that task's retry time five minutes forward, allowing siblings to run even
when one task fails. Successful catch-up batches rejoin the queue behind other
due tasks. Jobs start 45 seconds apart; deployment tuning includes these custom
jobs independently of the built-in policies. Deployment tuning preserves their
adaptive schedules instead of postponing catch-up. A successful worker requests its next run
when work or retention is due, with a ten-second minimum delay. Idle workers do
not wake every minute.

Timescale serializes scheduled executions of one job. Foreground `run_job` calls
bypass that scheduler, so this procedure treats them as asynchronous requests:
it requests an immediate canonical background run and returns. Calling a paused
job raises an error and preserves the pause; enable it explicitly to resume.
Only the registered background worker performs routine refresh/deletion.

Canonical workers hold a shared session maintenance lock across refresh commits.
They run in dedicated backends that exit and release that lock on failure.
Writers take the shared transaction counterpart; development imports take the
exclusive session counterpart on their own dedicated connection. Foreground
`run_job` dispatchers acquire no session lock, so an idle manual caller cannot
pin maintenance. This requires no durable backend claims or cross-role activity
checks. `telemetry_summary_step`, `retain_telemetry_raw`, and the seed preparation
function are private worker/import routines with PUBLIC execution revoked.

A pending marker survives refresh transaction boundaries. The next worker records
an unacknowledged attempt as a failure and halves its bucket count, rounding down
to a minimum of one whole bucket. Daily work cannot shrink below one UTC day.
Jobs allow up to 30 minutes per execution, rather than retrying a fixed day under
a five-minute limit. After three failures at minimum size, that task becomes
`blocked` and requires operator intervention; other tasks and retention attempts
continue. Invalid refresh bounds also block only their affected task, preserving
retention bookkeeping. Acknowledged success resets the minimum-size failure
streak. After three successive batches taking less than one minute each, the
bucket count doubles up to the original one-day maximum, restoring throughput
after transient failures.

Retention runs hourly within each source job, and completion of catch-up requests
an earlier attempt. It deletes at most 32 whole chunks fully contained in every
dependent stream's verified interval and older than 240 hours. Incomplete coverage
defers deletion. Deletion failures are recorded and retried after five minutes. Each source stores
its raw target in `raw_retention`; deletion and health views use that same value.
Miner/container/tank/module/hashboard summary retention remains three months for
hourly summaries and three years for daily summaries, with whole-chunk rounding.
**Miner status summaries retain their existing absence of a retention policy.**

## Upgrade and existing gaps

Stop application writers and scheduled database maintenance for migration 1027,
as for the earlier private maintenance migrations. Rehearse the upgrade on a
recent database copy. It removes the old bootstrap jobs and raw retention/refresh
policies, captures raw boundaries, and installs the replacement maintenance.
Unrelated aggregates and compression policies remain in place. Do not restart old
bootstrap procedures or re-add independent raw-retention policies.

Recovery starts at the first complete bucket supported by retained raw chunks,
rounded upward separately for each tier. Earlier history is unverified: existing
summaries remain available, but missing summaries cannot be assumed recoverable.
A partially retained first UTC day is never refreshed as a complete day. Remaining
raw chunks before the common coverage start stay pinned for operator review;
later verified chunks expire normally. Empty sources capture the UTC day boundary
at migration time so both tiers include the first day's live samples even when
ingestion starts before the first maintenance run.

The migration cannot recover previously deleted data. It does not infer coverage
from materialized buckets or destructively repair pre-existing gaps. The down
migration refuses to remove these safeguards; an application rollback leaves the
additive schema in place. Fix forward or disable the replacement jobs while
preserving raw evidence.

## Monitoring and recovery

Inspect both health views as the database maintenance owner:

```sql
SELECT aggregate_name, work_kind, completed_through, catchup_lag,
       batch_buckets, successful_batches, pending, failures, blocked, last_error,
       work_last_success_at, next_run_at, job_id, last_run_status, next_start
FROM telemetry_summary_health ORDER BY aggregate_name, work_kind;

SELECT source_name, completed_through, frozen_through, dropped_through, retention_deferrals,
       eligible_raw_chunks, awaiting_summary_chunks, unverified_raw_chunks,
       retention_error, next_retention_at, last_attempt_at, last_retention_at
FROM telemetry_retention_health ORDER BY source_name;
```

Alert immediately on blocked tasks or retention errors. Alert on overdue recent
tasks without success (allow the normal 30-minute/6-hour cadence and runtime),
catch-up lag that is not decreasing, or increasing failures. `pending` is normal
while a worker is refreshing; correlate long-lived pending markers with job stats
and the 30-minute runtime limit.

Alert separately on growing `eligible_raw_chunks` (deletion backlog), growing
`awaiting_summary_chunks` (coverage backlog), and changes to
`unverified_raw_chunks` (upgrade exceptions requiring review). A fixed pinned
partial upgrade day does not imply deletion has stopped. There is no blanket
"all expired chunks must reach zero" alert. Monitor disk and chunk sizes alongside
these counts; preserving raw is backpressure that requires capacity management.

1. Inspect `timescaledb_information.jobs`, `job_stats`, and `job_errors`. Repair
   database, lock, or resource failures. A daily minimum batch still needs enough
   capacity to refresh one whole UTC day; local tests do not establish fleet-scale
   throughput. If necessary, increase the canonical job's runtime after measuring
   the workload, or add database capacity.
2. For a blocked task, reset only its retry state after repairing the cause:

   ```sql
   UPDATE telemetry_summary_work
   SET blocked=false, minimum_batch_failures=0, next_run_at=now()
   WHERE aggregate_name='<affected aggregate>' AND work_kind='<catchup or recent>';
   CALL run_job(<canonical source job_id>);
   ```

   The CALL requests background execution and returns before completion. Monitor
   the views to observe completion; an idle manual session holds no maintenance
   claim. To resume a deliberately paused job, explicitly run
   `SELECT alter_job(<job_id>, scheduled=>true, next_start=>now());`.
   Requests to paused jobs leave their schedule unchanged.
3. Confirm catch-up checkpoints advance, siblings continue, and eligible raw
   chunks decrease. Do not advance checkpoints manually or use `drop_chunks` to
   bypass the coverage fence. If storage becomes urgent, pause ingestion or add
   capacity while preserving evidence.

Unverified upgrade chunks require a reviewed, data-specific disposition. Keep
them while reconciling complete history from another source, or explicitly accept
the historical loss and remove only the reviewed chunks. Do not relabel incomplete
history as verified coverage. These exceptions are not routinely auto-deleted.

## Development seeding and cleanup

`server/devtools/seedtelemetry` takes the exclusive maintenance lock on a dedicated
connection. Raw writer statements for `device_metrics` wait while it runs; use it
on development databases. It preserves the canonical job's enabled/paused state.

Before changing data, the tool reopens a supported UTC range and rewinds every
dependent checkpoint, including container summaries. Raw remains pinned after a
partial failure. A source with no raw, no dependent summary rows, and no recorded
raw expiration may extend its initial boundary for its first seed. Otherwise,
imports use only complete retained UTC days: older requested samples are omitted
with a log message. Existing seed rows outside that safe range cause an error
before deletion. Previously expired or partially retained history is preserved.

Replacement deletes and all insertion batches run in one transaction, so a
failed batch restores the previous seed rows. After commit, the tool runs the
same bounded catch-up/recent tasks as scheduled maintenance until due work is
complete. Cleanup likewise rewinds coverage before deletion and rebuilds through
recovery, without uncoordinated manual refreshes. Refresh failure preserves raw
and unfinished progress; rerun after repairing the cause, or resume maintenance.
Closing the command's connection releases its session lock, including on errors.

Real Timescale regressions cover all twelve streams after long outages, shared
source coverage gates, fair work rotation, recent materialization during catch-up,
adaptive retries, blocked minimum batches, worker timeouts, restricted-role manual
requests, bounded chunk deletion, DST, in-flight writers, stale board samples, coordinated seed replacement/cleanup,
and preservation of expired/partial history.
