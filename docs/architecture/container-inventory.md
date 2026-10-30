# Container inventory and telemetry

Containers remain paired `device` records. The controller owns the network
connection, credentials, and polling schedule. `container_inventory`,
`container_tank`, and `container_module` record its observed physical topology;
tanks are not managed racks and modules are not independently paired miners.

## Collection and identity

The CT1 plugin collects the bulk container map once per poll and derives module
observations from its board health. Hashrate is summed across all three boards
and converted from TH/s to H/s. Temperature is the mean of the boards' explicit
ASIC-temperature readings. Module power is the freshest valid PSU input-power
reading, counted once even when all boards repeat it. Board power is not a
fallback for PSU input power. Missing or stale contributions make the affected
metric unavailable; measured zero remains zero.

Container mining totals and plant telemetry are supplementary requests with
bounded deadlines. Their failure does not discard a successful topology/module
observation. Parent cancellation still cancels the collection.

Fleet Node transports the complete metrics JSON, including container topology
and board observations, with a 960 KiB limit inside its 1 MiB command-response
payload. Larger snapshots fail collection without persisting partial inventory.
Upgrade Fleet Server before Fleet Nodes when moving from the previous 256 KiB
limit; older servers reject observations above that limit.
Authenticated Fleet Node uploads use gzip for messages of at least 1 KiB,
including telemetry acknowledgments on the gRPC control stream. Compression
reduces bytes on the network; the JSON and command-response limits still apply
to the decoded messages.

The CT1 collector derives container efficiency from the final controller power
and hashrate measurements in each poll, storing watts divided by hashes/second
as J/H in the existing device telemetry field. This follows the controller power
selection (facility power, then plant total power, then mining power), independently
of module equipment totals. Missing or invalid inputs and zero hashrate leave
efficiency unavailable. Charts display the collected samples as J/TH; historical
samples without efficiency are not backfilled.

Fleet child IDs are independent of tank/module positions. Reconciliation is
serialized on the controller inventory row and only newer topology observations
may update current inventory. A complete map retires absent modules and marks
absent tanks; partial maps never retire absent members. A different nonempty
reported hardware ID in the same slot retires the previous module and creates
a new Fleet ID. Missing identity evidence does not erase an established ID.
Marking state unknown without new health evidence retains the last accepted
evidence timestamp, preventing a later poll from restoring older state.
Automatic identity reconciliation across moves is not implemented.

## Storage

Current module state lives with inventory; source observation time is retained
separately from the last collection that saw the module. A newer poll containing
older state evidence cannot overwrite newer state evidence.

