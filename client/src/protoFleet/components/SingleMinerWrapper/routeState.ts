import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export type SingleMinerMetadata = {
  minerName?: string;
  ipAddress?: string;
  macAddress?: string;
  firmwareVersion?: string;
};

export type SingleMinerRouteState = {
  singleMinerMetadata?: SingleMinerMetadata;
};

const nonEmpty = (value: string | undefined): string | undefined => {
  const normalized = value?.trim();
  return normalized ? normalized : undefined;
};

export const buildSingleMinerMetadata = (miner: MinerStateSnapshot): SingleMinerMetadata => ({
  minerName: nonEmpty(miner.model) ?? nonEmpty(miner.name) ?? nonEmpty(miner.deviceIdentifier),
  ipAddress: nonEmpty(miner.ipAddress),
  macAddress: nonEmpty(miner.macAddress),
  firmwareVersion: nonEmpty(miner.firmwareVersion),
});

export const buildSingleMinerRouteState = (miner: MinerStateSnapshot): SingleMinerRouteState => ({
  singleMinerMetadata: buildSingleMinerMetadata(miner),
});

export const canOpenEmbeddedMinerView = (miner: MinerStateSnapshot): boolean => miner.embeddedWebViewAvailable;
