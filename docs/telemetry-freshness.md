# Committed telemetry freshness

Fleet can have healthy HTTP requests and live Fleet Nodes while device telemetry
stalls. Freshness tracks committed device-level measurements against current
paired inventory, including targets that have never produced a measurement.

`TIMESCALEDB_TRACK_TELEMETRY_FRESHNESS` defaults to false. No rollout or production
qualification is implied by adding this capability. The separately gated host
observer and Datadog alert package live in `squareup/tf-protofleet`.

## Measurement and clock contract

The SDK mapper preserves observation timestamps. Freshness uses the oldest
observation time among finite device-level hashrate, temperature, power, fan-speed
and efficiency values, including measurement metadata timestamps. Zero is a valid
measurement. Status-only responses and empty topology do not establish freshness.
A stable value with a genuinely new observation is fresh; unchanged numeric values
alone do not establish a stall.

Fleet Nodes send `telemetry_observation_age` at response assembly, including time
spent in plugin caches. Fleet subtracts this duration from its earlier server
request start, giving a conservative server-clock bound without comparing node
and server wall clocks. Missing, invalid or negative ages cannot establish a new
observation. Existing valid evidence on the same route is retained and ages into
stale; a device without valid evidence for its current route remains unknown.
The legacy response timestamp fallback cannot create freshness evidence. Absolute
observation times from the JSON payload are never trusted for this purpose.
Older Nodes can continue serving their existing telemetry but provide no freshness
age; update every serving Node before enabling the alerts.

The collector/plugin must preserve the original timestamp when returning cached
measurements. ASIC-rs caches the poll-start timestamp alongside the data and uses
it as the SDK snapshot timestamp on every cache hit; a cache TTL longer than the
two-minute stale threshold cannot renew freshness. These timestamps describe
collector observations, not independently verified hardware sensor clocks.
Container controllers count as one target;
missing individual boards/sensors, chart rollup delays and command progress need
separate signals.

## Persisted state and inventory

Migration 1076 adds `telemetry_freshness`, one row per inventory device, and
`telemetry_freshness_exclusion`. The checkpoint driver owns migration transactions;
the up/down SQL contains no transaction-control statements.

The compact row is written after telemetry inside a savepoint in the same
transaction. A watermark can never commit without its associated telemetry.
Recoverable freshness-write errors roll back only that savepoint, emit a structured
`telemetry_freshness_write_failed` warning with error and batch size, and allow
telemetry to commit. Investigate those log events when telemetry is landing but
freshness remains stale/unknown. Failed savepoint recovery, lost connections,
exhausted transaction deadlines and final commit failures still fail the batch.
Container snapshots rejected by inventory ordering never update freshness because
their device-level measurements were not persisted.

Observation watermarks never move backward within a route. Status-only or invalid
observations preserve the last valid observation and known state on that route;
unknown-to-known evidence can update immediately. Source Node identity is set by
the server. Prior-route responses are ignored, and a new route resets observation
and trust to its own evidence. HA hand-offs update `assigned_at`, the start of the
current Node assignment, so they receive the same five-minute admission grace.

The 30-second write gate coalesces both `observed_at` and `ingested_at`, not just
ingestion time. Route changes and the first known observation bypass it. The
stored watermark can lag by roughly 30 seconds plus polling cadence, on top of
source/cache age; the two-minute stale threshold is conservative by that margin.
The observer's separate 30-second cadence adds detection latency. Row-conflict
checks still run at polling frequency. Ingestion alone does not refresh a target.
Successful commit means query-visible data; existing `AsyncMetricCommit`
durability semantics are unchanged.

`telemetry_freshness_summary` reads inventory plus compact state, not raw telemetry
history. Its partition is organization, current site, source Fleet Node and device
class. Source node 0 means direct polling; site 0 means unassigned.

- Live PAIRED, DEFAULT_PASSWORD and AUTHENTICATION_NEEDED devices are expected.
  Disconnection, explicit offline state and paused hashing do not shrink coverage.
