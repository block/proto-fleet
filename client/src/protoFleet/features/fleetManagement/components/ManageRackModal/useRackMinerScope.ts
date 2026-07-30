import { useMemo } from "react";

import {
  type SiteFilterFields,
  siteFilterFromActive,
  useActiveSite,
} from "@/protoFleet/components/PageHeader/SitePicker";

/** Header SitePicker scope for the rack miner-selection pickers (Manage Miners /
 *  Search Miners). Forwarded as `scope` so the pickers list only the scoped
 *  site's miners instead of the full org; "all sites" resolves to the empty
 *  filter (no regression).
 *
 *  For a specific scoped site we also surface site-unassigned miners: the common
 *  way to get a miner into a site is to assign it to a rack there, so it starts
 *  out unassigned and would otherwise be invisible in these pickers. The "all"
 *  (already everything) and "unassigned" (already includeUnassigned) cases need
 *  no adjustment. Shared between ManageRackModal and the rack-detail slot-search
 *  flow so the includeUnassigned decision lives in one place. */
export function useRackMinerScope(): SiteFilterFields {
  const { activeSite } = useActiveSite({});
  return useMemo(() => {
    const base = siteFilterFromActive(activeSite);
    return activeSite.kind === "site" ? { ...base, includeUnassigned: true } : base;
  }, [activeSite]);
}
