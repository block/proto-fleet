import { buildContainerOverview } from "./buildContainerOverview";
import type { ContainerOverviewData } from "./hooks/useContainerOverview";
import { tankMetricCoverage } from "./tankMetricCoverage";
import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import {
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
  formatTempRange,
} from "@/shared/utils/telemetryFormat";

export const tankPath = (identifier: string, tankId: string) =>
  `/containers/${encodeURIComponent(identifier)}/tanks/${encodeURIComponent(tankId)}`;

export function buildTankOverview(data: ContainerOverviewData, tankId: string, unit: "C" | "F", failed = false) {
  const belongsToContainer = (item: InventoryItem) =>
    item.ancestors.some((parent) => parent.kind === EntityKind.CONTAINER && parent.id === data.container.id);
  const tank = data.tanks.find(
    (item) => item.ref?.kind === EntityKind.TANK && item.ref.id.toString() === tankId && belongsToContainer(item),
  );
  if (!tank) return undefined;
  const modules = data.modules.filter(
    (item) =>
      item.ref?.kind === EntityKind.MODULE &&
      item.state !== "retired" &&
      belongsToContainer(item) &&
      item.ancestors.some((parent) => parent.kind === EntityKind.TANK && parent.id === tank.ref!.id),
  );
  const view = buildContainerOverview({ ...data, tanks: [tank], modules }, unit, failed);
  const complete = !view.stale && tank.assetCount > 0n && BigInt(modules.length) === tank.assetCount;
  const hashrate = !view.stale ? tank.hashrateThs : undefined;
  const power = !view.stale ? tank.powerKw : undefined;
  const efficiency = !view.stale ? tank.efficiencyJth : undefined;
  const temperature =
    !view.stale && tank.minTemperatureC !== undefined && tank.maxTemperatureC !== undefined
      ? formatTempRange(tank.minTemperatureC, tank.maxTemperatureC, unit)
      : "—";
  const incompleteCoverage = tank.coverage
    ? [tank.coverage.hashrate, tank.coverage.power, tank.coverage.temperature].some(
        (coverage) =>
          !coverage || !coverage.topologyComplete || coverage.expected <= 0 || coverage.reporting !== coverage.expected,
      )
    : tank.hashrateReportingCount < tank.assetCount || tank.powerReportingCount < tank.assetCount;
  return {
    tank,
    modules,
    card: view.tanks[0],
    title: `${data.container.name} Tank ${tank.position}`,
    stale: view.stale,
    incomplete:
      !complete ||
      incompleteCoverage ||
      hashrate === undefined ||
      power === undefined ||
      temperature === "—" ||
      view.tanks[0].modules.includes("unknown"),
    kpis: [
      {
        label: "Hashrate",
        text: hashrate !== undefined ? tankMetricCoverage(tank, "hashrate") : undefined,
        value: formatHashrateOrDash(hashrate ?? null),
        preformatted: true,
      },
      {
        label: "Tank power",
        text: power !== undefined ? tankMetricCoverage(tank, "power") : undefined,
        value: formatPowerKwOrDash(power ?? null),
        preformatted: true,
      },
      {
        label: "Efficiency",
        text: efficiency !== undefined ? tankMetricCoverage(tank, "efficiency") : undefined,
        value: formatEfficiencyOrDash(efficiency ?? null),
        preformatted: true,
      },
      {
        label: "Temperature",
        text: temperature !== "—" ? tankMetricCoverage(tank, "temperature") : undefined,
        value: temperature,
        preformatted: true,
        valueClassName: "block whitespace-normal",
      },
    ],
  };
}
