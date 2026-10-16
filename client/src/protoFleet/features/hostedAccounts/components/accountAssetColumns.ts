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
export const assetName = (asset: AccountAsset) =>
  asset.name || `Unavailable ${asset.kind === MiningAssetKind.MODULE ? "module" : "miner"} ${asset.id}`;
