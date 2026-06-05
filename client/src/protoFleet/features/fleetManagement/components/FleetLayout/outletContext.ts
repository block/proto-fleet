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
  // Pairing coordination between the Miners tab and the chrome-level
  // CompleteSetup banner. Miners → CompleteSetup: call `notifyPairingCompleted`
  // when pairing finishes so the banner's pool/auth probes refresh. Bumps
  // `minersChangedAt`: CompleteSetup → Miners signal for when an in-banner
  // flow (e.g. pool assignment) wants the miner list to refetch immediately
  // instead of waiting for the next poll tick.
  notifyPairingCompleted: () => void;
  minersChangedAt: number;
}

export const useFleetOutletContext = (): FleetOutletContext => useOutletContext<FleetOutletContext>();
