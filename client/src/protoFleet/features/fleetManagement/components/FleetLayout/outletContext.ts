import { useOutletContext } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

export interface FleetOutletContext {
  sites: SiteWithCounts[] | undefined;
  sitesError: string | null;
  refetchSites: () => void;
}

export const useFleetOutletContext = (): FleetOutletContext => useOutletContext<FleetOutletContext>();
