import type { AccountAsset } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { MiningAssetKind } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import {
  minerColTitles,
  miningColumnWidths,
} from "@/protoFleet/features/fleetManagement/components/MinerList/constants";

export const assetColumnTitles = {
  name: minerColTitles.name,
  assignedAccount: minerColTitles.assignedAccount,
  model: minerColTitles.model,
  kind: "Type",
  status: minerColTitles.status,
  poolConfiguration: minerColTitles.poolConfiguration,
  hashrate: minerColTitles.hashrate,
  efficiency: minerColTitles.efficiency,
  powerUsage: minerColTitles.powerUsage,
  temperature: minerColTitles.temperature,
  site: minerColTitles.site,
  container: "Container",
};
export type AssetColumn = keyof typeof assetColumnTitles;
export const assetColumnWidths = {
  ...miningColumnWidths,
  assignedAccount: "w-[160px]",
  kind: "w-[96px]",
  container: "w-[192px]",
};
export const assetKindLabel = (kind: MiningAssetKind) => {
  if (kind === MiningAssetKind.MODULE) return "Module";
  if (kind === MiningAssetKind.HASHBOARD) return "Hashboard";
  return "Miner";
};
export const assetName = (asset: AccountAsset) =>
  asset.name || `Unavailable ${assetKindLabel(asset.kind).toLowerCase()} ${asset.id}`;
