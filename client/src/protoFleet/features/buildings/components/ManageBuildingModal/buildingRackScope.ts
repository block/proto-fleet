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
