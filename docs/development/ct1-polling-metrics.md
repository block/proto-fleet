# CT1 polling measurements and stored topology

CT1 collection keeps the existing cadence and reusable native gRPC connection.
Each poll launches four bounded reads: the required container map and independent
mining status, plant/PDU/fan telemetry, and controls. The overall budget is ten
seconds; supplements have at most one second and reserve 500 ms before the outer
deadline. Failed supplements leave their measurements unavailable without
losing valid boards. Poll assembly is serialized independently of the existing
control sequence lock. Mining state, issue classification, and controls retain
their existing rules.

This implements #200. Controller-retained minute history, checkpoints, catch-up,
and firmware-side bucket averages remain #179.

## Measurement definitions

| Equipment | Hashrate | ASIC temperature | Only power source |
| --- | --- | --- | --- |
| Board | Board health, TH/s converted to H/s | Reported board mean | Board `health.power` |
| Module | Sum of reporting installed boards | Mean of installed boards | Freshest shared PSU input, counted once |
| Tank | Sum of accepted boards in this poll | Equal mean of complete module means | Dedicated PDU |
| Container | Sum of accepted boards in this poll | Equal mean of all complete descendant module means | Plant facility meter |

Missing meters never fall back to lower-level sums, mining totals, or plant
`totalPowerW`. Plant total power, inlet/outlet coolant temperatures, fans, and
controls remain separate diagnostic observations. Tank/container temperature
ranges are ranges of module means, not individual ASIC extremes.

Every level saves efficiency in J/H, calculated only for positive hashrate,
complete expected hashrate coverage, and valid dedicated power. APIs convert to
J/TH where appropriate. Inventory, charts, and the browser return the saved
value; they do not calculate a new ratio from displayed or bucketed values.

Confirmed empty slots are excluded from expected boards. Two installed boards
with two readings are complete; an unknown slot is not empty. Module health
classification still requires the existing three-board evidence. Container
completeness requires all six CT1 tanks, each tank's declared layout (16 modules
when omitted), and unambiguous slots/identities. A missing module or rejected
identity marks topology incomplete. Reporting counts describe known expected
contributors; an incomplete map cannot establish the full denominator.
Partial hashrate and temperature values carry coverage, and never establish
complete efficiency. Parent temperature uses complete module means so a
partially reporting module does not silently change weighting.

## Time, presence, and power attribution

A reading must have a source time less than two minutes old and no more than
five seconds ahead of collection. PSU time subtracts `sampleAgeMs` from board
health time. PDU/facility values use the enclosing telemetry timestamp. Firmware
measurement-time guarantees remain an assumption pending confirmation; there is
no separate cross-source skew threshold. Coverage records oldest/newest source
times as well as reporting/expected counts. Collection time is stored separately.

Server ingestion independently rejects zero meter source times and times more
than five seconds ahead of either collection or server receipt. Power coverage
must match the meter's source time. Rejected timestamps cannot advance durable
watermarks; an unavailable or invalid value with a trustworthy timestamp still
preserves ordering, preventing delayed samples from restoring superseded power.

Optional meter zero is a valid measurement. The pinned board hashrate and power
scalars have no presence bits, so their zero defaults remain unavailable. Fleet
cannot distinguish missing from measured zero for those fields; firmware must
add presence or validity. PSU electrical data is a complete snapshot when its
message is present, per the pinned contract.

Until firmware supplies explicit PDU-to-tank attribution, `pdu-N` is associated
with tank N using the naming convention. Available slot, observed, and expected
feed IDs must agree with `pdu-N-outlet-MM` and their module. Configured placement,
wiring conflicts/reasons, duplicate PDU records, or another tank claiming the
same PDU invalidate attribution. Saved provenance explicitly says
`naming_convention`; this is not a firmware-guaranteed exclusive mapping.

Current values expire by source age even when polls continue returning cached
health. An unavailable observation withdraws readings. Latest tables retain
source-ordering watermarks independently so delayed data cannot restore older
values. Board ordering is enforced per durable board; PSU/PDU/facility ordering
is enforced for each meter. A regressed power sample does not discard fresh
board hashrate.

## Persistence and access

