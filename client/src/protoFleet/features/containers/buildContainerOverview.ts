import type { ContainerTank } from "./components/ContainerOverviewLayout";
import type { TankModuleState } from "./components/TankModuleGrid";
import type { ContainerOverviewData } from "./hooks/useContainerOverview";
import { tankMetricCoverage } from "./tankMetricCoverage";
import { EntityKind, type EntityRef } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import {
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
  formatTempRange,
} from "@/shared/utils/telemetryFormat";

const states: Record<string, TankModuleState> = {
  active: "healthy",
  error: "attention",
  offline: "offline",
  inactive: "sleeping",
  maintenance: "sleeping",
  updating: "attention",
  needs_mining_pool: "attention",
  reboot_required: "attention",
};

const hasRef = <T extends { ref?: EntityRef }>(item: T): item is T & { ref: EntityRef } => item.ref !== undefined;

export function buildContainerOverview(data: ContainerOverviewData, unit: "C" | "F", failed = false) {
  const { container } = data;
  const tanks = data.tanks.filter(hasRef);
  const modules = data.modules.filter(hasRef);
  const stale = container.stale || failed;
  const hashrate = stale || container.hashrateHs === undefined ? null : container.hashrateHs / 1e12;
  const power = stale || container.powerW === undefined ? null : container.powerW / 1000;
  const allStatesKnown =
    modules.length > 0 &&
    modules.length === tanks.reduce((total, tank) => total + Number(tank.assetCount), 0) &&
    modules.every((module) => states[module.state]);
  const kpis = [
    {
      label: "Hashrate",
      value: formatHashrateOrDash(hashrate),
      preformatted: true,
    },
    { label: "Container power", value: formatPowerKwOrDash(power), preformatted: true },
    {
      label: "Efficiency",
      value: formatEfficiencyOrDash(
        !stale && container.efficiencyJh !== undefined ? container.efficiencyJh * 1e12 : null,
      ),
      preformatted: true,
    },
    {
      label: "Modules hashing",
      value:
        !stale && allStatesKnown
          ? `${modules.filter((module) => module.state === "active").length}/${modules.length}`
          : "—",
      preformatted: true,
    },
  ];
  const cards: ContainerTank[] = [...tanks]
    .sort((a, b) => a.position - b.position)
    .map((tank) => {
      const children = modules
        .filter((module) =>
          module.ancestors.some((parent) => parent.kind === EntityKind.TANK && parent.id === tank.ref.id),
        )
        .sort((a, b) => a.position - b.position);
      const cols = Math.min(8, Math.max(1, children.length));
      const complete =
        children.length === Number(tank.assetCount) && children.every((module) => !!states[module.state]);
      const slots = children.map((module, index) => ({
        id: module.ref.id.toString(),
        position: module.position,
        label: module.name,
        state: stale ? "unknown" : (states[module.state] ?? "unknown"),
        row: Math.floor(index / cols) + 1,
        column: (index % cols) + 1,
      }));
      return {
        id: tank.ref.id.toString(),
        label: `Tank ${tank.position}`,
        cols,
        rows: Math.max(1, Math.ceil(children.length / cols)),
        on: undefined,
        modules: slots.map((slot) => slot.state),
        slots,
        statSubtitles: [
          undefined,
          !stale && tank.minTemperatureC !== undefined && tank.maxTemperatureC !== undefined
            ? tankMetricCoverage(tank, "temperature")
            : undefined,
          !stale && tank.powerKw !== undefined ? tankMetricCoverage(tank, "power") : undefined,
        ],
        stats: [
          !stale && complete
            ? `${children.filter((module) => module.state === "active").length}/${children.length} modules hashing`
            : `${tank.assetCount} modules · readings incomplete`,
          !stale && tank.minTemperatureC !== undefined && tank.maxTemperatureC !== undefined
            ? formatTempRange(tank.minTemperatureC, tank.maxTemperatureC, unit)
            : "—",
          formatPowerKwOrDash(stale ? null : tank.powerKw),
        ],
      };
    });
  return { kpis, tanks: cards, stale, unlocatedHashboards: stale ? undefined : container.unlocatedHashboards };
}