- Pairing or Node assignment has five minutes of grace. Deleted/unpaired devices
  leave coverage. Data predating the current pairing/assignment is unknown.
- Explicit maintenance exclusions expire. They affect only alert expectations,
  without stopping polling or altering device operational state.
- A last valid observation older than two minutes is stale. Missing or
  untrustworthy evidence is unknown only when no valid observation exists for
  the current pairing and assignment. Expected count equals fresh + stale + unknown. Unknown
  and stale targets both contribute to the affected fraction.

Use the generated `GetTelemetryFreshnessSummary` query for diagnostics. Per-device
investigation can join inventory to `telemetry_freshness`; device identifiers stay
out of Datadog metric tags. A privileged operator can manage maintenance through
the exclusion table using the normal database administration workflow, supplying
an explicit device ID, reason and expiration. This package adds no public
maintenance API or automatic maintenance inference.

## Direct SQL collection

`telemetry_freshness_summary` is a vendor-neutral aggregate interface. External
collectors may query it directly with a dedicated `fleet_metrics_ro` login. The
HA PostgreSQL access policy permits that login over TLS from the two database
hosts only. Fleet does not create its credentials or grant broad table access.
The deployment owner provisions the login, grants CONNECT on `fleet`, USAGE on
`public`, and SELECT on this view only, with read-only sessions and bounded
statement/lock timeouts. A missing role or grant must fail collection visibly.

Direct collectors must enforce primary-only evaluation, per-query success
freshness, bounded cardinality, and separate unknown/missing-data handling. They
must not sum old-primary and new-primary samples across failover. Stateless SQL
collection does not provide the snapshot observer's retained removal tombstones;
missing-scope transitions need an explicit monitoring policy and qualification.
No additional application exporter or collector executable is required.

## Collection and rollout

The independent host observer runs every 30 seconds and queries only a local
PostgreSQL primary, with a five-second statement timeout and 250 ms lock timeout.
Replicas report observer health but export no population, preventing duplicate
counting. Evaluation therefore continues if Fleet's polling/writer goroutines
stall. Missing migration, failed query or missing/stale snapshot becomes explicit
observer failure. The Agent reads only nonsecret aggregate JSON and receives no
database credentials. Live populations are capped at 5,000 scopes; up to 1,000
recovery tombstones have a separate allowance. The complete snapshot must fit in
2 MiB. Older recovery entries are evicted first under pressure and reported by
`protofleet.telemetry.tombstones_dropped`; valid live populations are preserved.
Retained tombstones expire after ten minutes. Live-data overflow still fails closed.

1. Review migration and rollback plans, release the server and update all Nodes.
2. Enable `TIMESCALEDB_TRACK_TELEMETRY_FRESHNESS=true` through the existing
   configuration/release workflow. Check watermarks and replay/clock semantics
   before treating the data as qualified.
3. Install the gated observer from tf-protofleet on both database hosts. Verify
   explicit observer health from both, one authoritative primary, expected scope
   coverage, counts, maintenance expiry and initial missing-data behavior.
4. Qualify the default-off monitors in development/staging: one vanished target,
   partial/full Node stalls, replayed old samples with successful RPCs, failed
   persistence, unknown timestamps, grace, deletion, maintenance, lost export,
   primary failover and recovery. Verify alert/recovery Slack delivery.
5. Review the authoritative environment plan and enable notifications only after
   representative coverage, cost and failure transitions are accepted.

Disabling tracking stops watermark updates but does not clear stored evidence or
imply health. Disable dependent alerts before intentionally disabling collection.
Application rollback does not remove the new tables. The migration's down script
removes freshness/exclusion state; stop tracking and collection first, and expect
the initial grace/unknown behavior when the feature is installed again.

Local tests exercise 1,000 inventory targets and batched watermark updates. They
are not a production-load or HA qualification; record database contention, WAL,
replica lag, evaluator duration and exporter cardinality during staged rollout.