Migration 1015 adds typed board power/efficiency, module efficiency/coverage,
saved tank history/latest values, container latest values, and container
semantics/coverage/topology references in `device_metrics`.

Each accepted map produces a `container_topology_observation`, referencing a
content-addressed immutable `container_topology_version`. Normalized topology
includes controller/site, tank/module/slot placement, durable board identities,
empty/unknown slots, completeness, and feed evidence. Changing measurements or
poll times do not create a mapping version. These observations describe when
Fleet observed placement, not its exact physical change time.

The existing telemetry transaction serializes inventory reconciliation and all
board, module, tank, container, topology, and equipment writes. Rejected durable
identities cannot contribute to saved totals. Duplicate/delayed new-format polls
do not append rollups. Partial maps retain omitted hardware without renewing its
observation time. Moves never rebuild past equipment measurements.

Full-access reads use these saved physical measurements. Tank history checks
the sample's recorded site against access scopes. Container chart reads are
conservatively limited to samples from the container's current site, in addition
to the service's authorization. Per-customer subsets and allocated shared power
are future work; these endpoints do not imply partial descendant authorization.

## Semantics and retention

`device_metrics.metric_semantics=1` identifies prior container values (including
share-derived hashrate, fallback power, and coolant temperature). New polls use
2: board-total hashrate, facility-only power, and module-average ASIC temperature.
Container charts select only semantics 2, bypassing old hourly/daily aggregates
that mix definitions. Old rows remain stored under their original meanings;
module PSU history never becomes PDU tank history.

Container, tank, module, and hashboard measurements use the miner retention tiers:

| Tier | Resolution | Retention |
| --- | --- | --- |
| Raw observations | Each accepted poll | 10 days |
| Hourly summaries | One hour | 3 months |
| Daily summaries | One day | 3 years |

Dedicated continuous aggregates keep sums, valid sample counts, minima, maxima,
and conservative coverage for each metric. Averages combine sums and counts;
efficiency is the average of saved poll-time efficiencies, never a new ratio of
averaged power and hashrate. Missing values remain gaps and explicit zeroes remain
measurements. Each aggregate groups by equipment, recorded site, and immutable
topology version. Access filtering happens before combining these groups.
Module observations now also carry explicit site and topology-version provenance.
Existing rows are backfilled only where their original topology observation is
available; legacy values without that evidence do not enter the summaries.

History queries use raw data for windows up to one day within the last ten days,
hourly data for windows up to ten days within the last three months, and daily
data otherwise. Old narrow windows therefore still use a retained tier. Requested
chart widths round up to a multiple of the source resolution. Summary windows
include whole source buckets intersecting the request, including the current
incomplete bucket; boundary averages may include samples outside the exact
request timestamps. Raw reads preserve exact request bounds. Module history keeps
individual raw observations for recent short windows and returns bucket averages
for older or longer windows, with time-based pagination. Module and hashboard
history cursors also record the source resolution. Crossing a tier boundary between
pages returns FailedPrecondition and requires restarting pagination, including when
the new bucket would fall before the cursor. Tokens without a recorded resolution
also require a restart; continuing against a potentially expired tier is unsafe.
Each request resolves its tier once and uses that value for validation and fetching.
Real-time aggregates include the latest raw tail while scheduled materialization
catches up. The existing maximum API request span remains 366 days; older years
can be queried in separate windows.

Migration 1030 supersedes the one-time bootstrap jobs from 1016 and 1021 and
replaces independent refresh/raw-retention policies with persistent recovery.
Every hourly/daily tier has its own contiguous coverage checkpoint. Raw chunks
expire only after every dependent tier has completed that interval; miner metric,
miner status, and container tiers share the `device_metrics` gate. One staggered
job per raw source rotates catch-up and recent refreshes, with smaller batches
after interrupted attempts and gradual growth after fast successes. A shared write
gate waits for in-flight writers before finalization; stale cached readings are
skipped without rolling back live ingestion. Recent refreshes continue during catch-up at their
30-minute/hourly and 6-hour/daily cadences. Measurement summaries still retain
three months hourly and three years daily; status summaries keep their existing
absence of a retention policy.
See [telemetry summary recovery](telemetry-summary-recovery.md) for late-data
assumptions, upgrade boundaries, monitoring, and recovery procedures.

