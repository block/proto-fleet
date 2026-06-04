import { useOutletContext } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

export interface FleetOutletContext {
  sites: SiteWithCounts[] | undefined;
  sitesError: string | null;
  // True once listSites has returned at least one successful response;
  // distinguishes "never seen data" (show full-page error) from
  // "seen data and a later poll failed" (preserve last-good content).
  sitesLoaded: boolean;
  refetchSites: () => void;
}

export const useFleetOutletContext = (): FleetOutletContext => useOutletContext<FleetOutletContext>();
