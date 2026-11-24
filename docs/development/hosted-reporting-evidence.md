# Hosted reporting evidence hold

Fleet keeps durable, append-only hourly evidence for every allocated miner,
module and hashboard, separately from the mutable chart history. The hold is
provisional: it records what Fleet's normalized monitoring samples showed for
each hour. It does not certify contractual uptime, pool connectivity, metered
energy, commercial coverage or complete polling coverage. Observations rejected
before normalization and sources outside the hosted-history stream are not
included. Reporting requirements and long-term retention are tracked in
[issue 330](https://github.com/btc-mining/proto-fleet-internal/issues/330).

Migration `001077_internal_hourly_reporting_evidence` created this hold. It
replaced the raw per-poll hold from migration 1055, whose events it deleted.

## What is preserved

The hold starts at `hold_started_at`, the first full UTC hour after 1077 was
applied. Nothing earlier is backfilled. A scheduled job closes each hour once it
has ended, plus a grace period, and writes one row per allocated interval for
that hour. Storage therefore depends on the number of allocated intervals, not
on the poll cadence.

| Object | Contents |
|---|---|
| `hosted_account_reporting_hour` | One row per allocation interval, closed hour and revision. Append-only. |
| `hosted_account_reporting_allocation_snapshot` | One immutable identity snapshot per interval with evidence: organization, client, subclient, site, kind, asset, device and allocation start. Created by 1055 and kept. |
| `hosted_account_reporting_hour_gap` | Hour ranges the closer will never write. Append-only. |
| `hosted_account_reporting_hour_control` | The single closer state row: `hold_started_at`, `closing_through`, `closed_through`, `last_success_at`. |
| `hosted_account_reporting_hour_marker` | Queue of closed interval-hours waiting to be recomputed after a correction. Rows are deleted as the job processes them. |
| `hosted_account_reporting_capture_status` | Internal operations view: one row per expected stream of each current or recently ended interval. |

`hosted_account_reporting_hour`, `hosted_account_reporting_hour_gap` and the
allocation snapshot reject `UPDATE`, `DELETE` and `TRUNCATE`. These guards stop
application writes. A database administrator can still alter the schema, drop
tables or restore a backup, so they do not replace backups or access controls.

### Identity and revisions

A row is identified by `(interval_id, bucket, revision)`. `bucket` is the start
of a UTC hour, matching `hosted_account_history_hourly`. Kind `5` is a miner,
`6` a module and `7` a hashboard. A reallocation, resale or later change to an
interval's bounds never rewrites or moves a past row.

Revision `1` is written when the hour closes. If a correction changes anything
in a closed hour, the job appends revision `N+1` with its own `captured_at`.
Earlier revisions stay. Readers use the highest revision. A published report
can keep citing the exact `(interval_id, bucket, revision)` it used.

### What a row contains

Each metric has the aggregates that `hosted_account_history_hourly` computes
from the same normalized samples, plus a report count:

- `<metric>_report_count`: samples received in the hour, including samples whose
  value was missing.
- `<metric>_count`: samples with a value. `hashrate_positive_count` counts
  hashrate values above zero.
- `<metric>_sum`, `<metric>_complete` (hashrate and power), and
  `<metric>_source_oldest_at` / `<metric>_source_newest_at`.
- `temperature_latest_value` / `temperature_latest_at` and `state_latest_value` /
  `state_latest_at`: the last value in the hour.

Missing data stays missing. A silent hour has zero report counts and `NULL`
sums, never zeros. An hour where the device reported a missing value has a report
count above zero, a count of zero and a `NULL` sum. Completeness flags are
copied unchanged.

Metric codes are `1` hashrate (H/s), `2` power (W), `3` temperature (degrees C)
and `4` operational state, using the existing hosted-history state mappings.
Source codes are `1` miner metrics, `2` miner state, `3` module metrics, `4`
module state, `5` hashboard metrics and `6` hashboard state.

Other columns:

- `window_start`, `window_end`: the part of the hour the interval covered when
  the row was captured. Both are `NULL` when the interval no longer overlaps the
  hour, for example when samples were attributed to it before its end was moved
  earlier. Sums are not clipped to the window.
- `no_source_data`: `true` when none of the interval kind's sources delivered any
  sample at the interval's site, within its organization, in that hour.
  Intervals without a site form one group per organization. Read it as a
  Fleet-side ingestion outage, not as device silence. A later backfill that
  adds data appends a revision with the flag cleared.
- `capture_reason`: `closed` for rows written when the hour closed and their
  revisions. `unattributable` for interval-hours that first got a row after the
  hour had closed, because the interval was created, or its start moved, behind
  the closer. Samples are assigned to an interval only when they are ingested, so
  these rows cannot show what the device did in that hour.

## How hours close

The TimescaleDB job `hosted_reporting_hour_job` runs every 15 minutes, with a
10-minute `max_runtime` and a 5-minute retry period. Each run has three steps,
and each step commits on its own:

1. Begin close. Pick the newest hour that ended at least `grace` ago, at most
   `max_hours` past `closed_through`, and publish its end as `closing_through`. This
   step briefly takes a `SHARE ROW EXCLUSIVE` lock on
   `hosted_account_history_sample` and `hosted_account_asset_interval`, waiting
   at most `lock_timeout_ms` in total for in-flight sample and allocation
   writers. Any
   writer that commits afterwards sees the new boundary.
2. Finish close. Write revision `1` for every interval-hour in
   `[closed_through, closing_through)`: every interval overlapping the hour, plus
   every interval with samples in it. Then advance `closed_through` and set
   `last_success_at`. If this step fails, `closed_through` stays behind
   `closing_through` and the next run resumes.
3. Revise. Take up to `revision_batch` marked interval-hours, oldest first,
   recompute them and append a revision where the result differs from the latest
   revision.

Writers never write evidence. Triggers on `hosted_account_history_sample` and
`hosted_account_asset_interval` add a marker for each closed or closing hour
that a sample insert, update or delete, or an interval insert or bound change,
affects. Markers are added only for hours inside the hold and the correction
window. A source correction usually deletes and re-inserts each sample, so one
corrected sample adds two markers. The revise step handles all markers of an
interval-hour together.

### Job configuration

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `true` | When `false` or missing, each run returns without doing anything, so change settings with `config \|\| ...` rather than replacing the config. |
| `grace` | `30 minutes` | Time after an hour ends before it closes. |
| `correction_window` | `9 days` | How far back corrections are recomputed. It can be shortened but not lengthened. |
| `max_hours` | `24` | Most hours closed in one run. |
| `lock_timeout_ms` | `1000` | Longest total wait for the sample and interval table locks in step 1. |
| `revision_batch` | `10000` | Most interval-hours recomputed in one run. |

### Correction window and gaps

Raw samples are kept for 10 days. The job recomputes corrections only for hours
inside the 9-day correction window, so it never reads a partly expired hour.
A correction to an older hour adds no marker and no revision.

If the closer falls more than the correction window behind, for example because
the job was disabled or failing for over nine days, it does not close the expired
hours. It records one gap row covering them, with reason
`closer fell behind past the correction window`, and continues from the window
start. Gap hours never get evidence rows. Reports must treat them as unavailable.

## Inspect capture

Use the existing host database operations access. These queries are internal;
they do not grant hosted-account users database access or create a client export.

Closer state. `closed_through` and `closing_through` are exclusive hour ends:
hours before `closed_through` have evidence or a gap.

```sql
SELECT hold_started_at, closing_through, closed_through, last_success_at,
       now() - closed_through AS lag
FROM hosted_account_reporting_hour_control;
```

Job state and recent failures:

```sql
SELECT j.job_id, j.scheduled, j.config, s.last_run_status,
       s.last_successful_finish, s.next_start, s.total_failures
FROM timescaledb_information.jobs j
LEFT JOIN timescaledb_information.job_stats s USING (job_id)
WHERE j.proc_name = 'hosted_reporting_hour_job';

SELECT start_time, finish_time, sqlerrcode, err_message
FROM timescaledb_information.job_errors
WHERE proc_name = 'hosted_reporting_hour_job'
ORDER BY start_time DESC
LIMIT 20;
```

Correction backlog. The revise step should drain it within a run or two:

```sql
SELECT count(*) AS markers,
       count(DISTINCT (interval_id, bucket)) AS interval_hours,
       min(bucket) AS oldest_hour,
       min(enqueued_at) AS oldest_marker
FROM hosted_account_reporting_hour_marker;
```

Streams that need attention:

```sql
SELECT organization_id, interval_id, kind, source_id, metric,
       has_observation, last_observation_bucket, last_closed_bucket,
       last_closed_no_source_data, missing_hours, gap_hours
FROM hosted_account_reporting_capture_status
WHERE missing_hours > 0 OR gap_hours > 0 OR NOT has_observation
      OR last_closed_no_source_data
ORDER BY organization_id, interval_id, source_id, metric;
```

The view has one row per expected `(interval, source, metric)` stream of open
intervals and intervals that ended inside the correction window:

- `closer_lag_seconds`, `closing_through`, `closed_through`,
  `closer_last_success_at`: the closer state, repeated on every row.
- `has_observation` and `last_observation_*`: the most recent hour inside the
  correction window whose latest revision has a value for this stream's
  metric. A stream whose last value is older than the window reads as not
  observed. Correcting an older hour does not move it. A state observation
  does not make a metric fresh.
- `last_closed_bucket`, `last_closed_revision`, `last_closed_no_source_data`:
  the interval's newest hour row.
- `missing_hours`: hours inside the hold and the correction window, up to
  `closed_through`, that overlap the interval and have neither a row nor a gap.
  This should be zero.
- `gap_hours` and `last_gap_*`: gap hours overlapping the interval, at any age.

`missing_hours` and `gap_hours` describe the interval, so every stream row of an
interval repeats them. Count them once per `interval_id` when totaling.

Latest revision of each hour for one interval (replace `42`):

```sql
SELECT DISTINCT ON (bucket) *
FROM hosted_account_reporting_hour
WHERE interval_id = 42 AND bucket >= now() - INTERVAL '1 day'
ORDER BY bucket, revision DESC;
```

Gaps:

```sql
SELECT gap_id, from_bucket, to_bucket, reason, recorded_at
FROM hosted_account_reporting_hour_gap
ORDER BY from_bucket;
```

## Operations

### Disable and enable the job

Disabling stops closing hours and processing corrections. Writers keep adding
markers for hours that were already closing or closed. Evidence already written
is untouched.

```sql
SELECT alter_job(job_id, config => config || '{"enabled": false}')
FROM timescaledb_information.jobs
WHERE proc_name = 'hosted_reporting_hour_job';
```

To enable it again, set `enabled` back to `true`. The next scheduled run closes up
to `max_hours` hours, so a closer that is far behind catches up over several
runs. To run it now:

```sql
SELECT alter_job(job_id, config => config || '{"enabled": true}')
FROM timescaledb_information.jobs
WHERE proc_name = 'hosted_reporting_hour_job';

DO $$
DECLARE
  id integer;
BEGIN
  SELECT job_id INTO STRICT id FROM timescaledb_information.jobs
  WHERE proc_name = 'hosted_reporting_hour_job';
  CALL run_job(id);
END $$;
```

If the job stays disabled or failing for more than nine days, the next run
records a gap for the hours that left the correction window.

Change other settings the same way, for example
`config || '{"revision_batch": 20000}'`. Keep `correction_window` at nine days
unless raw sample retention changes.

### Respond to lag and failures

Hours should be closed through the hour that ended at least `grace` ago. With the
defaults, `closed_through` older than `now() - 1 hour`, rounded down to the hour,
means the closer missed two runs:

```sql
SELECT closed_through,
       closed_through < time_bucket(INTERVAL '1 hour', now() - INTERVAL '1 hour') AS lagging
FROM hosted_account_reporting_hour_control;
```

When it lags, check `job_errors` and the job state above:

- `canceling statement due to lock timeout`: step 1 could not get the sample and
  interval table locks within `lock_timeout_ms`, usually because of a long
  ingestion or allocation transaction. The job retries after five minutes.
  Investigate long-running writers before raising the timeout: new sample and
  allocation writes wait behind the lock request for up to that long.
- `hour ... has left the correction window`: step 2 was asked to finish hours that
  expired while it was behind. The next run's step 1 records the gap and moves on.
- A job that is unscheduled, or `enabled` set to `false`, explains lag without
  errors.

### Gaps and missing hours

A gap is permanent. Record its range in the deployment's reporting record, find
out why the job was disabled or failing, and treat those hours as unavailable in
reports. The hold cannot be backfilled.

`missing_hours > 0` means an interval-hour inside the window has no row and no
gap. Step 1 waits out in-flight allocation writes and later ones leave a marker,
so a missing hour points to an interval written outside that path, for example
with its trigger dropped or by a writer at a stricter isolation level. List the
missing hours of an interval (replace `42`):

```sql
SELECT i.id AS interval_id, m.bucket
FROM hosted_account_asset_interval i
CROSS JOIN hosted_account_reporting_hour_control c
CROSS JOIN LATERAL generate_series(
  GREATEST(c.hold_started_at, hosted_reporting_hour_window_floor(now()),
           time_bucket(INTERVAL '1 hour', i.started_at)),
  LEAST(c.closed_through,
        CASE WHEN i.ended_at IS NULL THEN 'infinity'::timestamptz
             WHEN i.ended_at <= i.started_at THEN time_bucket(INTERVAL '1 hour', i.started_at)
             ELSE time_bucket(INTERVAL '1 hour', i.ended_at - INTERVAL '1 microsecond')
                  + INTERVAL '1 hour' END) - INTERVAL '1 hour',
  INTERVAL '1 hour') m(bucket)
WHERE i.id = 42
  AND NOT EXISTS (SELECT 1 FROM hosted_account_reporting_hour h
                  WHERE h.interval_id = i.id AND h.bucket = m.bucket)
  AND NOT EXISTS (SELECT 1 FROM hosted_account_reporting_hour_gap g
                  WHERE m.bucket >= g.from_bucket AND m.bucket < g.to_bucket)
ORDER BY m.bucket;
```

While such an hour is inside the correction window, queue it for recomputation,
using a `bucket` the query returned. The next run writes it as revision `1` with
`capture_reason = 'unattributable'` if the interval overlaps the hour or has
samples in it:

```sql
INSERT INTO hosted_account_reporting_hour_marker (interval_id, bucket)
VALUES (42, '2026-01-15 09:00:00+00');
```

Queueing a marker is safe for any closed hour: the job appends a revision only if
the recomputed row differs from the latest one.

### Storage alert

Sample this query at least every five minutes from a host-operations alert routed
to the named owner, and keep the samples in the deployment's monitoring system so
growth is measured across evaluations:

```sql
SELECT clock_timestamp() AS measured_at,
       pg_total_relation_size('hosted_account_reporting_hour')
       + pg_total_relation_size('hosted_account_reporting_hour_gap')
       + pg_total_relation_size('hosted_account_reporting_hour_marker')
       + pg_total_relation_size('hosted_account_reporting_allocation_snapshot')
       AS evidence_bytes;
```

Warn at 80% and alert at 90% of the approved evidence storage budget, sustained
for ten minutes. Also warn when 24-hour growth projects reaching the budget
within seven days, or sooner if the capacity or backup process needs more lead
time. Treat query errors or missing samples for fifteen minutes as an alert. The
existing system-monitoring disk alerts remain required for the database
filesystem, including WAL and other tables, which this query does not cover.
When an alert fires, add capacity. Never delete evidence to recover space.

### Release prerequisites

Before a release that contains 1077 reaches a hosted deployment, the operations
owner approves the storage and backup budget, and the lag, job-failure and
storage alerts are routed and tested. Record the budget, contact and a delivered
test notification in the deployment record.

## Upgrade and rollback

Deployments that ran 1055 should keep raw capture paused until 1077 lands, and
record that gap. With writers stopped, pause it by dropping its trigger:

```sql
DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample;
```

Use `DROP TRIGGER`, not `DISABLE TRIGGER`: TimescaleDB can enable a disabled
parent's trigger on newly created chunks. 1077 applies whether raw capture is
paused or running.

`fleetd` applies 1077 at startup. Stop telemetry, allocation and scheduled
writers first, following the normal migration procedure. The migration locks the
same seven telemetry and history tables as 1055, including their chunks, so
rehearse it on a restored backup to size the maintenance window and lock
capacity. It then:

- deletes the raw hold: `hosted_account_reporting_evidence` and its events, the
  1055 epoch table, the raw capture trigger and function, and the old status view;
- keeps the allocation snapshot, the append-only guard function and the 1055
  hashboard history trigger;
- creates the hourly tables, triggers, functions, job and status view, with the
  hold starting at the next full hour.

The first rows appear once the first hour has ended, the grace period has passed
and the job has run: within about three hours of the upgrade.

A binary older than 1077 refuses to start against a 1077 database. The down
migration works only while `hosted_account_reporting_hour` and
`hosted_account_reporting_hour_gap` are empty, that is, before the first hour
closes. It removes the hourly objects and job and restores an empty 1055 raw hold
with a new epoch and its capture trigger active, which resumes raw capture as
soon as writers restart. To keep raw capture paused, drop the trigger after the
down migration and before writers restart:

```sql
DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample;
```

Deleted raw events cannot be restored. Once hourly evidence exists, returning to
an older build needs the coordinated pre-upgrade backup and its matching
application, and loses everything written since that backup. Prefer fixing
forward.

## Sizing

`TestStoredContainerRollupsReportingEvidenceRepresentativeScale` measures one
fully allocated CT1: 6 tanks, 96 modules and 288 hashboards, each allocated as
its own kind-7 interval, polled every 45 seconds. The test skips unless
`PROTO_FLEET_SCALE_TESTS=1` is set, because the run takes minutes. Run it from
`server/`:

```sh
PROTO_FLEET_SCALE_TESTS=1 DB_PASSWORD="${DB_PASSWORD:-fleet}" \
  go test ./internal/infrastructure/timescaledb \
  -run '^TestStoredContainerRollupsReportingEvidenceRepresentativeScale$' -count=1 -v
```

It replays one hour of real polls into 25 closed hours (about 80,640 samples per
hour; half the boards never report temperature), closes one hour, then closes
24 hours in one catch-up run after compressing chunks older than six hours. It
then applies corrections, adds 30 days of earlier evidence rows, and queries the
status view. One local run on an Apple M3 Max with Docker TimescaleDB, excluding
WAL, backups and concurrent load, measured:

| Measurement | Result |
|---|---|
| Ingestion per poll into an open hour, marker triggers removed / installed | 126 ms / 127 ms mean |
| Step 1 (lock, watermark, commit) | 2-3 ms |
| Step 2, one hour (288 rows) | 75 ms |
| Step 2, 24-hour catch-up (6,912 rows, mostly compressed) | 1.8 s |
| One-poll `hashboard_metrics` correction of 288 boards (three samples each) | 1,728 markers |
| One-poll delete-and-reinsert correction of 288 boards (720 samples) in one closed hour | 1,440 markers; revise 128 ms, 288 revisions |
| Same correction in each of 24 closed hours | 34,560 markers; one revise run, 3.0 s, 6,912 revisions |
| One revise run over 7,200 interval-hours (one marker each) | 3.1 s |
| Status view, all 1,152 streams, 31 days of rows | 122 ms |
| Evidence row, heap and primary key | 334.5 bytes (303.7 without temperature) |

At 6,912 rows per day, one fully allocated CT1 adds about 2.3 MB per day, or
about 0.84 GB per year, plus one row per corrected interval-hour and WAL and
backup overhead. Every allocated miner or module interval adds 24 rows per day
on top. Allocation snapshots take about 300 bytes per interval, once. The
retired raw hold measured about 294 bytes per event and 1,152 events per poll,
roughly 650 MB per CT1-day at a 45-second cadence.

A worst-case run for one CT1 (a 24-hour catch-up plus a full 10,000
interval-hour revise batch) is about 7 seconds, far inside the 10-minute
`max_runtime`. Cost grows roughly with the number of allocated intervals; that
extrapolation is not measured. The status view's missing-hour check and its
observation lookup are both bounded by the correction window, so their cost
does not grow with retained evidence. With 8 CT1s allocated to one client
(2,304 intervals), the capture-streams query took about 0.9 s at 31 days of
evidence and 0.7 s at 182 days, against a 5-second timeout; it grows with the
number of intervals.

Measure growth on the actual estate before setting the storage budget, and
rehearse restore on a disposable restored database.

## Report selection and source artifact limits

An account's current reporting facts accumulate through revision-checked bounded
mutations. Preparing and publishing a report query its current and annual
comparison windows, exact commercial support, episode starts and global latest
ticket states, and latest pool observation ties. Unrelated retained history does
not enter the selected report arrays. Preparation and publication serialize on
the account input row, protecting selection, original/correction creation and
publication against concurrent report and input mutations. Report work takes
`FOR NO KEY UPDATE`, input mutations take `FOR UPDATE`, and register readers
take `FOR KEY SHARE`. Register readers retain a consistent input revision while
report rendering proceeds. Their organization, user and permission admission
locks are shared, so mining summaries and report readers can continue. Issued report reads and artifact pages do not lock the report row;
each page still checks live authority.

Selection first reads aggregate metadata. One report may select up to 32 MiB of
stored fact payloads, subject also to a 256 MiB heap reservation: eight times
stored payload bytes, six times identity bytes, 2 KiB per fact, and 8 MiB reserved
for configuration. This is a byte and estimated heap budget, with no fixed fact
count or page-count limit. Input selection, including device locks and external
witness validation, has a 45-second deadline within the existing 60-second report
transaction. Provisional draft writes and audits roll back when any check fails.
Deadline exhaustion returns an actionable failed-precondition error; caller
cancellation retains its cancellation error.

### Evidence cutoff

A report does not copy or link evidence rows. Preparing or regenerating a draft
stores one instant, `hosted_report.evidence_cutoff`, and the report's monitoring
evidence is derived from it: for each allocated interval under the report's
commercial coverage and each hour of the period, the highest revision with
`captured_at <= evidence_cutoff`, plus the hours of every gap with
`recorded_at <= evidence_cutoff` that have no such row. Later revisions and gaps
never enter that report. A correction report takes its own, later cutoff and so
reads them; the original keeps its own. The cutoff is immutable once the report
is issued, and an issued report must have one.

`hosted_report_evidence_cutoff(as_of)` takes the cutoff in its own short
transaction, before the preparation transaction: it locks the hourly control row
`FOR SHARE`, reads `clock_timestamp()`, waits for the clock to advance past that
reading, and returns the earlier of it and the report's `as_of`. The hourly job's
close, revise and gap steps hold the same row `FOR UPDATE` from before their first
insert until they commit, so no evidence transaction is in flight at the cutoff:
everything stamped at or before it is already committed, and everything committed
later is stamped after it. Preparation therefore waits for a running job step
(up to 15 seconds, then fails and can be retried) rather than reading around it.

This depends on two things:

- Every writer of `hosted_account_reporting_hour` and
  `hosted_account_reporting_hour_gap` holds the control row `FOR UPDATE` first. A
  manual insert that skips it can commit a row stamped before an existing cutoff.
- The database clock does not step backwards. If it does, rows captured after a
  report was prepared can be stamped at or before its cutoff and enter a draft.
  Published artifacts are stored bytes and do not change. After a backwards step,
  regenerate open drafts once the clock has passed its earlier reading.

Publication requires the draft's input revision to still be current, so the
commercial coverage that scopes the evidence is the coverage the draft was
calculated from. The evidence archive is written once, at publication.

### Evidence export

The evidence archive holds one row per allocated interval and time, ordered by
`(interval_id, time)`, read one interval at a time.

- A report of at most 500,000 interval-hours (covered allocation intervals times
  the hours of its period) exports `observations.csv`: one row per interval and
  hour, with the revision, capture time and reason, source window, and each
  metric's report count, sample count, sum, completeness, latest state and
  temperature, and source times. A gap hour has reason `gap`, revision 0, the
  gap's reason and recording time, and no observation.
- A larger report exports `observation_days.csv`: the same hours rolled up per
  interval and UTC day, with the number of evidence, gap, revised, unattributable
  and no-source-data hours, summed counts and sums, the day's latest state and
  temperature, and the highest revision and latest capture time. Hourly rows
  remain in the database and can be re-derived from the report's cutoff.

The limit is on size, not period length: an hourly row is about 280 bytes, so
500,000 rows are about 140 MB of the 256 MiB uncompressed archive limit. Two
CT1s export a month by the hour; three export it by day. A year of one CT1 is by
day; a year of 50 intervals is by the hour.

Measured in the development database for one CT1 (288 allocated intervals, every
hour present with all four metrics):

| Period | Rows | Prepare | Publish | CSV | Archive |
| --- | --- | --- | --- | --- | --- |
| 30 days, hourly | 207,360 | 2.1 s | 3.0 s | 58.3 MB | 4.6 MB |
| 365 days, daily | 105,120 (from 2,522,880 hours) | 0.7 s | 10.1 s | 31.0 MB | 2.5 MB |

Day rollups still aggregate every hour inside the 60-second report transaction,
at about 250,000 hour rows per second here, so publication reaches the deadline
near 12 million interval-hours: about five CT1s for a year. Larger reports fail
publication with the deadline or size error; nothing is truncated.

## Known limits

- Corrections to hours older than the 9-day correction window leave no trace.
- Hours of an interval created, or whose start moved, behind the closer are
  written as `unattributable`, because samples are assigned to intervals only at
  ingest.
- Markers rely on writers running at `READ COMMITTED`, as Fleet's do. A
  `REPEATABLE READ` or `SERIALIZABLE` writer whose snapshot predates step 1 can
  miss the new boundary and leave no marker.
- Samples already assigned to an interval stay counted on it after its end is
  moved earlier. The row's window shows the mismatch.
- Step 1 can hold up new sample and allocation writes for up to
  `lock_timeout_ms` while it waits for the locks. A long-running allocation
  transaction can make it time out; the job retries.
- If the revise step falls so far behind that markers leave the correction window,
  those markers are discarded without a revision.
- Evidence rows never expire. Long-term retention belongs to issue 330.
