import {
  type Rollout,
  RolloutDeviceState,
  type RolloutLaneModelGroup,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { Segment } from "@/shared/components/CompositionBar";

export type StatusTone = "neutral" | "progress" | "success" | "critical";

export interface RolloutDeviceCounts {
  updated: number;
  updating: number;
  pending: number;
  total: number;
  percent: number;
}

export function rolloutDeviceCounts(rollout: Rollout): RolloutDeviceCounts {
  let updated = 0;
  let updating = 0;
  let pending = 0;
  for (const device of rollout.devices) {
    if (device.state === RolloutDeviceState.UPDATED) updated += 1;
    else if (device.state === RolloutDeviceState.UPDATING) updating += 1;
    else pending += 1;
  }
  const total = rollout.devices.length;
  return { updated, updating, pending, total, percent: total === 0 ? 0 : Math.round((updated / total) * 100) };
}

// Rollout progress colors follow the active curtailment card: done is
// primary, in-flight is accent, queued is muted, failures are critical.
export const rolloutProgressColorMap: Record<Segment["status"], string> = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-core-accent-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-primary-10",
};

export function rolloutProgressSegments(counts: RolloutDeviceCounts): Segment[] {
  return [
    { name: "Updated", status: "OK", count: counts.updated },
    { name: "Updating", status: "WARNING", count: counts.updating },
    { name: "Pending", status: "NA", count: counts.pending },
  ];
}

export function rolloutProgressSummary(counts: RolloutDeviceCounts): string {
  const minerNoun = counts.total === 1 ? "miner" : "miners";
  if (counts.percent >= 100) {
    return `${counts.updated.toLocaleString()} ${minerNoun} updated (100%)`;
  }
  return `${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} ${minerNoun} updated (${counts.percent}%)`;
}

export const deviceStateLabels: Record<RolloutDeviceState, string> = {
  [RolloutDeviceState.UNSPECIFIED]: "",
  [RolloutDeviceState.PENDING]: "Pending",
  [RolloutDeviceState.UPDATING]: "Updating",
  [RolloutDeviceState.UPDATED]: "Updated",
};

export const rolloutStatusLabels: Record<RolloutStatus, string> = {
  [RolloutStatus.UNSPECIFIED]: "Unknown",
  [RolloutStatus.ACTIVE]: "In progress",
  [RolloutStatus.COMPLETED]: "Completed",
  [RolloutStatus.CANCELED]: "Canceled",
};

export const deviceStateTone = (state: RolloutDeviceState): StatusTone => {
  if (state === RolloutDeviceState.UPDATED) return "success";
  if (state === RolloutDeviceState.UPDATING) return "progress";
  return "neutral";
};

export const rolloutStatusTone = (status: RolloutStatus): StatusTone => {
  if (status === RolloutStatus.COMPLETED) return "success";
  if (status === RolloutStatus.ACTIVE) return "progress";
  return "neutral";
};

// "current → target" while miners converge on the assignment, or just the
// assigned version once every miner reports it.
export const modelFirmwareLabel = (group: RolloutLaneModelGroup): string => {
  if (group.firmwareVersion === "") return "—";
  const behind = [...new Set(group.miners.map((miner) => miner.firmwareVersion))].filter(
    (version) => version !== group.firmwareVersion,
  );
  if (behind.length === 0) return group.firmwareVersion;
  const current = behind.length === 1 ? behind[0] || "Unknown" : "Mixed";
  return `${current} → ${group.firmwareVersion}`;
};