Migration 1021 adds hashboard hourly/daily aggregates; 1030 takes over their
initialization and gates the ten-day raw-retention target on durable coverage.
Board groups include physical board ID, installation ID, and nullable topology
version. Unknown legacy topology stays NULL and its measurements still summarize.
Migration 1021 backfills the retained raw board table in its transaction; 1022
builds reference indexes across retained chunks. Rehearse their duration and lock
footprint on a recent database copy before production rollout.
Source-time buckets retain first/last collection timestamps separately; parent
summaries use collection-time buckets. Board sums, valid counts, minima, and maxima
preserve measured zero and omit non-finite or absent inputs. Saved efficiency is
averaged with its own valid count. These statistics do not retain exact poll-level
cross-board correlations for future PSU residual attribution.

Board history uses the same resolution selection as module history. Responses
identify resolution (zero for raw), topology version, sample count, and collection
bounds; `observed_at` is the source bucket start for summaries. Summary diagnostics
and instantaneous state are unavailable. Groups remain separate when topology or
installation changes within a bucket. Latest readings still expire after two
minutes, independently of durable latest rows.

### Hosted-account charts

Hosted clients and host previews share Fleet's 1h, 24h, 7d, 30d, 90d and 1y
ranges, using the tier selection, measurement retention and bucket rounding above.
Retained buckets align to UTC; expected allocations cover the same intersecting
periods.
Requests allow at most 366 days and 1,500 output buckets, with these work limits:

- Raw reads: 300 weighted asset-days.
- Retained reads: 1.1 million allocation/source buckets, including hourly rows
  rolled up above the daily watermark.
- Unmaterialized raw tail: 300 asset-days, clipped to each allocation above the
  hourly watermark and, for daily reads, the daily watermark.

Uninitialized summaries count the full available request. Excess work caused by
refresh lag returns Unavailable; inherently oversized requests return
InvalidArgument. The initial budget includes the normal policy lag (up to about
2.5 hours hourly and 54 hours daily); the summary limit accommodates 2,500
year-long allocations at that lag. Shorter ranges and allocation/site filters
reduce the work.

Migration 1044 captures hash, power, temperature and state observations in
`hosted_account_history_sample`, recording allocation IDs and full source windows.
Wide hourly/daily rows retain per-metric sums/counts, source bounds and
completeness, plus positive-hash counts and latest temperature/state observations.
Both read paths use `hosted_history_authorized_intervals` to apply site filters,
current site policy and intersected host permissions once per allocation interval.
Membership and suspension also gate access. Each metric must match the recorded
site and fit its allocation's ownership/source window, including after recall,
reassignment or site changes.

Malformed coverage leaves that metric unavailable without failing raw telemetry;
unexpected database errors still propagate. A captured source window crossing a
later allocation-close boundary hides that metric's whole summary bucket,
including valid samples in it. Other allocations remain readable.

Retained history begins at migration 1044's summary epoch. Generic Fleet rollups
lack allocation/source evidence and are never imported; earlier authorized raw
history remains readable while retained. `history_available_from` reports the
later of the selected tier's epoch and retention boundary. Missing history stays
empty rather than being inferred from current ownership.

Charts combine per-asset sample averages. Efficiency is the fleet ratio of
complete paired power/hashrate; incomplete module sources make it unavailable.
Chart uptime is positive-hash sample percentage, not contractual uptime. Latest
authorized temperature/state observations determine distributions. Missing
measurements remain absent.

The [provisional reporting evidence hold](hosted-reporting-evidence.md) retains
new normalized observations and their changes separately from these mutable
chart samples. It does not certify contractual uptime or fill missing sources.

Retained transactions use custom query plans. Aligned source-width buckets with
one interval per typed asset project summary rows directly; other requests use
sample-weighted per-asset aggregation. Both routes share per-metric authorization.
Daily reads roll up hourly rows above the daily watermark and raw samples above
both watermarks, filtering requested allocation intervals before tail aggregation.

