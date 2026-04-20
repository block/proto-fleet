import {
  type DeviceSet,
  type DeviceSetStats,
  type RackSlotStatus,
  SlotDeviceStatus,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { SlotStatus } from "@/protoFleet/features/rackManagement/components/RackCard/types";
import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { formatTempRange } from "@/shared/utils/utility";

export const SLOT_STATUS_MAP: Record<SlotDeviceStatus, SlotStatus> = {
  [SlotDeviceStatus.UNSPECIFIED]: "empty",
  [SlotDeviceStatus.EMPTY]: "empty",
  [SlotDeviceStatus.HEALTHY]: "healthy",
  [SlotDeviceStatus.NEEDS_ATTENTION]: "needsAttention",
  [SlotDeviceStatus.OFFLINE]: "offline",
  [SlotDeviceStatus.SLEEPING]: "sleeping",
};

export type StatusSegment = { color: string; text: string };

export function deriveStatusSegments(stats: DeviceSetStats): StatusSegment[] {
  const issueCount =
    stats.controlBoardIssueCount + stats.fanIssueCount + stats.hashBoardIssueCount + stats.psuIssueCount;

  const segments: StatusSegment[] = [];
  if (issueCount > 0) segments.push({ color: "bg-intent-critical-fill", text: `${issueCount} issues` });
  if (stats.offlineCount > 0) segments.push({ color: "bg-intent-warning-fill", text: `${stats.offlineCount} offline` });
  if (stats.sleepingCount > 0) segments.push({ color: "bg-core-primary-20", text: `${stats.sleepingCount} sleeping` });

  if (segments.length === 0) {
    segments.push({ color: "bg-intent-success-fill", text: "Healthy" });
  }

  return segments;
}

export function mapSlotStatuses(slotStatuses: RackSlotStatus[], rows: number, cols: number): SlotStatus[] {
  // Build a row-major array (top-to-bottom, left-to-right) matching MiniRackGrid's render order.
  // Slot statuses carry physical (row, col) positions — place them directly by index.
  const grid: SlotStatus[] = new Array(rows * cols).fill("empty");

  for (const s of slotStatuses) {
    const index = s.row * cols + s.column;
    if (index >= 0 && index < grid.length) {
      grid[index] = SLOT_STATUS_MAP[s.status] ?? "empty";
    }
  }

  return grid;
}

export function formatRackCardStats(stats: DeviceSetStats, temperatureUnit: TemperatureUnit) {
  return {
    hashrate: stats.hashrateReportingCount > 0 ? `${getDisplayValue(stats.totalHashrateThs)} TH/s` : undefined,
    efficiency: stats.efficiencyReportingCount > 0 ? `${getDisplayValue(stats.avgEfficiencyJth)} J/TH` : undefined,
    power: stats.powerReportingCount > 0 ? `${getDisplayValue(stats.totalPowerKw)} kW` : undefined,
    temperature:
      stats.temperatureReportingCount > 0
        ? formatTempRange(stats.minTemperatureC, stats.maxTemperatureC, temperatureUnit)
        : undefined,
  };
}

export function mapRackToCardProps(
  rack: DeviceSet,
  stats: DeviceSetStats | undefined,
  temperatureUnit: TemperatureUnit,
) {
  const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
  const rows = rackInfo?.rows ?? 1;
  const cols = rackInfo?.columns ?? 1;

  const zone = rackInfo?.zone || undefined;

  if (!stats) {
    return {
      zone,
      rows,
      cols,
      loading: true,
      statusSegments: [] as StatusSegment[],
      slots: [] as SlotStatus[],
      hashrate: undefined,
      efficiency: undefined,
      power: undefined,
      temperature: undefined,
    };
  }

  const statusSegments = stats.deviceCount === 0 ? [] : deriveStatusSegments(stats);
  const slots = mapSlotStatuses(stats.slotStatuses, rows, cols);
  const { hashrate, efficiency, power, temperature } = formatRackCardStats(stats, temperatureUnit);

  return { zone, rows, cols, loading: false, statusSegments, slots, hashrate, efficiency, power, temperature };
}
