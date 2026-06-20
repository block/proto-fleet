import type { ResourceRef } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export const getMinerSiteLabel = (miner: MinerStateSnapshot): string => miner.placement?.site?.label ?? "";

export const getMinerBuildingLabel = (miner: MinerStateSnapshot): string => miner.placement?.building?.label ?? "";

export const getMinerRackLabel = (miner: MinerStateSnapshot): string => miner.placement?.rack?.label ?? "";

export const getMinerGroupRefs = (miner: MinerStateSnapshot): ResourceRef[] => miner.placement?.groups ?? [];

export const getMinerGroupLabels = (miner: MinerStateSnapshot): string[] =>
  getMinerGroupRefs(miner).map((group) => group.label);
