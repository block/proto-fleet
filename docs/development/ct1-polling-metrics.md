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

Migration 1016 temporarily removes raw retention for these three tables and
schedules `initialize_container_metric_summaries` one minute later. That job
materializes available history before installing summary refresh/retention and
restoring ten-day raw retention, then deletes itself. Failures retry without
expiring the raw evidence. Monitor this job in `timescaledb_information.jobs` and
`timescaledb_information.job_stats`; a persistent job or repeated failures needs
investigation. Hourly refresh covers the last day every thirty minutes; daily
refresh covers the last seven days every six hours. Both refresh windows stay
inside raw retention so normal refreshes do not erase older summaries. This
backfill uses Fleet's existing rows; it does not fetch firmware history or
recover samples already deleted. Like existing parent summaries, scheduled refresh
assumes live polling. Ingestion has no lower age fence: historical rows inserted
after bootstrap outside the hourly one-day/daily seven-day refresh windows can
expire without materialization. Any manual historical backfill must pause raw
retention and explicitly refresh its affected ranges before resuming retention.
Do not refill a source bucket whose raw data was already deleted: refreshing it
would replace its existing summary with only the newly inserted subset. Historical
catch-up requires a separate ingestion/materialization design; it is not enabled
or made safe by this compaction change.

Migration 1021 adds hashboard hourly/daily aggregates and follows the same
bootstrap sequence before changing board raw retention from thirty to ten days.
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
runs. The job runs every ten seconds with a two-second maximum runtime. Do not wrap
`CALL run_job(...)` or the job procedure in a client transaction; its scheduling
lease requires a top-level CALL. Direct `compact_container_topology(...)` calls are
atomic but do not have the scheduler's runtime limit.

Inspect before enabling (replace `:device_id` with a controller database ID):

```sql
SELECT * FROM container_topology_candidates(:device_id, now()-INTERVAL '10 days', 500);
SELECT * FROM container_topology_compaction_state ORDER BY next_run_at;
SELECT count(*) FROM container_topology_compaction_pending;
SELECT job_id, config FROM timescaledb_information.jobs
WHERE proc_name='compact_container_topology_job';
```

The candidate report is read-only and uses the same span classifier as deletion.
It identifies redundant candidates, not a promise they are unreferenced; inspect
`container_topology_references(ARRAY[...])` for their current pins. To enable after
reviewing a representative dry run and runtime:

```sql
SELECT alter_job(job_id, config=>'{"enabled":true,"batch_size":50}'::jsonb)
FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';
```

Set `enabled` false to stop future work. Track state-table examined/deleted totals,
last duration/error, pending depth, and Timescale job statistics. Worker termination
may only appear in Timescale job statistics; the committed scheduling lease remains.
Reduce the configured batch size if runs hit the runtime limit. No version or
installation deletion is included. Allocation authorization, subset rollups, and
shared PSU attribution remain future work.

Prefer disabling jobs or rolling back the application while keeping the schema.
Migration 1021 refuses downgrade with retained board summaries; 1022 refuses it
once confirmation spans exist. Their checks lock evidence tables through the
migration transaction. Stop application and scheduled writers before a coordinated
schema rollback; do not delete evidence to make the guards pass.

## Local validation and cost

Reproducible checks include `BenchmarkCT1PollNormalization` (6 tanks, 96 modules,
288 boards), `TestStoredContainerRollupsRepresentativeScale`, timestamp/absence,
move/retry/partial-map regressions, and API authorization tests. On an Apple M4
Max with the local Docker TimescaleDB, one representative run measured:

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

Prefer application rollback while retaining the schema and collected evidence.
The down migration is destructive: it removes the new rollups, topology, and
measurement columns. It is for a coordinated rollback after stopping new-format
writers, not routine recovery. Historical ingestion is not enabled by this change.
