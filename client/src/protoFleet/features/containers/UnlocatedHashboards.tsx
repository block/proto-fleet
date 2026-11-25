import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import UnlocatedHashboards from "@/shared/components/UnlocatedHashboards";
import {
  convertCtoF,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
} from "@/shared/utils/telemetryFormat";

const statuses: Record<string, string> = {
  active: "Mining",
  error: "Needs attention",
  offline: "Offline",
  inactive: "Stopped",
  maintenance: "Isolated",
  updating: "Updating",
  needs_mining_pool: "Needs pool",
  reboot_required: "Reboot required",
};

export default function ContainerUnlocatedHashboards({
  container,
  failed = false,
}: {
  container: Container;
  failed?: boolean;
}) {
  const unit = useTemperatureUnit();
  const boards = container.unlocatedBoards.map((board, index) => ({
    id: board.macAddress || board.serial || `unknown-${index}`,
    identity: board.macAddress || board.serial || "Identity unavailable",
    serial: board.serial || undefined,
    firmware: board.firmwareVersion || undefined,
    power: formatPowerKwOrDash(board.stale || board.powerW === undefined ? null : board.powerW / 1000),
    efficiency: formatEfficiencyOrDash(
      board.stale || board.efficiencyJh === undefined ? null : board.efficiencyJh * 1e12,
    ),
    status: board.stale ? "Unknown" : (statuses[board.state] ?? "Unknown"),
    hashrate: formatHashrateOrDash(board.stale || board.hashrateHs === undefined ? null : board.hashrateHs / 1e12),
    temperature:
      board.stale || board.temperatureC === undefined
        ? "—"
        : `${(unit === "F" ? convertCtoF(board.temperatureC) : board.temperatureC).toFixed(1)} °${unit}`,
    issues: board.stale || !board.issuesKnown ? undefined : board.issues,
  }));
  return (
    <UnlocatedHashboards
      boards={boards}
      count={container.unlocatedHashboards ?? boards.length}
      stale={failed || container.stale}
    />
  );
}
