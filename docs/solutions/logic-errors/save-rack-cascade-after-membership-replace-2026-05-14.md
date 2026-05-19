---
title: SaveRack cascade must run AFTER membership replace, not before
date: 2026-05-14
category: docs/solutions/logic-errors
module: server/internal/domain/collection
problem_type: logic_error
component: service_object
symptoms:
  - "Departing devices left with device.site_id pointing at the new rack site they never asked for"
  - "Cascade rewrote site_id against the pre-replace member set"
  - "Activity-log priors reflected outgoing members instead of the final member set"
  - "Site-less rack saves clobbered direct device.site_id assignments"
root_cause: logic_error
resolution_type: code_fix
severity: high
related_components:
  - database
tags: [multi-site, save-rack, cascade-order, membership-replace, activity-log]
---

# SaveRack cascade must run AFTER membership replace, not before

## Problem

When a single `SaveRack` call changed both the rack's site and its member set, the cascade ran *before* `RemoveAllDevicesFromCollection` + `AddDevicesToCollection`. The cascade was therefore aimed at the pre-replace member set — devices that were about to be removed got their `site_id` rewritten to the new site, then dropped from the rack, leaving them orphaned at a site they never asked to be assigned to.

A second flavor of the same bug: when a site-less rack (`finalSiteID == nil`) was saved without a site change, the cascade still fired and clobbered direct `device.site_id` assignments with NULL.

Activity-log "prior site_id" rows were captured against the wrong member set for the same reason.

## Symptoms

- Device removed from rack ends up with `site_id` = new rack site, no membership.
- Site-less rack save unassigns direct site stamps on its members.
- Activity-log `device_site_changes` shows priors for devices that aren't in the final member set.

## Solution

Move cascade into `replaceRackMembershipAndSlots`, *after* the remove+add steps. Capture per-device priors *after* membership replace so the audit reflects the final set. Add a `cascadeFires` guard:

```go
func (s *Service) replaceRackMembershipAndSlots(
    ctx context.Context, orgID, collectionID int64,
    deviceIdentifiers []string, slotAssignments []*pb.RackSlot,
    finalSiteID *int64, siteChanged bool,
) (rackCascadeOutcome, error) {
    var out rackCascadeOutcome
    if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, orgID, collectionID); err != nil {
        return out, err
    }

    // Cascade fires when the rack has a stamped site OR its site just
    // transitioned. Both false means the rack stayed site-less; cascading
    // NULL there would clobber direct device.site_id assignments.
    cascadeFires := finalSiteID != nil || siteChanged

    if len(deviceIdentifiers) > 0 {
        if _, err := s.collectionStore.AddDevicesToCollection(ctx, orgID, collectionID, deviceIdentifiers); err != nil {
            return out, err
        }
        if cascadeFires {
            priors, err := s.collectionStore.GetDeviceSiteIDsByMembership(ctx, collectionID, orgID)
            if err != nil { return out, err }
            out.deviceSiteChanges, out.totalAffected = buildDeviceSiteChanges(priors, finalSiteID)
            n, err := s.collectionStore.CascadeRackDeviceSites(ctx, collectionID, orgID, finalSiteID)
            if err != nil { return out, err }
            out.cascadeCount = n
        }
    }
    // ... slot replace omitted ...
}
```

Commit: `581a9897` (the F2 fix from the early PR-C review cycle). Coverage: `TestService_SaveRack_*` cases asserting post-replace cascade behavior.

## Why This Works

Cascading against the *final* member set is the only correct shape: only members of the rack post-save should inherit the rack's site. Removed devices keep whatever site they already had — they're no longer part of the rack and shouldn't be touched.

The `cascadeFires` guard prevents the second flavor: a site-less rack (no site, no site transition) has nothing to cascade. Without the guard the cascade would write NULL to every member's `site_id`, even ones that had a direct stamp the user set elsewhere.

Capturing per-device priors after replace anchors the activity log on the actual member set the user committed to, not the transient pre-replace set.

## Prevention

- Any mutation that combines a "rewrite child set" and a "cascade across that set" must order cascade *after* the rewrite. Treat the input-arg list as future state; never cascade based on a snapshot of the pre-write database.
- When a cascade can produce a NULL or unassign effect, gate it on an explicit predicate (`finalSiteID != nil || siteChanged`) rather than always firing.
- Place audit-log capture next to the cascade call so they share the same member-set snapshot.

## Related

Sister fixes from the same PR (#228, multi-site rack cascade):

- [SaveRack omitted-placement preserves current](save-rack-omitted-placement-preserves-current-2026-05-14.md)
- [DeleteCollection locks rack before cascade](delete-collection-locks-rack-before-cascade-2026-05-14.md)

GitHub issues: #197 (working branch), #226, #220. Source PR: #228.

Prior-session context (session history): No prior implementation attempts. The May 5–6 design sessions established that racks and devices can both be unassigned (nullable `site_id` columns), which is the structural condition that makes "cascade fires on site-less rack" a footgun in the first place. The Codex `sydney` session (May 8) confirmed that zone is building-scoped on racks — the corollary that cascade must respect "stayed site-less" came out of this PR's review cycle.
