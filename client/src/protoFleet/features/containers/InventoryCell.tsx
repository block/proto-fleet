import { ModuleIssues } from "./ModuleIssues";
import { ModuleStatus } from "./ModuleStatus";
import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { useTemperatureUnit } from "@/protoFleet/store";
import {
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
      return <span>{formatHashrateOrDash(item.hashrateThs ?? null)}</span>;
    case "power":
    case "powerUsage":
      return (
        <span>
          {item.ref?.kind === EntityKind.CONTAINER
            ? formatPowerMwOrDash(item.powerKw ?? null)
            : formatPowerKwOrDash(item.powerKw)}
        </span>
      );
    case "efficiency":
      return <span>{formatEfficiencyOrDash(item.efficiencyJth ?? null)}</span>;
    case "temperature":
      return (
        <span>
          {item.minTemperatureC === undefined || item.maxTemperatureC === undefined
            ? "—"
            : formatTempRange(item.minTemperatureC, item.maxTemperatureC, unit)}
        </span>
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