`container_module_metrics` retains ten days of raw observations. Container, tank,
and module hourly summaries retain three months; daily summaries retain three
years. Summaries preserve recorded site and topology-version boundaries. See
[CT1 retention](../development/ct1-polling-metrics.md#semantics-and-retention) for
query routing and the automatic migration backfill.
`container_module_latest_metrics` supports lists without scanning module history.
Source time controls freshness, while collection time distinguishes observations
whose aggregate source timestamps tie. Retrying the same observation is
idempotent. Late samples can enter history without replacing newer current
values. A poll with all module measurements unavailable writes an explicit
empty observation. It clears current values and reporting counts only when it
supersedes the latest sample in source order. Known health timestamps are
preserved even when stale or all measurements are unavailable; collection time
is used for empty observations only when no source timestamp exists. Samples
cannot be attached to a replacement with a different identity.
An accepted inventory poll can record explicit unavailability without a reported
module identity while preserving the last known identity. This exception applies
only to empty observations, never to measurements or delayed inventory polls.

Container card health summaries are computed from the same normalized module
state projection as inventory lists. Present tanks and current modules determine
membership; source timestamps and last-seen times determine freshness. The compact
per-tank counts remain part of the container API, without a duplicate stored JSON
snapshot. Container freshness comes from `container_inventory.observed_at`.
Device metrics and plant history remain separate. The collection payload named
`ContainerSnapshot` still transports observations into normalized inventory.
Controllers populate normalized inventory on their next successful poll;
the migrations do not invent history from old snapshots. Independent tank
measurements are not available through the currently consumed controller API;
tank summaries derive from module observations.

## Unified reads

`fleetinventory.v1.FleetInventoryService.ListInventory` provides three views:

| View             | Entity kinds              |
| ---------------- | ------------------------- |
| Facilities       | Buildings and containers  |
| Equipment groups | Racks and tanks           |
| Mining assets    | Paired miners and modules |

Each view has an explicit SQL query: `ListFleetFacilities`,
`ListFleetEquipmentGroups`, or `ListFleetMiningAssets`. Each query combines only
its two entity types before filtering and pagination; one filtered relation
supplies both the matching count and the page. The queries share the
`fleet_mining_asset` projection for miner/module membership, state, and current
measurements. Facilities aggregate those assets by facility; equipment groups
aggregate by rack or tank. Mining-asset reads do not aggregate parents. Parent
totals are computed at request time, while collection stores module history and
latest values.

Shared filters cover site, typed ancestors, entity kind, search, and telemetry
ranges. Sorting covers
name, hashrate, equipment input power, temperature, efficiency, asset
count, issues, and applicable miner metadata or rack zone. The cursor includes the sort value plus kind and ID, distinguishes null
values, and is bound to the request and its authority scopes. Live changes can
move rows between pages; cursors do not freeze telemetry.

Module names use `<container name> T<tank position> M<module position>`;
tank names use `<container name> T<tank position>`. The read projections derive
these names from current container metadata so display, search, and sorting
stay consistent after a rename. List names occupy a single line, with long
names truncated and the full name available on hover.

Parent totals require complete reporting coverage for the respective metric.
Reporting counts expose partial coverage rather than presenting an incomplete
sum as the whole parent. Module and parent efficiency is total equipment power divided by total
hashrate; individual miners retain their reported efficiency. Temperature range filters constrain the parent's minimum and maximum,
matching the existing aggregate filter contract.

Whole-container reported hashrate and power are separate response fields.
Shared power filters use equipment input power; they do not substitute facility
power. This avoids double counting or silently changing a measurement's scope.

`ListModuleHistory` returns source and collection timestamps and optional SI-unit
metrics with bounded pages. Its time window is start-inclusive/end-exclusive;
history pagination uses both timestamps. Existing organization/site RBAC scopes
are applied in SQL to lists, counts, and history. Module-specific leasing grants
are outside this implementation.

The Buildings, Racks, and Miners screens consume globally ordered inventory
pages. Detail payloads are enriched in batches after page selection so existing
building/rack metadata and miner actions can use their native types.
Miner enrichment rechecks the original permission scopes against placement from
the enrichment read before loading telemetry and collection details, so a move
to a forbidden site between reads cannot expose the new details.
Containers, tanks, and modules keep typed identities even when their numeric IDs coincide
with those of conventional entities. Container and tank list metrics use the
same equipment measurements as their filters. Module rows open a paginated,
read-only view of the past hour of telemetry.

Specialized filters retain their native applicability:

| Filter                                                               | Applicability                                                                 |
| -------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Site, search, shared telemetry ranges                                | All kinds in the selected view                                                |
| Typed facility/group ancestors                                       | Descendants of that kind and ID                                               |
| Miner component issues                                               | Miners, buildings, racks; never synthesized for modules                       |
| Building and zone placement                                          | Racks and conventional miners                                                 |
| Model, firmware, IP ranges/subnets, groups, pairing, voltage/current | Conventional miners                                                           |
| Native miner status filter                                           | Conventional miners; exact status semantics retained                          |
| Inventory state                                                      | Miners/modules through the API; native status controls retain miner semantics |

`miner_filter` reuses the existing miner-filter parser for validation and
normalization, narrows the result to miners, and combines with common filters
using AND. A filter on a property that does not exist for a module never matches
that module. Pairing filters operate within Fleet's visible paired,
authentication-needed, and default-password population; discovery/onboarding
continues to use its existing API. Unsupported filter/view combinations are
rejected. The client keeps exact miner-specific status choices in `miner_filter`
instead of broadening them to an attention bucket.

Module/tank rows cannot enter miner/rack selections. Bulk operations and CSV
export still apply to conventional miners, and CSV is labeled accordingly.
Container cards and rows open the native Fleet detail at `/containers/:identifier`.
It reads collected container metadata, tank/module inventory and controller history,
including for FleetNode-owned controllers. Tank navigation scopes the mining list
using the typed tank ancestor; module tiles and module rows open the native Fleet
module page at `/containers/:identifier/tanks/:tankId/modules/:moduleId`, with
collected metrics, hardware status, and stored performance history. Fan telemetry and equipment states are collected for this view. Container-reported power stays separate from tank/module equipment power.
Existing read-only roles lacking telemetry permissions retain their existing
metadata-only list paths. Rack rows additionally require rack-read scope. Tanks, modules, and module
history use their container’s current-site read permissions; tank rows do not
require rack-read, including in mixed rack/tank lists. More granular child-hardware
permissions are not implemented.
Building metadata remains visible under site-read permission even when its
telemetry is restricted. Telemetry is withheld before filtering and sorting,
and model/firmware choices include only miners permitted by every read scope.
No new leasing permission rules are introduced here.

The integration suite covers filtering, mixed cursors, topology reconciliation,
identity replacement, and historical observation ordering against TimescaleDB.
Large-fleet query performance has not yet been benchmarked; live cursor pages
remain subject to changes in telemetry and inventory between requests.


## Fan and equipment observations

The CT1 collector retains cooling fan component IDs, speed percentage, power,
direction, and alarm state from `/api/v2/telemetry/container`. Each fan keeps the
response source timestamp independently of inventory. No RPM or PWM reading is
inferred. `/api/v2/power/container/controls` is a separate bounded supplementary
read. It has no source timestamp, so successful read completion is its observation
time. Missing capability flags remain unknown; `stateKnown=false` becomes an
absent enabled value. Setpoints are labeled as setpoints, not measured output.

`container_fan_observation` stores one latest row per controller/component.
`container_controls_observation` orders successful control reads and stores
capability presence; its normalized control and setpoint children are replaced
atomically for a newer response. Omitted children are no longer reported; failure
or malformed JSON does not create a successful observation. An explicitly
unsupported response replaces old capabilities. Fan rows absent from a partial
response retain their original time and become stale. Equal or older timestamps
cannot replace current readings. These tables have no historical retention or
whole-container JSON cache. Both direct and FleetNode JSON telemetry use this path.

The container API projects these observations after current-site authorization.
Fans and controls become stale independently after two minutes. Fleet displays
stale labels and hides retained measurements, preserves measured zero, and
enables equipment commands only with current known state, advertised support, and
current-site write permission. Viewing requires no controller URL.

Backend support was verified against Frigate main commit
`61f1f4c3a073f65d71dcb506e74d5db5a485bb47`: `pkg/api/power_container_controls.go`,
`pkg/api/telemetry_container.go`, and Trident96's `container_controls.go` implement
these reads. This verifies backend implementation, not deployment to a specific
physical controller. Unsupported backends and unavailable endpoints remain valid
collection outcomes.

## Equipment commands

`ContainerService.SetEquipmentControl` accepts a controller identifier, one of
six named boolean controls (`dry_cooler`, `dry_cooler_auto`, `tank_led_light`,
`coolant_pump`, `air_conditioner_1`, `air_conditioner_2`), or the dedicated
`main_circuit_breaker`, and a required desired state. For the breaker, true means
closed and false means open. Alarm actions, reverse procedures, setpoints, and
module mining commands are excluded.

Dispatch resolves an organization-owned, paired controller and locks its current
site assignment through batch creation and enqueue. It requires site/fleet/miner
read access and **both** `miner:set_cooling_mode` and `miner:set_power_target` at
that site. The container response reports this permission decision for UI gating;
dispatch rechecks it. A locator-LED grant alone does not authorize plant lighting.
Only one equipment command can be pending per controller. Existing command
batches, actor attribution, device results, and activity completion records are
reused. The container-scoped results endpoint verifies current-site read access
and single-controller batch ownership, including for site-scoped operators.

Direct workers use the optional SDK equipment interface and plugin RPC. FleetNode
commands require protocol V2, with V1 retained for existing actions; older nodes
return an upgrade-required error before enqueueing transport work. The node
constructs a container-class device. The CT1 driver rereads capabilities and known
state immediately before each write. Missing support or readback blocks dispatch;
the PLC retains authority over interlocks and acceptance. Breaker writes require
the dedicated advertised capability and endpoint, never an AC-control alias.

Equipment write failures, including ambiguous disconnects and timeouts, are
terminal: Fleet does not automatically retry a potentially applied plant write.
The UI keeps observed state while pending and distinguishes write acknowledgment
from a fresh equipment observation newer than the server-recorded command
completion. It waits up to two minutes for matching readback, then reports an
unconfirmed physical state. Viewing and refreshing use Fleet APIs, with no browser
connection to the controller. Physical firmware deployment must still be verified
on the actual controller; contract and simulator tests are not device certification.


## Dry-cooler readouts

The dry-cooler card shows MDK's dedicated
`plant.dryCoolerInletTemperatureC` observation, retained in the existing
plant-metrics JSON, and the mean of the reported cooling-fan `speedPercent`
values. The percentage is labeled average fan speed; it is not the manual speed
target. The average is hidden unless every advertised fan has a current, valid
speed. Inlet temperature has its own freshness check, independent of equipment
switch observations. Missing or stale readings are hidden.

Auto mode's `dry_cooler_auto_temp_c` value is shown as a temperature target,
distinct from the measured inlet temperature. Both temperatures honor the
user's Celsius/Fahrenheit preference.

## Current module issue evidence

The CT1 bulk map supplies current board errors/warnings and explicit hashrail,
reference-clock, and PSU thermal flags. Issues retain board MAC attribution or
belong to the single module PSU. Shared PSU entries deduplicate by component and
cause; repeated thermal flags use the freshest PSU state (source board timestamp
minus its reported sample age), including a clear reading. Historical shutdown
errors are excluded. Mixed board states are labeled observations, never hardware
faults. An attention classification alone supplies no invented fault.

Issue evidence is bounded to 64 causes per module, 256 bytes per cause, 128 bytes
per component identity, and a shared 1 MiB budget per container poll. Oversized
causes are omitted; retained causes remain visible in a partial report. Truncated
reports omit clear-source watermarks so a later full report can recover without
the discarded evidence having advanced source ordering.

The SDK transports a current issue set with individual issue timestamps and
source times for clear board/PSU readings. `container_module.issues` stores this
small evidence payload; separate source watermarks prevent an older component
sample from replacing newer evidence, even when the aggregate oldest timestamp
ties. Inventory collection order still guards topology changes. Replacement
modules get fresh Fleet identities and their own issue sets and watermarks.
Source timestamps must be nonzero and no more than five seconds ahead of collection.
The aggregate issue timestamp must not be newer than any supplied source. A
complete CT1 report requires timestamps for all three boards and the shared PSU;
malformed reports are rejected before inventory or source watermarks change.
Watermarks advance only with accepted evidence; a rejected mixed-age report cannot
block a corrected report carrying the same newer component sample.
Missing cause arrays, missing PSU state, or incomplete evidence withdraw health
confirmation while preserving source ordering; only advancing source evidence
restores it. Fresh complete clear
observations replace the issue set, so resolved causes disappear. A partial
fresh observation can expose explicit causes without confirming overall health.
Repeating identical partial evidence retains those causes without advancing its
source timestamps or extending their freshness.

The inventory API includes module evidence directly in the authorized page read.
It hides each cause after two minutes based on its own source time, and hides all
causes when module collection is stale. An outdated board withdraws health
confirmation without hiding fresh causes from other components. Unknown and stale evidence are distinct from a complete
observation with no reported causes. Fleet's Issues cell opens all causes,
components, severity (fault, warning, or observation), and source timestamps.
Cached client causes also expire independently if list refreshes stop.
This is current evidence only; historical issue lifecycle, parent issue rollups,
and the existing miner-specific component filters remain separate concerns.
