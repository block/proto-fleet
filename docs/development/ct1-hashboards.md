# CT1 hashboard inventory and telemetry

The CT1 plugin collects three slot observations per module from the existing bulk
controller map. It makes no additional board or ASIC requests. Each occupied
slot carries normalized MAC/serial identity, mining state, isolation evidence,
source timestamps, and optional SI-valued hashrate, temperature, board power, and saved efficiency. The pinned protobuf
board object is retained as diagnostic JSON metadata, excluding `health`; health
is retained separately in firmware units. This preserves known firmware fields,
nested issues, sensors, counters, and string-encoded integers. See the
[native gRPC contract and presence limits](ct1-grpc.md).
PSU readings supply module power once; they never substitute for board power.
See [polling measurements](ct1-polling-metrics.md) for source, coverage, and topology rules.

An explicit unoccupied slot is `empty`; an omitted slot, omitted occupancy, or
occupied slot without a board is `unknown`. Health at least two minutes old or
more than five seconds ahead of collection time does not produce current
measurements. Missing values stay absent, including non-authoritative remote
isolation flags. Missing/future source timestamps never advance the latest
health watermark.

## Durable hardware and placements

`hashboard` owns the stable hardware ID within an organization.
`hashboard_identity` resolves normalized MAC and serial aliases; a later serial
can enrich an existing MAC identity. Contradictory aliases, changed known serials,
and duplicate placements make every implicated slot unavailable. Identity claims
are counted before diagnostic validation or database writes, including readable
MAC/serial fields on malformed board reports. A report needs at least one hardware
identifier. Safe aliases from rejected slots travel in a separate SDK protobuf
claim list used only for conflict detection. They never create, enrich, or move
hardware. Identities exceeding 256 bytes, invalid UTF-8, and NUL-containing identities are rejected, never repaired
by stripping characters. Both plugin and server check diagnostic JSON against the
[PostgreSQL JSONB contract](https://www.postgresql.org/docs/18/datatype-json.html),
including Unicode surrogate pairing and numeric storage limits, before persistence.
Diagnostic nesting is limited to 64 levels to stay within database parser limits.

Malformed slot/board data is contained at the slot; ambiguous slot layouts make
that module's slots unavailable. Rejected boards do not contribute to plugin
module aggregates. Independent controller totals, plant/equipment observations,
and valid modules continue to be collected and stored. Structured warnings and
slot `unavailable_reason` codes explain the rejection; Fleet displays that reason.
Actual database errors still fail the telemetry transaction. Missing or deleted
controllers are an expected no-op for container reconciliation.

`hashboard_installation` records the module, slot, site, and time interval of a
placement. Removal, replacement, module replacement, site reassignment, and
moves close the old interval and open a new one. Reinstallation retains the
hardware ID. Unknown slots do not prove removal. Controllers reporting the same
board concurrently have the incoming conflicting slot marked unavailable while
the old installation was seen within two minutes; explicit removal or expiration of that window allows a move. Older
reports cannot undo a newer placement or removal. Reconciliation is batched and
serialized within the organization inside the container telemetry transaction.
If a module is replaced while its slot evidence is rejected, the prior hardware
installation is retained but hidden with its retired module. Repeated rejected
reports retain that interval; trustworthy removal, replacement, movement, or
complete-topology disappearance resolves it.

`hashboard_slot` tracks the current occupancy and freshness separately from
hardware identity. `hashboard_latest_metrics` supplies current readings;
`hashboard_metrics` retains ten days of source-time samples with collection times to order
repeated evidence. Hourly summaries retain three months and daily summaries three
years, grouped by board, installation, and topology version. See
[polling retention and cleanup](ct1-polling-metrics.md#semantics-and-retention). Retried identical snapshots do not add samples. Metadata is
the latest inventory evidence for each installation, not a metadata change log.

## API and authorization

`FleetInventoryService.GetModuleHashboards` accepts up to 100 module IDs and
returns exactly three slots for each authorized current module. Missing, retired,
and inaccessible module IDs are omitted, including for single-ID requests; other
readable results in the batch remain available. Board readings
include durable board and installation IDs, source and collection timestamps,
typed measurements, and complete metadata/health objects. Stale slots are
unavailable. `ListHashboardHistory` pages a board's samples over a requested
source-time range; cursors are bound to the request, organization, scope, and
source resolution. If aging changes the source tier between pages, continuation
returns FailedPrecondition with a request to restart pagination. Tokens issued
without a resolution also require a restart. The service resolves the tier once
per request and uses it for both cursor validation and fetching rows.
History access requires a readable current or closed installation, independent
of the requested time range. Unknown/inaccessible boards both return NotFound;
a readable board without samples in the range returns an empty successful page.
`HashboardReading.collected_at` belongs to the health sample for raw readings
and the first contributing collection for summaries; `last_collected_at` records
the last contributing collection. Summaries expose resolution and sample count,
but no instantaneous diagnostic state; the slot's
own latest observation time is `HashboardSlot.observed_at`.

Both methods use the existing container/module read permissions. Historical
samples use the installation's recorded site, not the board's current site.
After a move, health and fault state originating before the new installation
are excluded from its current readings/history. A fresh health timestamp does not
make older fault evidence current: state and its timestamp are suppressed
independently at insertion and history reads, including for existing rows.
Raw health diagnostics are withheld when their fault evidence cannot be assigned
to the installation; fresh typed measurements are still retained and returned.
Initial discovery may use an
asynchronous sample from before first observation because no prior placement
is known.

## Miners list

CT1 module rows expand with a keyboard-accessible button into three read-only
slot rows. Only expanded modules on the current page are polled. Collapse or
navigation cancels requests. Collapse evicts that module's data while retaining
other expanded modules; navigation clears all retained data. Normal polling
preserves expansion. Errors expose a retry and clear readings. Board rows have no asset
selection or actions and do not enter parent pagination, counts, sorting, or
CSV exports. Unavailable and inapplicable values render as such, without
inheriting module values. Names use one line: `Hashboard N - serial` (MAC fallback).
Module and board issue cells use the existing miner issue summaries and icons,
with details in a modal. With no current reported causes, the issue cell is blank;
it does not assert healthy or invent a cause from missing evidence. Partial-report
context remains available in the details of reported issues.

## Retention and volume

The metrics hypertable uses one-day chunks and a 30-day retention policy.
Hardware and installation records persist independently of telemetry retention.
For `M` fully populated modules polled every `P` seconds, the upper bound is
`3 × M × 86,400 / P` board samples per day. At 1,000 modules and a 30-second
poll, this is 8.64 million samples/day, or 259.2 million over 30 days, in addition
to module telemetry. Missing health and unchanged retried snapshots reduce it.

Payload size depends on firmware diagnostics. Deployment sizing must measure
representative row/index bytes and ingestion/query performance at fleet scale;
one-day chunks are a starting configuration, not a capacity benchmark. The
large-fleet guidance referenced as issue #614 in #163 was unavailable when this
implementation was validated, so that guidance has not been confirmed.
