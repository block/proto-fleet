import { create } from "@bufbuild/protobuf";

import { type AssetColumn, assetKindLabel, assetName } from "./accountAssetColumns";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { type AccountAsset, MiningAssetKind } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { InventoryCell } from "@/protoFleet/features/containers/InventoryCell";
import { MinerMetricValue } from "@/protoFleet/features/fleetManagement/components/MinerList/MinerMeasurement";
import { useTemperatureUnit } from "@/protoFleet/store";
import StatusCircle from "@/shared/components/StatusCircle";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

export function AccountAssetCell({ asset, column }: { asset: AccountAsset; column: AssetColumn }) {
  const unit = useTemperatureUnit();
  if (column === "model") return <span>{asset.model || "—"}</span>;
  if (column === "kind") return <span>{assetKindLabel(asset.kind)}</span>;
  if (column === "container")
    return (
      <span className="block truncate" title={asset.containerName || undefined}>
        {asset.containerName || "—"}
      </span>
    );
  if (column === "status" && (asset.outsidePolicy || !asset.available))
    return (
      <span className="flex items-center gap-2">
        <StatusCircle status="inactive" variant="simple" width="w-[6px]" />
        {asset.outsidePolicy ? "Outside site policy" : "Unavailable"}
      </span>
    );
  if (asset.kind !== MiningAssetKind.MODULE) {
    switch (column) {
      case "name":
        return (
          <span className="block truncate text-left" title={assetName(asset)}>
            {assetName(asset)}
          </span>
        );
      case "hashrate":
        return (
          <MinerMetricValue value={asset.hashrateHs === undefined ? undefined : asset.hashrateHs / 1e12} unit="TH/s" />
        );
      case "efficiency":
        return <MinerMetricValue value={asset.efficiencyJTh} unit="J/TH" />;
      case "powerUsage":
        return <MinerMetricValue value={asset.powerW === undefined ? undefined : asset.powerW / 1000} unit="kW" />;
      case "temperature":
        return (
          <MinerMetricValue
            value={
              asset.temperatureC === undefined
                ? undefined
                : unit === "F"
                  ? convertCtoF(asset.temperatureC)
                  : asset.temperatureC
            }
            unit={`°${unit}`}
          />
        );
    }
  }
  const item = create(InventoryItemSchema, {
    ref: { kind: asset.kind === MiningAssetKind.MODULE ? EntityKind.MODULE : EntityKind.MINER, id: asset.id },
    name: assetName(asset),
    state: asset.state,
    siteName: asset.siteName,
    assetCount: 1n,
    hashrateReportingCount: asset.hashrateHs === undefined ? 0n : 1n,
    powerReportingCount: asset.powerW === undefined ? 0n : 1n,
    hashrateThs: asset.hashrateHs === undefined ? undefined : asset.hashrateHs / 1e12,
    powerKw: asset.powerW === undefined ? undefined : asset.powerW / 1000,
    efficiencyJth: asset.efficiencyJTh,
    minTemperatureC: asset.temperatureC,
    maxTemperatureC: asset.temperatureC,
    coverage: asset.coverage,
  });
  const readings: Partial<Record<AssetColumn, number | undefined>> = {
    hashrate: asset.hashrateHs,
    powerUsage: asset.powerW,
    efficiency: asset.efficiencyJTh,
    temperature: asset.temperatureC,
  };
  const reading = readings[column];
  return reading !== undefined && asset.unknownCoverage && !asset.coverage ? (
    <div>
      <InventoryCell item={item} column={column} />
      <span className="text-100 block text-text-primary-70">Module coverage unknown</span>
    </div>
  ) : (
    <InventoryCell item={item} column={column} />
  );
}
