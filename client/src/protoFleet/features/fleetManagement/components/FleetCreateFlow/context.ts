import { createContext, useContext } from "react";

// Seeds carried into each create flow from a bulk / single-row "New …"
// action. The selected items land in the new parent: position-seeded when
// the item type matches the modal's grid (miners→rack), or assigned as
// direct members + surfaced as a count line otherwise (option (i)).
export interface RackCreateSeed {
  minerIds: string[];
  // Number of selected miners that already have a site/building/rack
  // placement. A new rack has no site, so those placements are cleared on
  // save; when > 0 a confirm dialog warns first, mirroring the reparent flow.
  conflictCount?: number;
}

export interface BuildingCreateSeed {
  // Racks position-seeded into the building grid (item type matches the
  // modal's panel). Unplaced on entry, ready for aisle/position assignment.
  rackIds: bigint[];
  // Miners assigned directly to the building (no rack) — surfaced as the
  // count line under the racks list (option (i)).
  minerIds: string[];
  // Number of selected items that already belong to another building/site.
  // When > 0 a confirm dialog warns before entering the create flow,
  // mirroring the reparent flow. The call site computes it from data it
  // already holds (rack.buildingId, miner snapshots).
  conflictCount?: number;
  // Whether the seeded miners include any currently in a rack. Only then is
  // forceClearConflictingRackMembership set on the device assignment — the
  // server requires rack:manage when that flag is on, so leaving it false for
  // rack-less miners lets a site/building-only operator complete the flow.
  forceClearRackMembership?: boolean;
}

export interface SiteCreateSeed {
  // Buildings position-seeded into the site (item type matches the panel).
  buildingIds: bigint[];
  // Racks/miners assigned directly to the site (no building) — surfaced as
  // count lines under the buildings list (option (i)).
  rackIds: bigint[];
  minerIds: string[];
  conflictCount?: number;
  // See BuildingCreateSeed.forceClearRackMembership.
  forceClearRackMembership?: boolean;
}

export interface FleetCreateFlowContextValue {
  // Opens RackSettingsModal → ManageRackModal with the given miners
  // pre-seeded into the rack's left pane for slot assignment.
  launchCreateRack: (seed: RackCreateSeed) => void;
  // Opens BuildingSettingsModal → ManageBuildingModal: on create the seeded
  // racks/miners are assigned to the new building (force-clearing prior
  // memberships), then the manage modal opens for rack positioning.
  launchCreateBuilding: (seed: BuildingCreateSeed) => void;
  // Opens SiteSettingsModal → ManageSiteModal (edit): on create the seeded
  // buildings/racks/miners are assigned to the new site, then the manage
  // modal opens for building management. Routes through edit mode because
  // ManageSiteModal's create mode gates building assignment until the site
  // exists.
  launchCreateSite: (seed: SiteCreateSeed) => void;
  // Bumps whenever a create flow commits, so list pages can refetch
  // without the controller knowing which page is active.
  entitiesChangedAt: number;
  // Refreshes the shared site catalog and bumps entity/miner list refresh
  // signals after an external topology mutation such as site map import.
  refreshEntities: () => void;
}

export const FleetCreateFlowContext = createContext<FleetCreateFlowContextValue | undefined>(undefined);

// Returns undefined outside the provider (e.g. standalone routes that mount a
// list page without the FleetLayout shell), so callers can hide the "New …"
// affordance rather than crash.
export const useFleetCreateFlow = (): FleetCreateFlowContextValue | undefined => useContext(FleetCreateFlowContext);
