import type { ResourceRef } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export const getMinerSiteLabel = (miner: MinerStateSnapshot): string => miner.placement?.site?.label ?? "";

export const getMinerBuildingLabel = (miner: MinerStateSnapshot): string => miner.placement?.building?.label ?? "";

export const getMinerRackLabel = (miner: MinerStateSnapshot): string => miner.placement?.rack?.label ?? "";

export const getMinerCohortLabel = (miner: MinerStateSnapshot): string => miner.placement?.cohort?.label ?? "";

/** Normalize a placement ResourceRef id to bigint, treating the proto default
 *  (0 / absent ref) as "unassigned" (undefined) so id-based eligibility checks
 *  don't confuse an unplaced miner with one placed at id 0. */
const refId = (ref?: ResourceRef): bigint | undefined => (ref && ref.id !== 0n ? ref.id : undefined);

export const getMinerSiteId = (miner: MinerStateSnapshot): bigint | undefined => refId(miner.placement?.site);

export const getMinerBuildingId = (miner: MinerStateSnapshot): bigint | undefined => refId(miner.placement?.building);

export const getMinerRackId = (miner: MinerStateSnapshot): bigint | undefined => refId(miner.placement?.rack);

/** Placement of the rack a caller is assigning miners into. Miners in a
 *  different rack, building, or site are "ineligible". Fields are undefined
 *  when the target rack isn't placed at that level yet (e.g. a new rack). */
export type MinerEligibility = {
  rackId?: bigint;
  siteId?: bigint;
  buildingId?: bigint;
};

/** A placement is ineligible when it sits somewhere the target rack isn't, at
 *  any level — assigning it there moves it. SaveRack aligns members to the
 *  rack's placement, and an unplaced/partly-placed rack strips the mismatched
 *  levels to NULL, so a miner placed at a level the target *lacks* (target id
 *  undefined) is also a reassignment. This makes a new/unplaced rack warn before
 *  clearing a miner's existing rack/building/site. A miner unplaced at a level
 *  (its own id undefined) is never moved there, so it stays eligible.
 *  Id-based to avoid label collisions (a same-named rack in another building). */
export const isPlacementIneligible = (placement: MinerEligibility, eligibility: MinerEligibility): boolean =>
  (placement.rackId !== undefined && placement.rackId !== eligibility.rackId) ||
  (placement.buildingId !== undefined && placement.buildingId !== eligibility.buildingId) ||
  (placement.siteId !== undefined && placement.siteId !== eligibility.siteId);

export const isMinerSnapshotIneligible = (miner: MinerStateSnapshot, eligibility: MinerEligibility): boolean =>
  isPlacementIneligible(
    { rackId: getMinerRackId(miner), buildingId: getMinerBuildingId(miner), siteId: getMinerSiteId(miner) },
    eligibility,
  );

export const getMinerGroupRefs = (miner: MinerStateSnapshot): ResourceRef[] => miner.placement?.groups ?? [];

export const getMinerGroupLabels = (miner: MinerStateSnapshot): string[] =>
  getMinerGroupRefs(miner).map((group) => group.label);
