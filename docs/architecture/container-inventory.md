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

`container_module_metrics` retains 30 days of raw observations.
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

The existing container snapshot, device metrics, and plant history remain in
use. Controllers populate normalized inventory on their next successful poll;
the migration does not invent history from old snapshots. Independent tank
measurements are not available through the currently consumed controller API;
tank summaries derive from module observations.

## Unified reads

`fleetinventory.v1.FleetInventoryService.ListInventory` provides three views:

| View | Entity kinds |
| --- | --- |
| Facilities | Buildings and containers |
| Equipment groups | Racks and tanks |
| Mining assets | Paired miners and modules |

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

| Filter | Applicability |
| --- | --- |
| Site, search, shared telemetry ranges | All kinds in the selected view |
| Typed facility/group ancestors | Descendants of that kind and ID |
| Miner component issues | Miners, buildings, racks; never synthesized for modules |
| Building and zone placement | Racks and conventional miners |
| Model, firmware, IP ranges/subnets, groups, pairing, voltage/current | Conventional miners |
| Native miner status filter | Conventional miners; exact status semantics retained |
| Inventory state | Miners/modules through the API; native status controls retain miner semantics |

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
Tank and container navigation scopes the mining list using typed ancestors.
Existing read-only roles lacking telemetry permissions retain their existing
metadata-only list paths. Equipment-group reads also require rack-read scope.
Building metadata remains visible under site-read permission even when its
telemetry is restricted. Telemetry is withheld before filtering and sorting,
and model/firmware choices include only miners permitted by every read scope.
No new leasing permission rules are introduced here.

The integration suite covers filtering, mixed cursors, topology reconciliation,
identity replacement, and historical observation ordering against TimescaleDB.
Large-fleet query performance has not yet been benchmarked; live cursor pages
remain subject to changes in telemetry and inventory between requests.
