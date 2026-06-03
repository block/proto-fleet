import { useOutletContext } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

// Shape exposed by FleetLayout to its tab children via Outlet context.
// `sites === undefined` signals "load in flight"; `sitesError` is populated
// when the latest fetch failed. Calling `refetchSites` after a mutation
// refreshes both the consuming tab and the layout's Sites-tab-visibility
// check in one round-trip.
//
// Lives in its own module (not next to the component default export) so
// fast-refresh keeps working on FleetLayout.tsx.
export interface FleetOutletContext {
  sites: SiteWithCounts[] | undefined;
  sitesError: string | null;
  refetchSites: () => void;
}

export const useFleetOutletContext = (): FleetOutletContext => useOutletContext<FleetOutletContext>();
