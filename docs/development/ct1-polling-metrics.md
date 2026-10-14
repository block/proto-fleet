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

Container, tank, and module measurements use the miner retention tiers:

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
for older or longer windows, with the existing time-based pagination.
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
recover samples already deleted.

Board history keeps its existing **30-day** retention. Latest rows are retained
but readings expire after two minutes.

Tank history has daily chunks and a `(tank_id,time)` unique index. Topology
versions and observations are retained indefinitely for provenance, including
past placements, rather than deleting mappings still referenced by telemetry.
An observation is one small row per accepted poll; full topology is stored only
on change. Review this retention with actual fleet growth before introducing a
reference-aware pruning policy. Compressed `device_metrics` cannot acquire the
new foreign key in place: its observation reference is written in the same
transaction and topology is retained. New tank/latest tables enforce FKs.

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
