import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

export type TankMetric = "hashrate" | "power" | "temperature" | "efficiency";

/** Put coverage next to partial values; module health alone cannot describe telemetry coverage. */
export function tankMetricCoverage(item: InventoryItem, metric: TankMetric): string | undefined {
  if (item.ref?.kind !== EntityKind.TANK || item.assetCount <= 0n) return undefined;
  const counts = {
    hashrate: item.hashrateReportingCount,
    power: item.powerReportingCount,
    temperature: item.listStats?.temperatureReportingCount,
    efficiency: item.listStats?.efficiencyReportingCount,
  };
  const count = counts[metric];
  if (count === undefined) return undefined;
  const reporting = BigInt(count);
  return reporting < item.assetCount ? `${reporting}/${item.assetCount} modules reporting` : undefined;
}
