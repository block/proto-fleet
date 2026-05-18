---
title: "#229 — Multi-site: building + zone filter on miner list (and rack list)"
date: 2026-05-14
status: draft
type: plan
---

## Summary

Multi-site PR C (#228) introduced `site_ids` + `include_unassigned` on
`MinerListFilter`, but the list query still carries the org-wide flat
`zones: repeated string` filter from before buildings existed. Two
racks in different buildings can share the same zone string ("Room 2")
and the org-wide filter collapses them into one bucket. There is also
no building filter.

This plan adds:

1. `MinerListFilter.building_ids` + `include_no_building` (peer to
   `site_ids` + `include_unassigned`).
2. `MinerListFilter.zone_keys: repeated ZoneKey { building_id, zone }`
   — a building-scoped zone filter. `building_id = 0` is a
   transitional sentinel meaning "match this zone label across all
   buildings," covering today's no-buildings-in-UI case. See
   §Forward-look note for why it's left in place rather than
   removed in Phase 2.
3. Same upgrade on `ListDeviceSetsRequest` (rack list filter):
   `building_ids` + `zone_keys` + `include_no_building`.
4. `ListRackZonesResponse` reshape to return composite
   `repeated ZoneRef { int64 site_id; string site_label; int64
   building_id; string building_name; string zone; }` so the
   miner-list and rack-list dropdowns can display
   `Site A — Building 1 — Room 2`.
5. Validation: `maxFreeFormFilterValues` cap on the new arrays;
   reject `building_id < 0` and blank `zone`; reject explicit
   `building_id > 0` not in the caller's org with `INVALID_ARGUMENT`.
6. Old flat `zones: repeated string` field on `MinerListFilter` (10)
   and `ListDeviceSetsRequest` (6) stays in the proto as
   `[deprecated = true]`. The server translates non-empty `zones`
   into wildcard ZoneKeys (`building_id = 0`) so older clients keep
   working without a coordinated FE migration. New callers should
   emit `zone_keys` directly with explicit `building_id`. A follow-up
   issue tracks reserving the field numbers once telemetry confirms
   no live client emits the field.

Storage stays put. No DB migration. No `zone` entity. See the
multi-site plan §Storage model "Forward look" callout for when zone
promotion becomes worth it.

This plan covers the **backend** (proto, server validation, SQL
builders, integration tests) plus the minimum frontend patch needed to
not ship a broken miner-list dropdown. Cascading site→building→zone
picker UI is a Phase 2 follow-up.

## Goals

- A miner-list zone filter that distinguishes `("Building 1", "Room
  2")` from `("Building 2", "Room 2")` when callers ask for it.
- A building filter on miner list, symmetric in shape to the existing
  `site_ids` filter.
- Same upgrades on the rack list so dropdown UX stays consistent
  between the two surfaces.
- A single dropdown data source (`ListRackZones`) returning composite
  zones with enough context for the UI to render
  `Site — Building — Zone` without an extra round trip.
- Today's no-buildings-in-UI dropdown keeps working unchanged — the
  FE still emits `zones: [string]`; the server translates to
  wildcard ZoneKeys for backward compat. Phase 2's building-picker
  UI ships the FE migration to `zone_keys` directly.
- Deferred FE migration: this PR is server-only on the wire. The
  legacy `zones` field stays in the proto with `[deprecated = true]`
  so existing clients work unchanged. Removal of the deprecated
  field is tracked as a follow-up issue gated on telemetry that no
  live client emits it.

## Non-goals

- Promoting `zone` to its own entity (rack `zone_id` FK, `zone` table,
  `building.zones[]` array). Tracked separately in the multi-site
  plan's "Forward look" callout.
- A first-class cascading site → building → zone picker component.
  Today's flat dropdown gets richer labels; the cascading picker is a
  follow-up.
- Backfilling building context for pre-multi-site history rows.
  Phase 1 boundary already locked in the multi-site plan.
- Rack-list site filter, miner-list `floor` / `aisle` filters,
  per-zone capacity attributes. Out of scope.

## Wire contract

### New message: `ZoneKey` (and `ZoneRef`)

Both new messages live in a new file `proto/common/v1/zone.proto`
under the existing `common.v1` package. `fleetmanagement.v1` and
`device_set.v1` import from there — the same pattern
`collection.proto` already uses for `common/v1/device_selector.proto`
and `common/v1/sort.proto`. One definition, two consumers.

```proto
// proto/common/v1/zone.proto
syntax = "proto3";
package common.v1;

import "buf/validate/validate.proto";

// A building-scoped zone identifier. Two ZoneKeys with the same
// `zone` but different `building_id` refer to physically different
// zones.
//
// `building_id = 0` is a sentinel meaning "match this zone label
// across all buildings in the caller's org." This is the shape
// clients send when they have no building picker (today's UI).
// When `building_id > 0`, the match is scoped to that one
// building.
message ZoneKey {
  int64 building_id = 1 [(buf.validate.field).int64.gte = 0];
  string zone = 2 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
}

// Denormalized zone reference returned by ListRackZones. See
// device_set.v1.ListRackZonesResponse for usage.
message ZoneRef {
  int64 building_id = 1;
  string building_label = 2;
  int64 site_id = 3;
  string site_label = 4;
  string zone = 5;
}
```

`fleetmanagement.v1.fleetmanagement.proto` and
`device_set.v1.device_set.proto` add `import "common/v1/zone.proto";`
and reference fields as `common.v1.ZoneKey` / `common.v1.ZoneRef`.
The `ZoneRef` block in §`ListRackZonesResponse reshape` below shows
only the field layout — the actual message definition lives here.

### `MinerListFilter` changes

```proto
message MinerListFilter {
  // … existing fields …

  reserved 10;
  reserved "zones";

  // … existing fields …
  // bool include_unassigned = 14;

  // Filter by building IDs. Returns miners assigned (via rack
  // membership) to ANY of the specified buildings. When combined
  // with include_no_building, miners whose rack has building_id IS
  // NULL — and miners not in any rack — are also included.
  repeated int64 building_ids = 15;

  // When true, miners whose rack has building_id IS NULL are
  // included. This is the "racked but no building assignment"
  // bucket. Independent of building_ids — set only this to filter
  // that bucket alone; set both to combine specific buildings with
  // racked-but-unassigned.
  bool include_no_building = 16;

  // Filter by zone keys (acts as OR condition across entries).
  // Each entry is a (building_id, zone) pair; building_id=0 matches
  // any building. Miners not in a rack are excluded by this filter.
  // When combined with other filters, uses AND logic.
  repeated common.v1.ZoneKey zone_keys = 17;

  // When true, miners with no rack membership at all are included.
  // Distinct from include_no_building, which surfaces racked
  // miners without a building. Use this to triage unracked /
  // newly-paired devices.
  bool include_no_rack = 18;
}
```

### `DeviceSetFilter` (rack list) changes

```proto
// proto/device_set/v1/device_set.proto

message ListDeviceSetsRequest {
  // … existing fields …

  reserved 6;
  reserved "zones";

  // Filter racks by building. Only valid when type is RACK.
  repeated int64 building_ids = 7;

  // When true, racks with building_id IS NULL are included. Only
  // valid when type is RACK.
  bool include_no_building = 8;

  // Filter by zone keys. Only valid when type is RACK. Each entry
  // is a (building_id, zone) pair; building_id=0 matches any
  // building.
  repeated common.v1.ZoneKey zone_keys = 9;

  // (No include_no_rack here — racks are the entity, so "no rack"
  // is not a meaningful slice of a rack list.)
}
```

### `ListRackZonesResponse` reshape

Both `device_set.v1.ListRackZones` and `collection.v1.ListRackZones`
exist; both return composite zones now.

```proto
// proto/device_set/v1/device_set.proto

import "common/v1/zone.proto";

message ListRackZonesResponse {
  // All distinct (building_id, zone) pairs across the org's racks,
  // sorted by site_label, then building_label, then zone. ZoneRef
  // shape: see proto/common/v1/zone.proto.
  repeated common.v1.ZoneRef zones = 1;
}
```

This is a breaking change to the old `repeated string zones = 1`
response.

**Naming precedent.** `building_label` / `site_label` mirror the
existing denormalized-projection pattern on
`Device.site_label = 26` (fleetmanagement.proto:158). Source
entities (`Building.name`, `Site.name`) keep their `name` field;
denormalized references onto non-entity messages use `*_label`.
This avoids two API calls for the dropdown — `ListBuildings` would
be a second round trip for data already joinable at query time.

**Cache staleness on rename.** Renaming a building or site
invalidates cached `ListRackZones` responses for the org. FE
should not cache the dropdown source across long sessions; the
dropdown re-fetches on filter-panel open (existing pattern).

### Reserved field numbers

- `MinerListFilter`: `reserved 10; reserved "zones";`
- `ListDeviceSetsRequest`: `reserved 6; reserved "zones";`

Done in the same PR as the new shape. No deprecated-field
deprecation window. The wildcard sentinel inside `ZoneKey` carries
the legacy "any building" shape that today's no-buildings-UI
clients need.

### Forward-look note: the wildcard is transitional

Once the buildings UI ships (Phase 2 site map + per-rack building
selector), zones can only be created on racks whose `building_id`
is set, and rack-move-out-of-building cascades clear the zone
string (this rule already exists in PR C / #197). At that point
`building_id = 0` ZoneKeys still parse and execute, but
well-formed clients have no reason to emit one — every zone the
operator sees in the dropdown carries a real `building_id`.

The sentinel survives because removing it would be a breaking
proto change for a marginal cleanup win, not because we've
identified a forever-workflow that needs it. If a concrete
operator workflow for "any building zone match" surfaces later
(cross-building thermal aggregates, fleet-wide cold-aisle
queries), the sentinel is already in place. If it doesn't, the
sentinel is harmless dead surface area, not a feature.

## Go domain (`MinerFilter`) changes

```go
// server/internal/domain/stores/interfaces/device.go

type ZoneKey struct {
    BuildingID int64 // 0 = wildcard across all buildings
    Zone       string
}

type MinerFilter struct {
    // … existing fields …

    // Removed: Zones []string

    // … existing fields …
    BuildingIDs       []int64
    IncludeNoBuilding bool // rack has building_id IS NULL
    ZoneKeys          []ZoneKey
    IncludeNoRack     bool // no rack membership at all
}
```

### `DeviceSetFilter` — new struct (interface refactor)

`CollectionStore.ListCollections` today takes 8 positional args
(`interfaces/collection.go:116`):

```go
ListCollections(ctx, orgID, collectionType, pageSize, pageToken,
    sort, errorComponentTypes, zones) (..., error)
```

Adding `building_ids`, `include_no_building`, `zone_keys` to this
shape pushes the signature to 11 args. Instead, introduce a
`DeviceSetFilter` struct mirroring `MinerFilter` and migrate the
call sites:

```go
type DeviceSetFilter struct {
    ErrorComponentTypes []int32
    BuildingIDs         []int64
    IncludeNoBuilding   bool
    ZoneKeys            []ZoneKey
}

ListCollections(
    ctx context.Context,
    orgID int64,
    collectionType pb.CollectionType,
    filter *DeviceSetFilter,
    pageSize int32,
    pageToken string,
    sort *SortConfig,
) (..., error)
```

One-shot refactor: regenerate `mock_collection_store.go`, update
every call site (~6 in handlers + tests), and any `ListCollections`
test helpers. Future filter additions (rack-list site filter, etc.)
land as struct field additions instead of signature changes.

`MinerFilter.Zones` field is deleted from the struct. Existing
references in SQL builders (`zonesFilter`, `zoneValues`) and tests
get removed.

## Server validation (`parseFilter`)

### Signature change (P1 risk)

Today's `parseFilter` is pure:

```go
// server/internal/domain/fleetmanagement/service.go:655
func parseFilter(pbFilter *pb.MinerListFilter) (*interfaces.MinerFilter, error)
```

No `ctx`, no `orgID`, no store. `site_ids` validation (the
purported precedent for cross-org checks) does **only** positivity
+ cap — it does not query the DB. Adding the cross-org building
check requires changing the signature:

```go
func parseFilter(
    ctx context.Context,
    orgID int64,
    pbFilter *pb.MinerListFilter,
    buildingStore stores.BuildingStore,
) (*interfaces.MinerFilter, error)
```

…and threading the new arguments through every call site:

- `service.go:275` — `ListMiners` handler.
- `service.go:310` — `ListMinerStateSnapshots` / streaming path.
- `service.go:1128` — `DeleteMiners` / `RenameMiners` (the
  `DeviceSelector.all_devices` branch).
- `export_csv.go:50` — `ExportMinerListCsv` handler.
- `parse_filter_test.go` — every existing test (15+ cases) gets
  updated to pass a stub `BuildingStore` and a test `orgID`.

`interfaces/building.go` adds a new bulk helper:

```go
// BuildingsByIDs returns building IDs from the provided set that
// belong to the org (rows present, not soft-deleted). Caller compares
// the returned set against the requested set to detect cross-org or
// missing IDs.
BuildingsByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error)
```

Existing `BuildingBelongsToOrg(ctx, orgID, id)` is single-ID and
inadequate for the bulk-check pattern.

### Validators

```go
if len(pbFilter.BuildingIds) > maxFreeFormFilterValues {
    return nil, fleeterror.NewInvalidArgument(
        "building_ids exceeds maximum of %d values", maxFreeFormFilterValues)
}
for _, id := range pbFilter.BuildingIds {
    if id <= 0 {
        return nil, fleeterror.NewInvalidArgument(
            "building_ids must contain only positive IDs")
    }
}

if len(pbFilter.ZoneKeys) > maxFreeFormFilterValues {
    return nil, fleeterror.NewInvalidArgument(
        "zone_keys exceeds maximum of %d values", maxFreeFormFilterValues)
}
for _, zk := range pbFilter.ZoneKeys {
    if zk.BuildingId < 0 {
        return nil, fleeterror.NewInvalidArgument(
            "zone_keys entries must have non-negative building_id")
    }
    if zk.Zone == "" {
        return nil, fleeterror.NewInvalidArgument(
            "zone_keys entries must have non-empty zone")
    }
}
```

### Cross-org rejection

Collect every explicit `building_id > 0` from `building_ids` and from
each `zone_keys` entry into one set. Call `BuildingsByIDs(ctx, orgID,
ids)`. If the returned set is smaller than the requested set, reject
with `INVALID_ARGUMENT`. Wildcard `building_id == 0` entries are not
in the requested set (no specific building to check).

**Error message must not echo the rejected IDs.** A message like
"building_id 42 not in your org" lets a caller enumerate building
IDs across orgs by probing. Use a generic shape:

```go
return nil, fleeterror.NewInvalidArgument(
    "one or more building_ids reference buildings outside the caller's org")
```

Same wording covers both `building_ids` and `zone_keys.building_id`
rejection paths.

**Audit logging on rejection.** Emit a WARN-level structured log
when the bulk check returns a smaller set than requested. Fields:
`org_id` (the caller), `rejected_count` (an integer, not the IDs),
and a fixed `event = "cross_org_filter_probe"` tag for dashboard
aggregation. Do not include the rejected building IDs in the log —
that would record another org's internal identifiers in the
caller's org's logs.

### Cap value confirmation

`maxFreeFormFilterValues = 1024`. Real fleets have <20 buildings and
<50 zones; the cap exists purely to keep `= ANY($N::text[])` planner
cost bounded under hostile input. Same constant for all new fields.

## SQL builder (`device_filters.go`)

### `building_ids` predicate

Mirrors the existing `rack_ids` EXISTS shape but joins through
`device_set_rack.building_id`:

```sql
AND EXISTS (
  SELECT 1
  FROM device_set_membership dcm
  JOIN device_set ds ON ds.id = dcm.device_set_id
  JOIN device_set_rack dsr ON dsr.device_set_id = dcm.device_set_id
  WHERE dcm.device_id = device.id
    AND dcm.org_id = $orgID
    AND dcm.device_set_type = 'rack'
    AND ds.deleted_at IS NULL
    AND dsr.building_id = ANY($buildingIDs::bigint[])
)
```

### `include_no_building` predicate

Matches devices whose rack membership row exists AND the rack's
`building_id IS NULL`. Single EXISTS branch over
`device_set_membership → device_set_rack` with `dsr.building_id IS
NULL`. Devices with no rack membership do NOT satisfy this
predicate — they need `include_no_rack` instead.

OR'd with the `building_ids` predicate at the top level when both
are set (e.g., `building_ids = [B1] + include_no_building = true`
returns devices in B1 OR in racks with no building).

### `include_no_rack` predicate

Matches devices with no rack membership row at all. Single NOT
EXISTS over `device_set_membership` filtered to
`device_set_type = 'rack'`. Pure absence-of-membership predicate.
This is the bucket for newly-paired or staging devices.

OR'd at the top level with `building_ids` / `include_no_building` /
`zone_keys` when any of those are set. Each filter's positive
predicate identifies a population; setting their corresponding
"include" bool widens the union.

### `zone_keys` composite predicate

`parseFilter` partitions a single `zone_keys` array into two slices
before handing them to the SQL builder:

- `scopedBuildingIDs[]` / `scopedZones[]` — entries with
  `building_id > 0`. Parallel arrays of equal length.
- `wildcardZones[]` — entries with `building_id == 0`.

The SQL builder emits whichever branches have data, OR'd inside one
EXISTS:

```sql
AND EXISTS (
  SELECT 1
  FROM device_set_membership dcm
  JOIN device_set ds ON ds.id = dcm.device_set_id
  JOIN device_set_rack dsr ON dsr.device_set_id = dcm.device_set_id
  WHERE dcm.device_id = device.id
    AND dcm.org_id = $orgID
    AND dcm.device_set_type = 'rack'
    AND ds.deleted_at IS NULL
    AND (
      -- scoped branch, present when scopedBuildingIDs[] non-empty
      (dsr.building_id, dsr.zone) IN (
        SELECT b, z FROM UNNEST($scopedBuildingIDs::bigint[],
                                $scopedZones::text[]) AS t(b, z)
      )
      OR
      -- wildcard branch, present when wildcardZones[] non-empty
      dsr.zone = ANY($wildcardZones::text[])
    )
)
```

Either branch can be omitted when its slice is empty (the OR
collapses). When both slices are empty the entire predicate is
omitted.

**Wildcard subsumption is intentional, not validated.** If a
request contains both `{0, "Room 2"}` and `{B1, "Room 2"}`, the
wildcard branch matches every "Room 2" across the org, making the
scoped entry redundant. parseFilter does not detect or reject this
case — the dominance is a property of OR semantics, not a bug.
Saved-view migration emits wildcards by design (legacy → `{0,
zone}`), so this shape arrives naturally.

**Single-layer org defense — load-bearing constraint.** Wildcard
`zone_keys` (entries with `building_id == 0`) skip the parseFilter
cross-org ownership check by design — there is no specific
`building_id` to check. The only org boundary on the wildcard path
is the EXISTS subquery's `dcm.org_id = $orgID` clause. This
single-layer defense MUST be preserved by every future refactor.
Mitigation: a load-bearing comment lives next to the wildcard
branch in `device_filters.go` calling out the constraint, and
`device_filters_orgid_audit_test.go` asserts via regex that every
emitted predicate for the `zone_keys` (and `building_ids`,
`include_no_building`) paths includes an `org_id` bind reference.
The audit test is mechanical — it does not validate SQL semantics,
only that the clause is present.

Alternative SQL shapes to benchmark at code time:
- `UNNEST` + JOIN instead of `IN (SELECT ... UNNEST ...)` — same
  semantics, planner may prefer one or the other.
- Treat wildcard as `(0, zone)` and rewrite the scoped branch's
  `(dsr.building_id, dsr.zone) IN (...)` to be `(0 OR
  dsr.building_id, dsr.zone) IN (...)` — uglier, drop.

### Rack-list filter

`ListDeviceSets` queries hit `device_set_rack` directly — no
`device_set_membership` join needed. `building_ids` becomes
`AND dsr.building_id = ANY($1::bigint[])`. `zone_keys` uses the same
two-branch partition pattern, joined directly on `dsr`.
`include_no_building` becomes `OR dsr.building_id IS NULL`.

## `ListRackZones` server-side change

Replace the current `SELECT DISTINCT dsr.zone` query with a join to
`building` + `site`:

```sql
-- name: ListRackZones :many
SELECT DISTINCT
    COALESCE(dsr.building_id, 0) AS building_id,
    COALESCE(b.name, '')         AS building_label,
    COALESCE(b.site_id, 0)       AS site_id,
    COALESCE(s.name, '')         AS site_label,
    dsr.zone
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
LEFT JOIN building b ON b.id = dsr.building_id AND b.org_id = $1 AND b.deleted_at IS NULL
LEFT JOIN site s     ON s.id = b.site_id      AND s.org_id = $1 AND s.deleted_at IS NULL
WHERE ds.org_id = $1
  AND ds.deleted_at IS NULL
  AND dsr.zone IS NOT NULL
  AND dsr.zone != ''
ORDER BY site_label, building_label, dsr.zone;
```

Notes:
- Racks with `building_id IS NULL` still surface (legacy / Phase 1
  uncategorized racks); `building_id = 0` + empty `building_label`
  / `site_label` in the response. UI renders these as
  `Unassigned — Room 2` so operators can see and migrate them.
- Sort order matches the dropdown grouping (`Site A — Building 1
  — Zone X`).
- **`collection.v1.ListRackZones` is frozen on the old `string[]`
  shape.** That service is already marked deprecated
  (`collection.proto:12`). `device_set.v1.ListRackZones` handler
  stops delegating to `collection.v1` (today's path at
  `handlers/deviceset/handler.go:211-218`) and calls the store
  directly, returning `ZoneRef[]`. No `ZoneRef` duplication in
  `collection.proto`. When `collection.v1` eventually retires, its
  handler and proto definition retire with it.

## FE migration (this PR series)

Constraint: every consumer of the dropped `MinerListFilter.zones`
field, the dropped `ListDeviceSetsRequest.zones` field, and the
reshaped `ListRackZonesResponse` must update in this same PR series
— `just gen` regenerates proto stubs, and stale call sites
hard-fail at TypeScript compile time.

### Miner-list surface

- **`MinerList/MinerList.tsx`**
  - `availableZones` today derives from `availableRacks.forEach(r =>
    zones.add(r.typeDetails.value.zone))` at lines 631-640 — a
    client-side aggregation over rack metadata. After the response
    reshape, RackInfo still carries `zone` but no `building_id`, so
    this aggregation cannot construct ZoneKeys. Replace the
    in-memory source with the `listRackZones` query and feed
    `ZoneRef[]` directly into the dropdown options.
  - **Label rule:** bare `zone` (e.g. `"Room 2"`) when
    `building_id == 0`. Enhanced `"{site_label} — {building_label}
    — {zone}"` when `building_id > 0`. Today's UI never sees the
    enhanced form (no buildings exist yet); it kicks in once
    operators create buildings.
  - Line 880 `minerFilter.zones.push(...zoneFilters)` becomes
    `minerFilter.zoneKeys.push(...zoneKeyFilters)`. Selection
    payload changes from string to `{building_id, zone}`.

- **`features/fleetManagement/utils/filterUrlParams.ts`**
  - Line 159-161 (encode) writes `URL_PARAMS.ZONE` from
    `filter.zones`. Switch to encoding `filter.zoneKeys` — likely
    via two URL params (`zone` for the label, `building_id` for the
    scope) or a single composite param. Lock the URL serialization
    shape in this plan before code time.
  - Line 251-253 (decode) reads `URL_PARAMS.ZONE` into
    `filter.zones`. Mirror change.
  - Legacy URL params containing bare zone names get translated to
    `zoneKeys: [{building_id: 0, zone: value}]` — preserves
    bookmarks at the wildcard sentinel. (Saved-view migration UX
    signal is a separate walk-through finding.)

- **`features/fleetManagement/utils/fleetVisiblePairingFilter.ts`**
  Line 36 references `filter?.zones`. The pairing-filter helper
  forwards or stubs the field — update to use `zoneKeys` (or drop
  the field reference if it's pass-through).

### Rack-management surface (NEW — plan v1 missed these)

The rack list **already exposes** a zone filter UI and **already
calls** `listRackZones`. The plan can't pretend otherwise.

- **`features/rackManagement/pages/RacksPage.tsx`**
  - Line 46-50 destructures `listRackZones` and tracks
    `allZones: { id: string; label: string }[]`. Reshape the
    response handler at line 87-91 to map `ZoneRef[]` to a
    composite option list (label = `Site — Building — Zone`).
  - The FilterChip backed by zones at line 117-132 keeps working
    via the same shim shape as the miner list: when the operator
    selects a zone, the FE sends `zone_keys: [{building_id:
    ref.building_id, zone: ref.zone}]`. Zones from the Unassigned
    bucket (`building_id == 0`) preserve today's org-wide match.
    Building scope follows in Phase 2.

- **`api/useDeviceSets.ts`**
  - `listRacks` at line 357-378 forwards `zones: zones ?? []` to
    `listDeviceSets`. Update its arg shape from `zones: string[]` to
    `zoneKeys: ZoneKey[]` to match the new request type.
  - `ListRackZonesProps` type at line 100-104 expects
    `onSuccess(zones: string[])`. Update to `onSuccess(zones:
    ZoneRef[])`.

- **`features/rackManagement/components/RackSettingsModal.tsx`**
  - Line 70 destructures `listRackZones`. Line 106 tracks
    `zoneSuggestions: string[]`. Line 117-122 calls `listRackZones`
    and sets `setZoneSuggestions(zones)`.
  - The modal uses zone names for autocomplete inside a *single*
    rack's create / edit flow, where the building context is the
    rack's own `building_id`. After the reshape, filter
    `ZoneRef[]` to entries matching the modal's current
    `building_id` and pass only the zone labels into the
    autocomplete.

### Saved views

Existing saved views serialize the old `zones: [string]`. On read,
translate each legacy string into `{building_id: 0, zone:
legacyString}` — a dumb 1:1 transform, no fuzzy auto-matching
against the current building list. Operators keep the same result
set they had before; they can refine to a specific building later.
New saved views write the composite shape directly.

(Whether the dropdown also surfaces a visible signal that a saved
view contains a wildcard is a separate walk-through finding — the
plan's current locked answer is "no signal," which the design-lens
and product-lens reviewers pushed back on.)

### Stories / mocks

- `MinerList/stories/mocks.ts`
- `MinerList/stories/statusMocks.ts`
- Any `useDeviceSets` test fixtures returning `ListRackZonesResponse`.

### Other MinerListFilter consumers (mandatory audit)

`MinerListFilter` is embedded in multiple request types beyond
`ListMiners` / `StreamFilteredMinerList`:

- `proto/fleetmanagement/v1/fleetmanagement.proto:212` —
  `ListMinerStateSnapshotsRequest.filter`
- `proto/fleetmanagement/v1/fleetmanagement.proto:381` —
  `ExportMinerListCsvRequest.filter`
- `proto/fleetmanagement/v1/fleetmanagement.proto:444` —
  `GetMinerModelGroupsRequest.filter`
- `proto/fleetmanagement/v1/fleetmanagement.proto:472` —
  `DeviceSelector.all_devices` (used by `DeleteMiners`,
  `RenameMiners`, pool-needed-count, auth-needed)
- `GetMinerStateCountsRequest.filter`

Every FE hook that constructs or forwards `MinerListFilter` must be
checked. Either it pass-throughs from miner-list state (no code
change beyond compile-error fixes from the field rename), or it
constructs its own filter (explicit migration required). Mandatory
audit list at code time:

- `client/src/protoFleet/api/usePoolNeededCount.ts`
- `client/src/protoFleet/api/useExportMinerListCsv.ts`
- `client/src/protoFleet/api/useMinerModelGroups.ts`
- `client/src/protoFleet/api/fetchAllMinerSnapshots.ts`
- `client/src/protoFleet/api/useAuthNeededMiners.ts`
- `client/src/protoFleet/features/.../ManageMinersModal.tsx`

For each: confirm whether the hook constructs `zones` or forwards
the field unchanged. Constructed callers migrate to `zoneKeys`
(wildcard `building_id = 0` when no building context). Forwarded
callers compile-fix automatically.

Server handlers for the listed RPCs all route through
`parseFilter()`, so the new validation + cross-org check fires
uniformly. No per-handler logic update beyond the signature change
already documented in §Server validation.

### No new building-filter UI

Backend ready; UI follows Phase 2's site-aware miner list redesign.

## Tests

### `parse_filter_test.go`

- `building_ids` happy path (single + multiple).
- `building_ids` over cap → `INVALID_ARGUMENT`.
- `building_ids` containing 0 or negative → `INVALID_ARGUMENT`.
- `building_ids` containing a building not in caller's org →
  `INVALID_ARGUMENT`.
- `zone_keys` happy path: all scoped.
- `zone_keys` happy path: all wildcard (`building_id = 0`).
- `zone_keys` mixed scoped + wildcard in the same array.
- `zone_keys` over cap.
- `zone_keys` with negative `building_id` or empty `zone`.
- `zone_keys` with scoped `building_id > 0` not in caller's org →
  `INVALID_ARGUMENT`. Wildcard entries don't trigger the ownership
  check.
- `include_no_building` alone → no validation error, sets the bool.
- `include_no_rack` alone → no validation error, sets the bool.
- Both `include_no_building` and `include_no_rack` set → both
  predicates emit, union as expected.
- All combinations: `(building_ids, include_no_building,
  include_no_rack, zone_keys, site_ids, include_unassigned)`
  intersection table.
- Old `zones (10)` is now reserved. No separate regression test —
  the `reserved 10; reserved "zones";` declarations in the proto
  cause `protoc` to reject any re-use at compile time.

### `device_filters_test.go` (SQL emission)

- `building_ids` only → one EXISTS predicate, one `bigint[]` arg.
- `include_no_building` only → emits the NULL/no-rack branch with no
  building array arg.
- `building_ids` + `include_no_building` → predicates OR'd.
- `zone_keys` all-scoped → emits the parallel-array `UNNEST` branch
  only.
- `zone_keys` all-wildcard → emits the `ANY($::text[])` branch only.
- `zone_keys` mixed → emits both branches OR'd inside one EXISTS.
- `building_ids` + `zone_keys` → both predicates at the top level,
  AND'd (different field semantics).
- All filters together → arg count / position correctness.

### `device_filters_integration_test.go`

Real DB. Fixture: two buildings in the same site, each with a rack
labeled zone `"Room 2"`, each rack with 2 devices. Plus one
no-building rack and one device with no rack at all.

- **Cross-building zone collision (scoped)**:
  `zone_keys = [{B1, "Room 2"}]` returns the 2 devices in B1 only,
  not the 4 devices org-wide. Proves the bug fix.
- **Wildcard zone match**:
  `zone_keys = [{0, "Room 2"}]` returns all 4 devices. Proves the
  shim shape preserves today's behavior.
- **Mixed scoped + wildcard**:
  `zone_keys = [{B1, "Room 2"}, {0, "Other Zone"}]` returns the
  union.
- **Building filter scope**: `building_ids = [B1]` returns all
  devices under racks in B1.
- **`include_no_building` semantics**: only the no-building rack's
  devices surface when `include_no_building = true`. The no-rack
  device does NOT surface (use `include_no_rack` for that).
- **`include_no_rack` semantics**: only the no-rack device surfaces
  when `include_no_rack = true`.
- **Both bools true**: union of both populations surfaces.
- **`include_no_building` + `building_ids` combined**: OR semantics
  inside the building branch.
- **`zone_keys` × `site_ids` intersection**: filtering both at once
  returns the AND.
- **`zone_keys` × `building_ids` intersection**: e.g.,
  `building_ids = [B1]` AND `zone_keys = [{B2, "Room 2"}]`
  returns 0 devices (top-level AND, scoped zone misses B1).
- **Cross-org isolation (scoped)**: org A's caller passing
  `zone_keys = [{B_orgB, "Room 2"}]` is rejected at parseFilter
  (cross-org check on explicit `building_id`).
- **Cross-org isolation (wildcard)**: org A's caller passing
  `zone_keys = [{0, "Room 2"}]` returns only org A's devices — the
  SQL builder's `org_id` predicate handles the org boundary even
  though the wildcard skipped the cross-org parseFilter check.
  Mirrors `TestZoneFilter_CrossOrgIsolation`.

### `ListRackZones` integration

- Returns composite entries, sorted by site/building/zone.
- Racks with `building_id IS NULL` surface with `building_id = 0`
  and empty building/site names.
- Cross-org isolation: a building in org B does not appear in org
  A's response.

## Dependencies

This plan rebases onto `issue-197` (PR #228, multi-site PR C) and
assumes the following are merged:

- **`device_set_rack.building_id` column** (nullable bigint FK to
  `building.id`).
- **`building` table** with `id`, `org_id`, `site_id`, `name`,
  `deleted_at`.
- **`site` table** with `id`, `org_id`, `name`, `deleted_at`.
- **PR C / #197 cascade rules**: rack move out of a building
  clears `dsr.zone` in the same transaction; building-site
  reassignment cascades to descendant racks/devices.

**If PR C is reverted post-merge:** this PR's SQL, validation, and
filter contract are invalid. Revert this PR with it; no partial
rollback path. The `building_id`/`site_id` columns this plan reads
from `device_set_rack` and the `building` / `site` tables this
plan joins to do not exist without PR C.

**If PR C is rebased / reshaped before merge:** re-verify
column/table names match before applying this plan. The shape this
plan reasons against is captured by current `issue-197` head; any
upstream rename ripples into §SQL builder and §ListRackZones.

## Rollout

- No DB migration. Pure wire + server-logic change.
- `just gen` regenerates Connect/sqlc/Go protos. Generated `.pb.go`
  and `.pb.ts` files change.
- Old clients (FE before this PR ships) break — `ListRackZones`
  response shape change and the dropped `zones` field both surface
  as compile errors. The same PR series ships the FE patch, so they
  merge together.
- Server is forward-clean from day one. No follow-up cleanup
  issue needed.

## File touch list (rebuilt from codebase)

Proto (3 source files, plus generated):
- **NEW** `proto/common/v1/zone.proto` — `ZoneKey` and `ZoneRef`
  message definitions. Both other proto files import from here.
- `proto/fleetmanagement/v1/fleetmanagement.proto` — imports
  `common/v1/zone.proto`; adds `building_ids`,
  `include_no_building`, `include_no_rack`, `zone_keys`; reserves
  `zones (10)`.
- `proto/device_set/v1/device_set.proto` — imports
  `common/v1/zone.proto`; adds `building_ids`,
  `include_no_building`, `zone_keys`; reserves `zones (6)`;
  reshapes `ListRackZonesResponse` to `repeated common.v1.ZoneRef`.
- `proto/collection/v1/collection.proto` — **no change**.
  `collection.v1.ListRackZonesResponse` stays on `repeated string`.
  Decision locked at walk-through: `device_set.v1` handler bypasses
  `collection.v1` and reads the store directly, so no `ZoneRef`
  duplication in `collection.proto`.
- Generated `.pb.go` and `.pb.ts` files via `just gen`.

Server (~13 — grew from v1's "9"):
- `internal/domain/fleetmanagement/service.go` — `parseFilter`
  signature change; new validation; cross-org bulk check; threading
  ctx + orgID + buildingStore through 4 call sites at lines 275, 310,
  1128, and the export-csv handler.
- `internal/domain/fleetmanagement/export_csv.go` — call-site update.
- `internal/domain/fleetmanagement/parse_filter_test.go` — every
  existing test updated for the new signature; new test cases.
- `internal/domain/stores/interfaces/device.go` — `ZoneKey` struct,
  `MinerFilter.{BuildingIDs, IncludeNoBuilding, ZoneKeys}`, remove
  `MinerFilter.Zones`.
- `internal/domain/stores/interfaces/building.go` — new
  `BuildingsByIDs(ctx, orgID, ids)` bulk helper.
- `internal/domain/stores/interfaces/mocks/mock_building_store.go` —
  regenerated for the new method.
- `internal/domain/stores/interfaces/collection.go` — rack-list
  filter shape. Open decision: introduce `DeviceSetFilter` struct OR
  extend `ListCollections` positional args (currently 8 args at
  collection.go:116 — add 3 more to get 11). Plan currently assumes
  the struct exists; locking the shape is part of the rack-list
  scope walk-through finding.
- `internal/domain/stores/interfaces/mocks/mock_collection_store.go`
  — regenerated.
- `internal/domain/stores/sqlstores/device_filters.go` — new
  `building_ids`, `include_no_building`, `zone_keys` predicates;
  remove `zonesFilter` / `zoneValues`.
- `sqlc/queries/device_set.sql` — `ListRackZones` rewrite; rack-list
  filter parameters.
- `sqlc/queries/collection.sql` — analogous changes if the bypass
  path isn't taken.
- `internal/domain/stores/sqlstores/collection.go` — rack-list
  filter param wiring.
- `internal/handlers/deviceset/convert.go` — proto ↔ domain
  mapping for new filter fields and reshaped `ListRackZones`
  response.
- `internal/handlers/deviceset/handler.go` — `ListRackZones`
  delegation: either stop calling `collection.v1` and call the
  store directly, or translate the response after the call (gap
  flagged separately).

Tests (~11):
- `parse_filter_test.go` (extended; all cases updated for signature
  change).
- `device_filters_test.go` (extended).
- `device_filters_integration_test.go` (new cases + new fixture).
- New: `device_filters_orgid_audit_test.go` — regex-asserts that
  every emitted `building_ids` / `include_no_building` / `zone_keys`
  predicate contains an `org_id` bind reference. Guardrail against
  future refactors that drop the org clause from the wildcard path.
- New: `list_rack_zones_integration_test.go`.

FE (~10 — grew from v1's "4"):
- `features/fleetManagement/components/MinerList/MinerList.tsx` —
  dropdown source migration + filter payload shape.
- `features/fleetManagement/utils/filterUrlParams.ts` — URL
  serialization for `zoneKeys` + `buildingIds`.
- `features/fleetManagement/utils/fleetVisiblePairingFilter.ts` —
  field rename.
- `features/rackManagement/pages/RacksPage.tsx` — composite
  dropdown source + FilterChip decision (keep + reshape OR strip).
- `features/rackManagement/components/RackSettingsModal.tsx` —
  building-scoped zone autocomplete.
- `api/useDeviceSets.ts` — `listRackZones` callback shape +
  `listRacks` filter shape.
- Saved-view legacy translation (likely co-located with the URL
  param handler).
- `MinerList/stories/mocks.ts`, `statusMocks.ts`.
- Story fixtures for `useDeviceSets` mocking the new response shape.

Plan doc updates (1):
- This file.

Estimate: **~38-42 files** including generated. Plan v1 understated
the FE surface (4 → ~10) and the server surface (9 → ~13) by
reasoning from the issue rather than the codebase. Ticket's ~22
estimate is consistent with v1's narrow scope; revised estimate
reflects the actual breakage radius of the proto changes.

## Open questions

1. **Composite SQL shape.** `UNNEST` + tuple `IN` vs. `UNNEST` +
   JOIN. Settle at code time after `EXPLAIN ANALYZE` on a realistic
   fixture.
2. **`include_no_building` interaction with `building_ids = []`.**
   When `building_ids` is empty and `include_no_building = true`,
   semantics are "only no-building devices." Same model as
   `include_unassigned`. Locked.
3. **Saved-view legacy translation strategy.** Dumb 1:1
   `zones[string] → zone_keys[{0, string}]` is the lock. No fuzzy
   auto-detect against the current building set.
4. **ListRackZones index + EXPLAIN audit.** New query has 2 LEFT
   JOINs (`building`, `site`) and a 5-column DISTINCT. Before
   merging, confirm indexes exist on
   `device_set_rack(building_id)`, `building(id, org_id)`, and
   `site(id, org_id)`; run `EXPLAIN ANALYZE` on a fixture with
   ~10K racks across ~50 buildings. If any index is missing, add
   it in this PR.

## Out of scope (tracked elsewhere)

- Cascading `site → building → zone` picker UI.
- Promoting `zone` to a first-class entity — covered in the
  multi-site plan's Storage model "Forward look" callout.
- Building filter on rack list as a separate UI control (backend
  ready; UI follows Phase 2).
