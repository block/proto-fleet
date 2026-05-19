---
title: SaveRack with omitted placement nulled site/building/zone on legacy clients
date: 2026-05-14
category: docs/solutions/logic-errors
module: server/internal/domain/collection
problem_type: logic_error
component: service_object
symptoms:
  - "Rack-edit modal save would null-out site_id, building_id, and zone on a previously placed rack"
  - "Legacy clients that don't yet send placement fields silently unassigned the rack"
  - "After the initial preserve fix, zone could still null even though site/building were kept (rack still in a building)"
  - "No validation error: the request was accepted and the placement was destroyed"
root_cause: logic_error
resolution_type: code_fix
severity: high
related_components:
  - documentation
tags: [multi-site, save-rack, proto3-optional, placement, zone, legacy-client]
---

# SaveRack with omitted placement nulled site/building/zone on legacy clients

## Problem

`SaveRack` update treated "field omitted by caller" identically to "explicit zero (unassign)". In proto3 the wrapped `optional int64` distinguishes the two, but `saveRackUpdate` flowed both through the same path and overwrote `device_set.site_id`, `building_id`, and `zone` with NULL. The rack-edit modal in production today doesn't send placement fields, so every save destroyed placement.

A follow-up sub-issue: after the initial preserve fix kept site/building, the zone-clearing switch still nulled `zone` when the request carried an empty zone string but the rack was still in a building. Zone validation only required a non-empty zone when the request itself set a non-zero `building_id` — it was anchored on request shape, not persisted state.

## Symptoms

- Rack-edit save returns 200; on reload, site/building/zone are unset.
- Legacy modal (no placement awareness) silently unassigns racks.
- After the site/building preserve patch landed, zone-only regression remained on building-bound racks.
- No validation error surfaces — the request looks well-formed.

## Solution

Introduce a helper that distinguishes omitted from explicit-zero, then branch in the update path:

```go
// rackPlacementOmitted reports whether the caller omitted placement intent
// (both ids nil). Explicit zero (unassign) returns false.
func rackPlacementOmitted(rackInfo *pb.RackInfo) bool {
    return rackInfo != nil && rackInfo.SiteId == nil && rackInfo.BuildingId == nil
}
```

In `saveRackUpdate`, preserve current placement when omitted; otherwise resolve+lock site/building as before:

```go
if rackPlacementOmitted(rackInfo) {
    // Preserve current; rack lock alone serializes the no-op cascade.
    current, err = s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
    if err != nil { return nil, err }
    newSiteID = current.SiteID
    newBuildingID = current.BuildingID
} else {
    newSiteID, newBuildingID, err = s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
    if err != nil { return nil, err }
    current, err = s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
    if err != nil { return nil, err }
}
```

Zone fallback in the zone-clearing switch — only clear when leaving or crossing buildings; preserve current zone when the request omitted zone but the rack still has a building:

```go
finalZone := rackInfo.GetZone()
leavingBuilding := current.BuildingID != nil && newBuildingID == nil
crossingBuildings := current.BuildingID != nil && newBuildingID != nil && !int64PtrEqual(current.BuildingID, newBuildingID)
switch {
case leavingBuilding || crossingBuildings:
    finalZone = ""
case finalZone == "" && newBuildingID != nil:
    finalZone = current.Zone
}
```

Sentinel documented in `proto/collection/v1/collection.proto` and `proto/device_set/v1/device_set.proto`:

> SaveRack update: leaving both site_id and building_id unset preserves the current placement. Send an explicit 0 to unassign.

Tests: `TestService_SaveRack_OmittedPlacementPreservesCurrent`, `TestService_SaveRack_OmittedPlacementPreservesZone` (file `server/internal/domain/collection/service_test.go`).

Commits: `6835d392` (preserve branch via F15), `015e261c` (proto sentinel docs), `0b6bbbba` (zone preservation follow-up).

## Why This Works

proto3 optional carries a presence bit. The helper inspects that bit, so the server can distinguish "field absent" from "field set to zero" and pick the right action: preserve vs. unassign. Skipping the site/building locks on the preserve branch is safe because the rack-row lock alone serializes the no-op against concurrent placement mutators (which all take the same lock).

The zone fallback closes the parallel gap: zone is building-scoped, so the only times it must clear are leaving a building or crossing buildings. Anchoring the empty-zone case on `newBuildingID != nil` (post-resolution, not on the raw request) lets a building-bound rack survive a save that doesn't mention zone.

## Prevention

- For any proto3 `optional` scalar with placement-like semantics, document the omitted-vs-explicit-zero distinction in the proto comment itself (the sentinel doc is the contract).
- In service-layer update paths, anchor validation and branching on the *resolved post-merge state*, not on the raw request shape — request shape is incomplete by design.
- Pair every "preserve" branch with a test that constructs an empty request and asserts the persisted row is unchanged.

## Related

Sister fixes from the same PR (#228, multi-site rack cascade):

- [DeleteCollection locks rack before cascade](delete-collection-locks-rack-before-cascade-2026-05-14.md)
- [SaveRack cascade after membership replace](save-rack-cascade-after-membership-replace-2026-05-14.md)

GitHub issues: #197 (working branch), #226 (rack zone semantics when `building_id` is null — natural follow-up for this fix), #220 (cross-collection site invariant). Source PR: #228.

Prior-session context (session history): No prior implementation attempts exist for SaveRack omitted-placement handling. The May 5–6 planning sessions established that `building.site_id` and `device_set_rack.building_id` are both nullable — meaning "rack not yet placed" is a first-class state — which is the condition that created the omitted-vs-unassign ambiguity this fix resolves.
