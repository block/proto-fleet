import { ModuleIssues } from "./ModuleIssues";
import { ModuleStatus } from "./ModuleStatus";
import { type TankMetric, tankMetricCoverage } from "./tankMetricCoverage";
import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { useTemperatureUnit } from "@/protoFleet/store";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import {
  convertCtoF,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
  formatPowerMwOrDash,
  formatTempRange,
} from "@/shared/utils/telemetryFormat";

// Shared read-only cells for tanks and modules. A controller's network address,
// firmware, and worker are deliberately not presented as module attributes.
export function InventoryCell({ item, column }: { item: InventoryItem; column: string }) {
  const unit = useTemperatureUnit();
  const stats = item.listStats;
  const metricValue = (metric: TankMetric, value: string) => {
    const coverage = value === "—" ? undefined : tankMetricCoverage(item, metric);
    return (
      <span>
        <span className="block">{value}</span>
        {coverage ? <span className="text-100 block text-text-primary-70">{coverage}</span> : null}
      </span>
    );
  };
  switch (column) {
    case "name":
      return (
        <span className="block truncate text-emphasis-300" title={item.name}>
          {item.name}
        </span>
      );
    case "site":
      return <span>{item.siteName || "—"}</span>;
    case "rack":
      return <span>{item.groupName || "—"}</span>;
    case "building":
      return <span>{item.facilityName || "—"}</span>;
    case "miners":
      return <span>{item.assetCount.toString()}</span>;
    case "hashrate":
      return metricValue("hashrate", formatHashrateOrDash(item.hashrateThs ?? null));
    case "power":
    case "powerUsage":
      return metricValue(
        "power",
        item.ref?.kind === EntityKind.CONTAINER
          ? formatPowerMwOrDash(item.powerKw ?? null)
          : formatPowerKwOrDash(item.powerKw),
      );
    case "efficiency":
      return metricValue("efficiency", formatEfficiencyOrDash(item.efficiencyJth ?? null));
    case "temperature":
      if (item.ref?.kind === EntityKind.MODULE) {
        // The inventory API puts the single stored module mean in both bounds.
        // Render it like a miner temperature, not a range of board readings.
        const mean = item.minTemperatureC;
        return metricValue(
          "temperature",
          mean === undefined ? "—" : `${getDisplayValue(unit === "F" ? convertCtoF(mean) : mean)} °${unit}`,
        );
      }
      return metricValue(
        "temperature",
        item.minTemperatureC === undefined || item.maxTemperatureC === undefined
          ? "—"
          : formatTempRange(item.minTemperatureC, item.maxTemperatureC, unit),
      );
    case "issues":
      return item.ref?.kind === EntityKind.MODULE ? <ModuleIssues item={item} /> : <span>—</span>;
    case "status":
      return <ModuleStatus state={item.state} />;
    case "health":
      return stats ? (
        <HealthBar
          healthy={stats.hashingCount}
          needsAttention={stats.brokenCount}
          offline={stats.offlineCount}
          sleeping={stats.sleepingCount}
        />
      ) : (
        <span>—</span>
      );
    default:
      return <span>—</span>;
  }
}
