import { useMemo } from "react";

import {
  type SiteFilterFields,
  siteFilterFromActive,
  useActiveSite,
} from "@/protoFleet/components/PageHeader/SitePicker";

/** Header SitePicker scope for the building-side rack pickers (Manage racks /
 *  Search racks). Forwarded as `scope` so the pickers list only the scoped
 *  site's racks instead of the full org; "all sites" resolves to the empty
 *  filter (no regression). Mirrors `useRackMinerScope` on the miner side.
 *
 *  For a specific scoped site we also surface site-unassigned racks: the common
 *  way to get a rack into a site is to assign it to a building there, so it
 *  starts out site-unassigned and would otherwise be invisible in these
 *  pickers. The "all" (already everything) and "unassigned" (already
 *  includeUnassigned) cases need no adjustment.
 *
 *  Note the scope governs which racks are *fetched* (header active site); the
 *  building's own site still drives per-row eligibility in buildRackPickerItem. */
export function useBuildingRackScope(): SiteFilterFields {
  const { activeSite } = useActiveSite({});
  return useMemo(() => {
    const base = siteFilterFromActive(activeSite);
    return activeSite.kind === "site" ? { ...base, includeUnassigned: true } : base;
  }, [activeSite]);
}
