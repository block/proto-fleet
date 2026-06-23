import { useOutletContext } from "react-router-dom";

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type FilterLabelSource } from "@/protoFleet/features/fleetManagement/views/viewSummary";

export interface FleetOutletContext {
  sites: SiteWithCounts[] | undefined;
  sitesError: string | null;
  // True once listSites has returned at least one successful response;
  // distinguishes "never seen data" (show full-page error) from
  // "seen data and a later poll failed" (preserve last-good content).
  sitesLoaded: boolean;
  // True only after FleetLayout has proven org-scoped site catalog access via
  // a successful ListSites response. A flat site:read permission can be true
  // for site-scoped-only users while org-scoped catalog RPCs are still denied.
  siteCatalogAccessGranted: boolean;
  refetchSites: () => void;
  // Pairing coordination between the Miners tab and the chrome-level
  // CompleteSetup banner. Miners → CompleteSetup: call `notifyPairingCompleted`
  // when pairing finishes so the banner's pool/auth probes refresh. Bumps
  // `minersChangedAt`: CompleteSetup → Miners signal for when an in-banner
  // flow (e.g. pool assignment) wants the miner list to refetch immediately
  // instead of waiting for the next poll tick.
  notifyPairingCompleted: () => void;
  minersChangedAt: number;
  /**
   * Child tabs publish their filter metadata up to FleetLayout so the
   * saved-view modal can render human-readable labels for filter ids. Tabs
   * without filters skip the call. Each child publishes only the keys it
   * knows about — defaults fill the rest.
   */
  publishViewFilterContext: (ctx: {
    availableGroups?: DeviceSet[];
    availableRacks?: DeviceSet[];
    availableBuildings?: FilterLabelSource[];
    availableSites?: FilterLabelSource[];
  }) => void;
}

export const useFleetOutletContext = (): FleetOutletContext => useOutletContext<FleetOutletContext>();

/**
 * Like `useFleetOutletContext` but tolerates being rendered outside the
 * FleetLayout shell — returns undefined for routes (e.g. standalone `/racks`)
 * that mount the same page without a parent Outlet.
 */
export const useOptionalFleetOutletContext = (): FleetOutletContext | undefined =>
  useOutletContext<FleetOutletContext | undefined>();
