import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";

/** Rack list-filter scope for the building-side rack pickers (Manage racks /
 *  Search racks). Derived from the building's *own* site rather than the header
 *  SitePicker so the fetch stays correct on the unscoped `/buildings/:id` and
 *  `/sites/:id` routes, where the header selection is the last-persisted value
 *  and may be an unrelated site (a bookmarked North building opened while
 *  "South" is selected would otherwise fetch South's racks and hide North's).
 *
 *  The scope mirrors the per-row eligibility in `buildRackPickerItem`: a rack is
 *  eligible for this building when it shares the building's site or is
 *  site-unassigned (racks in another site are ineligible). So we fetch exactly
 *  that set — the building's site plus site-unassigned racks:
 *    - real site (id != 0) → siteIds=[id], includeUnassigned=true
 *    - site-unassigned building (id == 0) → siteIds=[], includeUnassigned=true
 *      (only site-unassigned racks are eligible; that IS the "unassigned" scope)
 *
 *  Surfacing site-unassigned racks matters because assigning a currently-
 *  unplaced rack to a building is the common way a rack first enters a site. */
export function buildingRackScope(buildingSiteId: bigint): SiteFilterFields {
  return {
    siteIds: buildingSiteId !== 0n ? [buildingSiteId] : [],
    includeUnassigned: true,
  };
}

/** Broadened fetch scope used when the "Show assigned racks" toggle is ON, so
 *  ineligible (already-placed) racks surface for reparenting. Mirrors the
 *  miner-side global-vs-scoped model:
 *    - header all-sites → global (unscoped) fetch → other-site racks surface
 *      too (cross-site reparent).
 *    - header scoped → the building-site scope, unchanged. That scope already
 *      fetches the whole site (every building + site-unassigned), so the
 *      same-site, other-building racks are already present — turning the toggle
 *      on simply stops hiding them. No broader fetch is needed and there is no
 *      cross-site reparent from a scoped header.
 *
 *  Relies on scope-sync (#764) keeping the header in agreement with the building
 *  on the headerless detail routes, so no union / mismatch handling is needed:
 *  a scoped header is guaranteed to equal the building's own site. */
export function assignedRackScope(buildingSiteId: bigint, allSites: boolean): SiteFilterFields {
  if (allSites) return { siteIds: [], includeUnassigned: false };
  return buildingRackScope(buildingSiteId);
}
