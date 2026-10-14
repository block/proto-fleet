import type { MeasurementCoverage } from "@/protoFleet/api/generated/common/v1/equipment_coverage_pb";
import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

export type TankMetric = "hashrate" | "power" | "temperature" | "efficiency";

/** Put coverage next to partial values; module health alone cannot describe telemetry coverage. */
export function tankMetricCoverage(item: InventoryItem, metric: TankMetric): string | undefined {
  if (
    item.ref?.kind !== EntityKind.TANK &&
    item.ref?.kind !== EntityKind.CONTAINER &&
    item.ref?.kind !== EntityKind.MODULE
  )
    return undefined;
  if (item.coverage) {
    const metricCoverage = metric === "efficiency" ? item.coverage.hashrate : item.coverage[metric];
    return measurementCoverage(
      metricCoverage,
      metric === "temperature" && item.ref?.kind !== EntityKind.MODULE
        ? "modules"
        : metric === "power"
          ? "meters"
          : "boards",
    );
  }
  if (item.assetCount <= 0n) return undefined;
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

export function measurementCoverage(coverage: MeasurementCoverage | undefined, unit: string): string | undefined {
  if (!coverage) return undefined;
  if (!coverage.topologyComplete) return `${coverage.reporting} ${unit} reporting · topology incomplete`;
  return coverage.reporting < coverage.expected
    ? `${coverage.reporting}/${coverage.expected} ${unit} reporting`
    : undefined;
}