Standard Timescale policies refresh the last nine days: hourly every 30 minutes,
daily every six hours. Hosted history has no catch-up job, write fence or retention
gate. Missed buckets outside that window need an explicit refresh before their
source expires: ten-day raw history for hourly summaries, three-month hourly
history for daily summaries. The one-day refresh/raw-retention margin is not an
outage allowance. See [deployment checks](migrations.md#hosted-chart-summaries-1044).

An explicit refresh over expired source data can erase saved aggregate rows.
Inspect the missed periods and remaining source chunks first; retention settings
alone do not prove the source is still present. In `psql`, set `missing_from` and
`missing_to` to the inspected outage bounds as UTC-aligned timestamps. Refresh
hourly first, then daily, clamping each range to complete periods inside its
source retention. Run each call only if its clamped start is before its end:

```sql
SET TIME ZONE 'UTC';

CALL refresh_continuous_aggregate('hosted_account_history_hourly',
  GREATEST(:'missing_from'::timestamptz,
    date_trunc('hour', now() - INTERVAL '10 days') + INTERVAL '1 hour'),
  LEAST(:'missing_to'::timestamptz,
    date_trunc('hour', now() - INTERVAL '1 hour')));

CALL refresh_continuous_aggregate('hosted_account_history_daily',
  GREATEST(:'missing_from'::timestamptz,
    date_trunc('day', now() - INTERVAL '3 months') + INTERVAL '1 day'),
  LEAST(:'missing_to'::timestamptz,
    date_trunc('day', now() - INTERVAL '1 day')));
```

Do not widen these ranges into expired or missing source periods. If hourly
history was never materialized and its raw source has expired, refreshing daily
history cannot recover it. Preserve existing summaries and investigate recovery
from a backup instead.

## Raw telemetry compression

CT1 plant, tank, module, and hashboard telemetry use the same compression schedule
as `device_metrics`: one-hour chunks, compressed after the entire chunk is six
hours old, with the policy running hourly. Segments use `device_id`, `tank_id`,
`module_id`, and `hashboard_id`, respectively, ordered by `time DESC`. Existing
day/week chunks keep their original ranges and become eligible after those ranges
age out; migration 1027 does not split or synchronously compress them. Retention
and summary policies are unchanged. Container rollup summaries read
`device_metrics`; plant JSON has its own raw history and no continuous aggregate.

Late inserts and topology stamps remain supported on compressed chunks. Board
source timestamps can predate a poll by more than six hours even though stale
numeric readings are withdrawn. `stamp_hashboard_topology` groups samples by exact
source time inside PostgreSQL and updates only the requested board IDs and
collection time. It forces custom plans within the function so Timescale can
prune the board-ID array before decompression, even when the outer prepared query
uses a generic plan. PostgreSQL restores the caller's planning mode on return
through the [function's SET clause](https://www.postgresql.org/docs/18/sql-createfunction.html).
Future backfills must likewise bound updates by equipment ID and time.

Topology compaction probes tank history by the observation's collection time and
the controller's tank IDs, including historical tanks. Migration 1027 also replaces
the tank observation FK with `(topology_observation_id, time)` referencing
`(id, collected_at)`. PostgreSQL's independent FK checks can then prune by time;
the application lookup alone cannot bound those checks. The migration validates
this collection-time invariant against existing tank rows before enabling
compression. Rehearse that validation on retained data when sizing upgrade time.

The compression down migration decompresses retained chunks before disabling
compression and restoring the original chunk intervals; allow enough time and
disk space for the uncompressed data. Prefer a forward correction for deployed
databases.

See TimescaleDB's [compression policy](https://github.com/timescale/docs/blob/latest/api/compression/add_compression_policy.md)
and [compressed-data writes](https://github.com/timescale/docs/blob/latest/use-timescale/compression/modify-compressed-data.md)
for the underlying policy and DML behavior.

## Topology compaction

A version is an immutable container layout. An installation is one board's
placement episode; the same installation can participate in several whole-container
versions. Both remain retained. Raw container/tank records reference a per-poll
observation and its version; raw module and board records reference the version
directly. Board records additionally reference their physical board and installation.
All summary tiers retain version IDs, never a representative per-period receipt.
A change inside an hour/day creates separate groups in that bucket.

Migration 1022 registers cleanup **disabled by default**. Once enabled it compacts
observations older than ten days into the first observation plus
`last_confirmed_at`. Only consecutive observations of the same version and
completeness join. A gap greater than 120 seconds starts a new span; A–B–A also
stays three spans. The fixed gap policy is independent of display freshness and
cannot retroactively recover deleted timestamps. Recent observations stay exact.
A span represents sampled confirmations, not proof of uninterrupted physical state.

Cleanup retains any redundant observation still referenced by raw container/tank
or latest-value records. Container raw references are logical because compressed
`device_metrics` has no observation FK. Ingestion stamps them at the observation's
collection time for the same controller; cleanup uses that invariant for chunk and
compressed-segment pruning. Tank checks also constrain collection time and tank
ID; compressed chunks do not use the raw observation index. Tank/latest FKs
remain enforced and have supporting rowstore indexes. Pinned
interiors enter a separate retry queue and do not stall the forward cursor or split
the span. Due retries keep a controller eligible for another bounded turn even
when forward scanning has caught up; retries deferred into the future do not.
Latest references can pin an observation indefinitely.

Each run processes one controller, at most `batch_size` new observations plus
`batch_size` due retries. Migration 1027 caps each scheduled queue at 50, even if
an older job config still requests 500; smaller configured batches are honored.
This leaves room for chunk-level FK trigger overhead within the two-second
deadline. Direct manual calls still accept limits up to 10,000.
A nonblocking organization
lock serializes reference checks and deletion with ingestion. Expected lock
contention defers cleanup without incrementing the failure counter. Progress, span
updates, and deletes commit atomically. A separate committed one-minute scheduling
lease survives cancellation so a slow controller cannot monopolize subsequent
runs. Migration 1035 changes the job from a fixed ten-second schedule to a
one-second delay after each successful finish (`fixed_schedule=false`), with the
same two-second maximum runtime and one cleanup worker. The configured one-minute
`retry_period` is not a guaranteed job-level delay: TimescaleDB 2.30.2 caps ordinary
failure backoff at five schedule intervals (about five seconds with jitter).
Drifting scheduling avoids shortening that backoff further to the next fixed
one-second slot. The committed one-minute **per-controller** lease is independent
of job-level retries. See the [versioned scheduler implementation](https://github.com/timescale/timescaledb/blob/2.30.2/src/bgw/job_stat.c#L308-L420).
Do not wrap
`CALL run_job(...)` or the job procedure in a client transaction; its scheduling
lease requires a top-level CALL. Direct `compact_container_topology(...)` calls are
atomic but do not have the scheduler's runtime limit.

Inspect before enabling (replace `:device_id` with a controller database ID):

```sql
SELECT * FROM container_topology_candidates(:device_id, now()-INTERVAL '10 days', 500);
SELECT * FROM container_topology_compaction_state ORDER BY next_run_at;
SELECT count(*) FROM container_topology_compaction_pending;
SELECT job_id, config, scheduled, fixed_schedule, schedule_interval, retry_period
FROM timescaledb_information.jobs
WHERE proc_name='compact_container_topology_job';
```

Before upgrading an existing deployment through migration 1035, record the job's
configuration and scheduling state using the query above. **An already enabled
job adopts the faster cadence as soon as the migration runs.** Disable cleanup
and pause scheduling before migrating, then verify any active run has finished:

```sql
SELECT alter_job(job_id, config=>config || '{"enabled":false}'::jsonb,
                 scheduled=>false)
FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';
SELECT job_id, job_status, last_run_status
FROM timescaledb_information.job_stats
WHERE job_id IN (SELECT job_id FROM timescaledb_information.jobs
                WHERE proc_name='compact_container_topology_job');
```

Pausing prevents future starts; it does not cancel an active run. Wait until
`job_status` is no longer `Running`. Apply the migration, validate capacity and
runtime on a representative isolated copy, then explicitly enable the deployment
and watch its first scheduled runs. Preserve prior opt-outs; do not automatically
restore an enabled setting before validation. Leave deployments that are not
ready with `scheduled=false` and `enabled=false`.

The candidate report is read-only and uses the same span classifier as deletion.
It identifies redundant candidates, not a promise they are unreferenced; inspect
`container_topology_references(ARRAY[...])` for their current pins. To enable after
reviewing a representative dry run and runtime:

```sql
SELECT alter_job(job_id, config=>config || '{"enabled":true,"batch_size":25}'::jsonb,
                 scheduled=>true, next_start=>now())
FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';
```

Enablement is a separate rollout step after applying migration 1035 and validating
the deployment's database capacity. The migration preserves both `config.enabled`
and `scheduled`, including existing opt-outs; it does not enable cleanup. Record
the deployment, migration version, job ID, enablement time, observation rate, and
before/after progress in the deployment record. Verify several successful
scheduled runs, falling backlog, and acceptable ingestion latency before recording
the deployment as enabled. No deployment is qualified by the local test alone.

Keep the cadence change in the migration so enabled jobs share the tested schedule
and rollback behavior. A fresh or previously disabled job can still be scheduled:
`enabled=false` alone launches a no-op worker every second after migration. Pause
it with `scheduled=false` when cleanup is unused. On the mini profile's four-slot
Timescale worker pool, qualify retention, compression and aggregate progress as
well as cleanup before enabling; the isolated capacity test does not measure this
shared-worker cost.

The enablement example uses an operator batch size of 25, below the unchanged hard
cap of 50 for each queue. The one-second ingestion fixture exposed a cold-batch
organization-lock stalls above its 250 ms limit at batch sizes 50 and 40. Qualify the smaller
setting against both ingestion latency and throughput on the target deployment;
the migration deliberately preserves existing batch settings, so applying it alone
does not establish this latency bound.

Set `enabled` false and `scheduled` false to stop future work and worker launches.
Track state-table examined/deleted totals, last duration/error, pending depth,
and Timescale job statistics. Worker termination
may only appear in Timescale job statistics; the committed scheduling lease remains.
Migration 1057 also bounds reference lookup planning to the candidate collection-time
range. It forces a custom plan inside the lookup so prepared callers do not plan
every retained metric chunk before excluding unrelated chunks at runtime. This
leaves more of the runtime budget for deletion and synchronous commits; it does
not change enablement, batch size, the runtime limit, or reference retention.

Reduce the configured batch size if runs hit the runtime limit. No version or
installation deletion is included. Allocation authorization, subset rollups, and
shared PSU attribution remain future work.

### Scheduled capacity and backlog checks

The sizing target is **100 controllers at one observation every 15 seconds**:
6.67 newly eligible observations/second, plus retry work. The 50-row batch cap
with the old ten-second interval could examine at most 5 new observations/second,
even before runtime and contention. The one-second interval uses the existing
worker more frequently instead of adding workers that contend on an organization's
ingestion lock. At 50 new observations per successful batch, a two-second run
plus a one-second scheduling delay would provide 16.67/second before additional
worker delays: a sizing estimate, not a throughput guarantee. Do not count retry
deletions as forward-scanning capacity.

Qualify at least twice the incoming eligible-observation rate to leave backlog
drain margin. Smaller configured batches, database load, failure backoff, and
worker availability reduce that margin. The shared Timescale worker pool also
serves retention, compression and aggregates; ensure it has headroom and check
those jobs for regressions. Reaching the two-second deadline rolls back progress;
increasing batch size beyond 50 cannot increase scheduled capacity. When a
controller has caught up, its existing one-hour idle delay still applies, so a
small sawtooth backlog is expected. Sustained growth across those cycles is not.

Run the opt-in capacity regression on an isolated TimescaleDB using the repository
image (substitute its port; do not point this at a deployment database):

```sh
DB_ADDRESS=127.0.0.1:<port> DB_PASSWORD=fleet CT1_TOPOLOGY_CAPACITY_TEST=1 \
  go test ./internal/infrastructure/timescaledb \
  -run '^TestTopologyCompactionScheduledCapacity$' -count=1 -v -timeout=12m
```

Run from `server/` with Hermit active. The fixture uses 100 controllers in one
organization, six tanks each, 216 hourly chunks containing 129,600 compressed
samples, 10,000 initially unscanned receipts, and 5,000 due retries. It flushes one
transaction per second for 150 seconds, distributing the 100 controllers across
each 15-second cycle. Each flush locks the organization once per controller, as
the metrics writer does, and inserts one eligible and one current observation for
each of its six or seven controllers. Backdated inserts simulate observations
crossing the ten-day cutoff without waiting ten days. The fixture exercises
receipt ingestion and production locking, not full inventory/metric reconciliation
or controller transport; its latency bound applies to those receipt transactions.
The real background job uses its migrated schedule and runtime limit, with the
documented operator batch size of 25. The test
requires forward throughput above 13.33/s and bounded, shrinking backlog during
arrivals. Every controller must receive service and the backlog must drain within
eight minutes after arrivals stop. This is a fixture completion bound, not a
worst-case scheduling guarantee: a lock deferral adds a one-minute lease plus
older due work ahead of that controller.
It also requires every ingestion transaction to finish within 250 ms, checks
preserved compressed samples/current observations and zero cleanup or background-job
failures, and reports sampled no-progress deferrals alongside throughput. A separate
pre-measurement probe holds the ingestion lock across a real scheduled run to
verify deferral; that probe is excluded from the throughput measurement. Natural
overlap during the measured phase varies between runs. The separate
three-million-sample deadline/FK regression remains required.
Other maintenance jobs are paused in this fixture;
production worker contention and controller ingestion need deployment validation.

The reference local environment uses PostgreSQL 18.6, TimescaleDB 2.30.2,
256 MB shared buffers, four Timescale background-worker slots, and `fsync=on`.
Record run-specific throughput, ingestion latency, deferrals and drain time in the
PR or deployment validation record alongside the exact commit and environment.
This document defines the fixture and pass criteria rather than a benchmark that
drifts with each rerun. These checks do not establish deployment capacity.

Sample counters at two wall-clock times to calculate `delta(examined)/seconds`
and `delta(deleted)/seconds`. Compare the former to the deployment's observed
eligible input rate; the latter includes retries and cannot replace it:

```sql
SELECT sum(examined) AS examined, sum(deleted) AS deleted,
       sum(failures) AS failures, max(last_duration_ms) AS last_run_max_ms
FROM container_topology_compaction_state;
SELECT count(*) AS pending, count(*) FILTER (WHERE next_check_at <= now()) AS due
FROM container_topology_compaction_pending;
SELECT j.job_id, j.schedule_interval, j.fixed_schedule, j.retry_period,
       j.max_runtime, j.scheduled, j.config,
       s.last_run_status, s.last_successful_finish, s.total_runs, s.total_failures,
       s.next_start
FROM timescaledb_information.jobs j
LEFT JOIN timescaledb_information.job_stats s USING (job_id)
WHERE j.proc_name = 'compact_container_topology_job';

-- Oldest unscanned eligible receipt per controller; skips retained anchors and
-- future pins so neither is mistaken for forward-scanning lag.
SELECT s.device_id, s.scanned_through, s.next_run_at,
       now() - INTERVAL '10 days' - oldest.collected_at AS eligible_lag
FROM container_topology_compaction_state s
CROSS JOIN LATERAL (
  SELECT o.collected_at FROM container_topology_observation o
  WHERE o.device_id = s.device_id
    AND o.collected_at > COALESCE(s.scanned_through, '-infinity'::timestamptz)
    AND o.collected_at < now() - INTERVAL '10 days'
  ORDER BY o.collected_at LIMIT 1
) oldest
ORDER BY eligible_lag DESC;
```

Lock deferrals do not increment `failures` or update `last_error`. To find suspected
repeated deferrals, take two state samples at least 65 seconds apart in the same
SQL session (outside a transaction), so the one-minute lease can expire:

```sql
CREATE TEMP TABLE topology_progress_before AS
SELECT device_id, next_run_at, last_run_at, examined, deleted, failures
FROM container_topology_compaction_state;

-- Wait at least 65 seconds, then run this query in the same session.
SELECT s.device_id, b.next_run_at AS previous_next_run_at, s.next_run_at,
       s.last_run_at, s.examined, s.failures
FROM container_topology_compaction_state s
JOIN topology_progress_before b USING (device_id)
WHERE s.next_run_at > b.next_run_at
  AND s.last_run_at IS NOT DISTINCT FROM b.last_run_at
  AND s.examined = b.examined AND s.deleted = b.deleted AND s.failures = b.failures
  -- Exclude freshly claimed leases that may still be doing their two-second batch.
  AND s.next_run_at < clock_timestamp() + INTERVAL '55 seconds'
ORDER BY s.next_run_at;
DROP TABLE topology_progress_before;
```

This is a sampled no-progress signal, not an exact lock-deferral counter: transient
leases may be missed, and canceled workers can leave the same pattern. Repeat the
samples and correlate them with Timescale failures/crashes and live ingestion
before attributing them to lock contention. Successful work changes `last_run_at`,
including idle checks, so the ordinary one-hour idle delay is not counted here.

Observe beyond the one-hour idle cycle before judging steady-state lag. Investigate
increasing lag, controllers repeatedly deferred, failed jobs, or ingestion latency
regressions. Disable cleanup if it harms ingestion; preserve progress and diagnose
worker availability, locks and query runtime before re-enabling. Rolling back
migration 1035 restores the fixed ten-second schedule without deleting progress or
changing enablement, but also restores the known capacity limit.

Prefer disabling jobs and fixing forward while preserving the schema and evidence.
An application release must recognize the installed database checkpoint; see the
[recovery rollback restrictions](telemetry-summary-recovery.md#upgrade-and-existing-gaps).
Migration 1021 refuses downgrade with retained board summaries; 1022 refuses it
once confirmation spans exist. Their checks lock evidence tables through the
migration transaction. Stop application and scheduled writers before a coordinated
schema rollback; do not delete evidence to make the guards pass.

## Local validation and cost

Reproducible checks include `BenchmarkCT1PollNormalization` (6 tanks, 96 modules,
288 boards), timestamp/absence, move/retry/partial-map regressions, and API
authorization tests. The allocated reporting-estate fixture,
`TestStoredContainerRollupsReportingEvidenceRepresentativeScale`, and its current
capture measurements are documented in the
[reporting evidence runbook](hosted-reporting-evidence.md#local-capture-cost).

The following are historical CT1 baseline measurements from an Apple M4 Max and
local Docker TimescaleDB, without an allocated reporting estate:

- Normalization: approximately 20 ms, 12.6 MB allocations, 398k allocations/poll;
  the minimal protobuf fixture is about 26 KB (real diagnostic payloads are larger).
- Twelve full inventory/telemetry transactions: approximately 58 ms/poll.
- A small saved-container chart query: approximately 23 ms.
- One compressed JSONB topology value: approximately 5.1 KB; each tank sample:
  approximately 952 bytes before index/chunk overhead. At a ten-second example
  cadence, six tank rows use roughly 49 MB/day, or 490 MB for ten days.

These are development measurements, not capacity guarantees. Actual polling
cadence remains unchanged. Monitor RPC duration/timeouts and supplement warnings,
coverage and source ages, topology versions versus observations, table/chunk
sizes, query latency, and transaction failures. Measure real controller CPU/load
and actual wire bytes before cadence tuning. Real firmware qualification for both
direct and FleetNode paths remains required; local tests cover their transport
contracts but do not qualify deployed hardware.

## Rollout and rollback

Deploy the migration/server and matching SDK/plugin to direct servers and
FleetNodes. Confirm facility/PDU wiring against commissioning information,
source timestamps, partial coverage, and old/new chart separation. A server
with an older plugin cannot produce these new totals: tank/container values
remain unavailable, rather than restoring fallback measurements.

Prefer a compatible fix-forward release while retaining the schema and collected
evidence. Older binaries reject database checkpoints outside their embedded
migration history. The telemetry recovery migration refuses downgrade; a
coordinated rollback requires the pre-deploy database snapshot and matching
application release. See the [recovery runbook](telemetry-summary-recovery.md#upgrade-and-existing-gaps).
Historical ingestion is not enabled by this change.
