import {
  type Rollout,
  RolloutCancelReason,
  RolloutDeviceState,
  type RolloutLaneModelGroup,
  RolloutMethod,
  RolloutStage,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { Segment } from "@/shared/components/CompositionBar";
import { formatHashrate } from "@/shared/utils/telemetryFormat";

export type StatusTone = "neutral" | "progress" | "success" | "critical";

export interface RolloutDeviceCounts {
  updated: number;
  // On the version, not yet back online / hashing. Counted with `updating`
  // in the progress bar, reported separately in text.
  verifying: number;
  updating: number;
  pending: number;
  total: number;
  percent: number;
}

function deviceCounts(devices: Rollout["devices"]): RolloutDeviceCounts {
  let updated = 0;
  let verifying = 0;
  let updating = 0;
  let pending = 0;
  for (const device of devices) {
    if (device.state === RolloutDeviceState.UPDATED) updated += 1;
    else if (device.state === RolloutDeviceState.VERIFYING) verifying += 1;
    else if (device.state === RolloutDeviceState.UPDATING) updating += 1;
    else pending += 1;
  }
  const total = devices.length;
  return {
    updated,
    verifying,
    updating,
    pending,
    total,
    percent: total === 0 ? 0 : Math.round((updated / total) * 100),
  };
}

export function rolloutDeviceCounts(rollout: Rollout): RolloutDeviceCounts {
  return deviceCounts(rollout.devices);
}

// Devices in the batch currently in flight or under review.
export function currentBatchDevices(rollout: Rollout): Rollout["devices"] {
  return rollout.devices.filter((device) => device.batch === rollout.currentBatch + 1);
}

// Progress of just the current batch of a staged rollout.
export function batchCounts(rollout: Rollout): RolloutDeviceCounts {
  return deviceCounts(currentBatchDevices(rollout));
}

// The counts that describe what the rollout is working on right now: the
// current batch while batching or at the gate, everything otherwise.
export function scopeCounts(rollout: Rollout): RolloutDeviceCounts {
  return isBatchStage(rollout) || isAwaitingReview(rollout) ? batchCounts(rollout) : rolloutDeviceCounts(rollout);
}

export function isStaged(rollout: Rollout): boolean {
  return rollout.method === RolloutMethod.PILOT || rollout.method === RolloutMethod.BATCHES;
}

export function isPaused(rollout: Rollout): boolean {
  return rollout.status === RolloutStatus.ACTIVE && rollout.pausedAt !== undefined;
}

// True while a staged rollout sits at its review gate: the batch is done and
// the remaining miners wait for the rollout to be continued.
export function isAwaitingReview(rollout: Rollout): boolean {
  return rollout.status === RolloutStatus.ACTIVE && rollout.stage === RolloutStage.AWAITING_REVIEW;
}

export function isBatchStage(rollout: Rollout): boolean {
  return rollout.status === RolloutStatus.ACTIVE && rollout.stage === RolloutStage.BATCH;
}

// "Pilot" for pilot rollouts, "Batch 2 of 5" for fixed batches.
export function batchLabel(rollout: Rollout): string {
  if (rollout.method === RolloutMethod.PILOT) return "Pilot";
  return `Batch ${rollout.currentBatch + 1} of ${rollout.batchCount}`;
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
    { name: "Updating", status: "WARNING", count: counts.updating + counts.verifying },
    { name: "Pending", status: "NA", count: counts.pending },
  ];
}

export function rolloutProgressSummary(counts: RolloutDeviceCounts): string {
  const minerNoun = counts.total === 1 ? "miner" : "miners";
  if (counts.percent >= 100) {
    return `${counts.updated.toLocaleString()} ${minerNoun} updated (100%)`;
  }
  const verifying = counts.verifying > 0 ? `, ${counts.verifying.toLocaleString()} verifying` : "";
  return `${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} ${minerNoun} updated (${counts.percent}%)${verifying}`;
}

export const deviceStateLabels: Record<RolloutDeviceState, string> = {
  [RolloutDeviceState.UNSPECIFIED]: "",
  [RolloutDeviceState.PENDING]: "Pending",
  [RolloutDeviceState.UPDATING]: "Updating",
  [RolloutDeviceState.VERIFYING]: "Verifying",
  [RolloutDeviceState.UPDATED]: "Updated",
};

export const rolloutStatusLabels: Record<RolloutStatus, string> = {
  [RolloutStatus.UNSPECIFIED]: "Unknown",
  [RolloutStatus.ACTIVE]: "In progress",
  [RolloutStatus.COMPLETED]: "Completed",
  [RolloutStatus.CANCELED]: "Canceled",
};

export const rolloutMethodLabels: Record<RolloutMethod, string> = {
  [RolloutMethod.UNSPECIFIED]: "All at once",
  [RolloutMethod.IMMEDIATE]: "All at once",
  [RolloutMethod.PILOT]: "Pilot first",
  [RolloutMethod.BATCHES]: "Fixed batches",
};

const cancelReasonLabels: Record<RolloutCancelReason, string> = {
  [RolloutCancelReason.UNSPECIFIED]: "Canceled",
  [RolloutCancelReason.SUPERSEDED]: "Superseded",
  [RolloutCancelReason.ABORTED]: "Aborted",
  [RolloutCancelReason.CLEARED]: "Canceled",
};

// Outcome label for history rows: distinguishes an operator abort from a
// rollout that was simply replaced by a newer assignment.
export function rolloutOutcomeLabel(rollout: Rollout): string {
  if (rollout.status === RolloutStatus.CANCELED) return cancelReasonLabels[rollout.cancelReason];
  return rolloutStatusLabels[rollout.status];
}

// Status headline for a rollout, aware of pause, stage and batch.
export function rolloutStatusHeadline(rollout: Rollout): string {
  if (rollout.status !== RolloutStatus.ACTIVE) return rolloutOutcomeLabel(rollout);
  if (isPaused(rollout)) return "Paused";
  if (isBatchStage(rollout)) return `${batchLabel(rollout)} in progress`;
  if (isAwaitingReview(rollout)) return `${batchLabel(rollout)} complete — review needed`;
  return rolloutStatusLabels[rollout.status];
}

export const deviceStateTone = (state: RolloutDeviceState): StatusTone => {
  if (state === RolloutDeviceState.UPDATED) return "success";
  if (state === RolloutDeviceState.UPDATING || state === RolloutDeviceState.VERIFYING) return "progress";
  return "neutral";
};

export const rolloutStatusTone = (rollout: Rollout): StatusTone => {
  if (rollout.status === RolloutStatus.COMPLETED) return "success";
  if (rollout.status === RolloutStatus.ACTIVE) return "progress";
  if (rollout.status === RolloutStatus.CANCELED && rollout.cancelReason === RolloutCancelReason.ABORTED) {
    return "critical";
  }
  return "neutral";
};

const HS_PER_TH = 1e12;

// Server hashrates are in H/s; the shared formatter takes TH/s.
export function formatHashRateHs(hashRateHs: number): string {
  return formatHashrate(hashRateHs / HS_PER_TH) ?? "—";
}

export function formatPercentChange(percent: number): string {
  const sign = percent > 0 ? "+" : "";
  return `${sign}${percent.toFixed(1)}%`;
}

export function formatDurationSeconds(seconds: number): string {
  if (seconds <= 0) return "0s";
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours}h` : `${hours}h ${rest}m`;
}

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
