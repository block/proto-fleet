---
title: DeleteCollection must lock the rack before cascade to serialize against concurrent writers
date: 2026-05-14
category: docs/solutions/logic-errors
module: server/internal/domain/collection
problem_type: logic_error
component: service_object
symptoms:
  - "Rack soft-deleted while concurrent AddDevicesToCollection added new orphan members"
  - "device.site_id left dangling, pointing at the deleted rack's old site"
  - "Race window between UnassignDeviceSitesByRack and SoftDeleteCollection under read-committed Postgres"
  - "DeleteCollection was the only rack mutator NOT taking LockRackPlacementForWrite"
root_cause: async_timing
resolution_type: code_fix
severity: high
related_components:
  - database
tags: [multi-site, delete-collection, rack-lock, race-condition, postgres, cascade]
---

# DeleteCollection must lock the rack before cascade to serialize against concurrent writers

## Problem

Under read-committed Postgres, `DeleteCollection`'s cascade path could interleave with a concurrent `AddDevicesToCollection`:

1. `DeleteCollection` calls `UnassignDeviceSitesByRack` to clear `device.site_id` for all current members.
2. Between that call and `SoftDeleteCollection`, a concurrent `AddDevicesToCollection` grabs the rack lock, adds new devices, and cascades their `site_id` to the (about-to-be-deleted) rack's site.
3. `SoftDeleteCollection` runs. Now the rack is gone, but the new members are orphaned with `device.site_id` pointing at a site they never explicitly chose.

Every other rack mutator (`AddDevicesToCollection`, `SaveRack`, `UpdateCollection`) already called `LockRackPlacementForWrite`. `DeleteCollection` was the odd one out.

## Symptoms

- Post-delete query: devices exist with `site_id = <deleted rack's old site>` and no rack membership.
- Reproducible only under concurrent load — the bug is timing-dependent.
- `idx_one_rack_per_device` was respected at every step (the bug is about cascade ordering, not constraints).

## Solution

Inside the `COLLECTION_TYPE_RACK` branch of `DeleteCollection`'s transaction, acquire `LockRackPlacementForWrite` *before* `UnassignDeviceSitesByRack`:

```go
if collType == pb.CollectionType_COLLECTION_TYPE_RACK {
    // Lock the rack FOR UPDATE so concurrent AddDevicesToCollection
    // / SaveRack can't slip a new member or cascade in between our
    // unassign + membership-drop + soft-delete steps.
    if _, err := s.collectionStore.LockRackPlacementForWrite(ctx, req.CollectionId, info.OrganizationID); err != nil {
        return err
    }
    n, err := s.collectionStore.UnassignDeviceSitesByRack(ctx, req.CollectionId, info.OrganizationID)
    if err != nil { return err }
    siteUnassignedCount = n
}
```

Test: `TestService_DeleteCollection_LocksRackBeforeCascade` uses `gomock.InOrder` to assert lock → unassign → soft-delete ordering. Commit: `233b49ea`.

## Why This Works

`LockRackPlacementForWrite` is a row-level `SELECT … FOR UPDATE` on the rack's `device_set_rack` row. Every other mutator takes the same lock, so the lock is the global serialization point for placement changes against a rack. By taking it before the cascade, `DeleteCollection` becomes mutually exclusive with `AddDevicesToCollection`, `SaveRack`, and `UpdateCollection` on the same row — the concurrent-add can't run until the delete commits (at which point the rack lookup fails and the add returns NotFound, which is correct).

## Prevention

- When introducing a new operation that mutates rack state, audit the existing mutator set and call `LockRackPlacementForWrite` in the same canonical position (after type-check, before cascade/data mutations).
- For symmetric operations (Create/Update/Delete), grep for the lock call in sibling methods before merging — uniformity of locking is the invariant.
- Add a `gomock.InOrder` test for any new mutator to lock the ordering contract.
- The broader pattern: every cross-collection consistency rule needs to identify its single canonical lock (here: the rack extension row) and force every writer through it.

## Related

Sister fixes from the same PR (#228, multi-site rack cascade):

- [SaveRack omitted-placement preserves current](save-rack-omitted-placement-preserves-current-2026-05-14.md)
- [SaveRack cascade after membership replace](save-rack-cascade-after-membership-replace-2026-05-14.md)

GitHub issues: #197 (working branch), #220 (cross-collection site invariant — original motivator), #229. Source PR: #228.

Prior-session context (session history): The `issue-196` lock-order fix for `ReassignDevicesToSite` (commit `598f703b`) established the `site → device` lock ordering convention earlier in May. That session noted explicitly that "tests don't enforce lock-order pairs via `InOrder`, so the swap is invisible to them" — which is why this fix's `TestService_DeleteCollection_LocksRackBeforeCascade` test uses `gomock.InOrder` to capture the contract.
