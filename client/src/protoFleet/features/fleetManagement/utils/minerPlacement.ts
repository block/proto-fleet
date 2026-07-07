import type { ResourceRef } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export const getMinerSiteLabel = (miner: MinerStateSnapshot): string => miner.placement?.site?.label ?? "";

export const getMinerBuildingLabel = (miner: MinerStateSnapshot): string => miner.placement?.building?.label ?? "";

export const getMinerRackLabel = (miner: MinerStateSnapshot): string => miner.placement?.rack?.label ?? "";

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

/** A placement is ineligible when it sits somewhere incompatible with the
 *  target rack. Any miner in a *different* rack is ineligible — including every
 *  racked miner when the target rack doesn't exist yet (rackId undefined), so a
 *  new rack still only pulls from unracked miners. Building/site only gate when
 *  the target rack is placed at that level. Unplaced miners stay eligible.
 *  Id-based to avoid label collisions (a same-named rack in another building). */
export const isPlacementIneligible = (placement: MinerEligibility, eligibility: MinerEligibility): boolean =>
  (placement.rackId !== undefined && placement.rackId !== eligibility.rackId) ||
  (eligibility.buildingId !== undefined &&
    placement.buildingId !== undefined &&
    placement.buildingId !== eligibility.buildingId) ||
  (eligibility.siteId !== undefined && placement.siteId !== undefined && placement.siteId !== eligibility.siteId);

export const isMinerSnapshotIneligible = (miner: MinerStateSnapshot, eligibility: MinerEligibility): boolean =>
  isPlacementIneligible(
    { rackId: getMinerRackId(miner), buildingId: getMinerBuildingId(miner), siteId: getMinerSiteId(miner) },
    eligibility,
  );

export const getMinerGroupRefs = (miner: MinerStateSnapshot): ResourceRef[] => miner.placement?.groups ?? [];

export const getMinerGroupLabels = (miner: MinerStateSnapshot): string[] =>
  getMinerGroupRefs(miner).map((group) => group.label);
