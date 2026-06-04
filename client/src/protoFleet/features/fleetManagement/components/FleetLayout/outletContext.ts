import { useOutletContext } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

export interface FleetOutletContext {
  sites: SiteWithCounts[] | undefined;
  sitesError: string | null;
  // True once `listSites` has returned at least one successful response.
  // Lets consumers distinguish "never seen data" (initial-load failure —
  // show full-page error) from "seen data and a later poll failed"
  // (transient — keep last-good content + inline retry banner). A
  // legitimate zero-site org reaches `sites.length === 0` with
  // `sitesLoaded === true`, so its empty-state CTA can still render.
  sitesLoaded: boolean;
  refetchSites: () => void;
}

export const useFleetOutletContext = (): FleetOutletContext => useOutletContext<FleetOutletContext>();
