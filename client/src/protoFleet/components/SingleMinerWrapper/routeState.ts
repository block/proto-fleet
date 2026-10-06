import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  getMinerBuildingLabel,
  getMinerRackLabel,
  getMinerSiteLabel,
} from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import type { MinerMetadata } from "@/shared/types/minerMetadata";

export type SingleMinerMetadata = MinerMetadata;

export type SingleMinerRouteState = {
  singleMinerMetadata?: SingleMinerMetadata;
};

const nonEmpty = (value: string | undefined): string | undefined => {
  const normalized = value?.trim();
  return normalized ? normalized : undefined;
};

export const buildSingleMinerMetadata = (miner: MinerStateSnapshot): SingleMinerMetadata => ({
  // Match the miner-list name column (MinerName): the device name, falling back
  // to its identifier — not the model.
  minerName: nonEmpty(miner.name) ?? nonEmpty(miner.deviceIdentifier),
  ipAddress: nonEmpty(miner.ipAddress),
  macAddress: nonEmpty(miner.macAddress),
  firmwareVersion: nonEmpty(miner.firmwareVersion),
  site: nonEmpty(getMinerSiteLabel(miner)),
  building: nonEmpty(getMinerBuildingLabel(miner)),
  rack: nonEmpty(getMinerRackLabel(miner)),
});

export const buildSingleMinerRouteState = (miner: MinerStateSnapshot): SingleMinerRouteState => ({
  singleMinerMetadata: buildSingleMinerMetadata(miner),
});

// The protoOS index routes redirect via loaders (loader: () => redirect(...)),
// which run before render and drop navigation state — so metadata can't ride on
// location.state into SingleMinerWrapper. The opener stamps it here keyed by
// device id (it already holds the list snapshot); the wrapper reads it back.
const metadataByDevice = new Map<string, SingleMinerMetadata>();

export const rememberSingleMinerMetadata = (miner: MinerStateSnapshot): void => {
  metadataByDevice.set(miner.deviceIdentifier, buildSingleMinerMetadata(miner));
};

export const recallSingleMinerMetadata = (deviceIdentifier: string): SingleMinerMetadata | undefined =>
  metadataByDevice.get(deviceIdentifier);

export const canOpenEmbeddedMinerView = (miner: MinerStateSnapshot): boolean => miner.embeddedWebViewAvailable;
