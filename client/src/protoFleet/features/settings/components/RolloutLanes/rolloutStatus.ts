import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type MetricComparison,
  type Rollout,
  RolloutCancelReason,
  type RolloutDevice,
  RolloutDeviceState,
  type RolloutLaneModelGroup,
  RolloutMethod,
  RolloutStage,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { Segment } from "@/shared/components/CompositionBar";
import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF, formatEfficiency, formatHashrate, formatPowerKwOrDash } from "@/shared/utils/telemetryFormat";

export type StatusTone = "neutral" | "progress" | "success" | "critical";

// A miner needs attention when the update left it worse off than its own
// baseline: new open errors, or on the version but offline / no longer
// hashing when it was hashing before.
export function deviceNeedsAttention(device: RolloutDevice, targetVersion: string): boolean {
  if (device.state === RolloutDeviceState.UPDATED) return false;
  if (device.openErrors > device.baselineOpenErrors) return true;
  if (device.firmwareVersion !== targetVersion) return false;
  return !device.online || (device.baselineHashing && !device.hashing);
}

export interface RolloutDeviceCounts {
  updated: number;
  // On the version, not yet back online / hashing. Counted with `updating`
  // in the progress bar, reported separately in text.
  verifying: number;
  updating: number;
  pending: number;
  // Worse off than baseline (see deviceNeedsAttention); a subset of the
  // non-updated miners, drawn as its own segment.
  attention: number;
  total: number;
  percent: number;
}

function deviceCounts(devices: RolloutDevice[], targetVersion: string): RolloutDeviceCounts {
  let updated = 0;
  let verifying = 0;
  let updating = 0;
  let pending = 0;
  let attention = 0;
  for (const device of devices) {
    if (device.state === RolloutDeviceState.UPDATED) updated += 1;
    else if (device.state === RolloutDeviceState.VERIFYING) verifying += 1;
    else if (device.state === RolloutDeviceState.UPDATING) updating += 1;
    else pending += 1;
    if (deviceNeedsAttention(device, targetVersion)) attention += 1;
  }
  const total = devices.length;
  return {
    updated,
    verifying,
    updating,
    pending,
    attention,
    total,
    percent: total === 0 ? 0 : Math.round((updated / total) * 100),
  };
}

export function rolloutDeviceCounts(rollout: Rollout): RolloutDeviceCounts {
  return deviceCounts(rollout.devices, rollout.firmwareVersion);
}

// Devices in the batch currently in flight or under review.
export function currentBatchDevices(rollout: Rollout): Rollout["devices"] {
  return rollout.devices.filter((device) => device.batch === rollout.currentBatch + 1);
}

// Progress of just the current batch of a staged rollout.
export function batchCounts(rollout: Rollout): RolloutDeviceCounts {
  return deviceCounts(currentBatchDevices(rollout), rollout.firmwareVersion);
}

// Devices whose evidence governs the rollout right now: the current batch
// while batching or at the gate, everything otherwise.
export function scopeDevices(rollout: Rollout): RolloutDevice[] {
  return isBatchStage(rollout) || isAwaitingReview(rollout) ? currentBatchDevices(rollout) : rollout.devices;
}

export function attentionDevices(rollout: Rollout): RolloutDevice[] {
  return rollout.devices.filter((device) => deviceNeedsAttention(device, rollout.firmwareVersion));
}

// An active rollout that is waiting on a human: at its review gate, or with
// miners worse off than before the update.
export function rolloutNeedsAttention(rollout: Rollout): boolean {
  if (rollout.status !== RolloutStatus.ACTIVE) return false;
  return isAwaitingReview(rollout) || attentionDevices(rollout).length > 0;
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

// The process step that leads the detail lockup, in the reference design's
// vocabulary: "Pilot batch", "Batch 2 of 5", "Pilot batch review",
// "Batch 2 review", "Remaining batch", "Paused", "Completed", "Aborted".
export function rolloutStageLabel(rollout: Rollout): string {
  if (rollout.status !== RolloutStatus.ACTIVE) return rolloutOutcomeLabel(rollout);
  if (isPaused(rollout)) return "Paused";
  const pilot = rollout.method === RolloutMethod.PILOT;
  if (isAwaitingReview(rollout)) return pilot ? "Pilot batch review" : `Batch ${rollout.currentBatch + 1} review`;
  if (isBatchStage(rollout))
    return pilot ? "Pilot batch" : `Batch ${rollout.currentBatch + 1} of ${rollout.batchCount}`;
  return isStaged(rollout) ? "Remaining batch" : "Updating";
}

// Rollout progress colors follow the active curtailment card: done is
// primary, remaining is accent, miners needing attention are critical.
export const rolloutProgressColorMap: Record<Segment["status"], string> = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-core-accent-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-primary-10",
};

// Updated / Remaining / Needs attention, dropping empty buckets (the
// reference design's three-segment bar).
export function rolloutProgressSegments(counts: RolloutDeviceCounts): Segment[] {
  const remaining = Math.max(counts.total - counts.updated - counts.attention, 0);
  return [
    { name: "Updated", status: "OK" as const, count: counts.updated },
    { name: "Remaining", status: "WARNING" as const, count: remaining },
    { name: "Needs attention", status: "CRITICAL" as const, count: counts.attention },
  ].filter((segment) => segment.count > 0);
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

// Update-status vocabulary for the release channels table, in the reference
// design's tones: attention (critical), active (primary), completed
// (success), none (muted).
export type UpdateTone = "attention" | "active" | "completed" | "none";

export interface UpdateStatus {
  label: string;
  tone: UpdateTone;
}

const shortDate = (timestamp?: Timestamp): string =>
  timestamp
    ? new Date(timestampMs(timestamp)).toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric",
      })
    : "";

// Status of one model group: its active rollout when there is one, else how
// the group stands against its assignment (with the last completed update's
// date when known).
export function modelUpdateStatus(
  group: RolloutLaneModelGroup,
  activeRollout: Rollout | undefined,
  lastCompleted: Rollout | undefined,
): UpdateStatus {
  if (activeRollout) {
    const counts = scopeCounts(activeRollout);
    if (isPaused(activeRollout)) return { label: `Paused, ${counts.updated} of ${counts.total}`, tone: "none" };
    if (isAwaitingReview(activeRollout)) return { label: "Review needed", tone: "attention" };
    const attention = attentionDevices(activeRollout).length;
    if (attention > 0) {
      return { label: `Needs attention, ${attention} of ${counts.total}`, tone: "attention" };
    }
    const progress = `${counts.updated} of ${counts.total}`;
    return {
      label: isBatchStage(activeRollout)
        ? `${batchLabel(activeRollout)}: updating ${progress}`
        : `Updating, ${progress}`,
      tone: "active",
    };
  }
  if (group.firmwareVersion === "") return { label: "No firmware assigned", tone: "none" };
  if (group.miners.length === 0) return { label: "No miners", tone: "none" };
  const onTarget = group.miners.filter((miner) => miner.firmwareVersion === group.firmwareVersion).length;
  if (onTarget === group.miners.length) {
    const finished = lastCompleted?.finishedAt ? shortDate(lastCompleted.finishedAt) : "";
    return { label: finished ? `Updated ${finished}` : "Up to date", tone: "completed" };
  }
  return { label: `${onTarget} of ${group.miners.length} on target`, tone: "none" };
}

// Roll-up of a channel's active rollouts: "2 active, 1 needs attention".
export function channelUpdateStatus(activeRollouts: Rollout[]): UpdateStatus {
  if (activeRollouts.length === 0) return { label: "No active updates", tone: "none" };
  const attention = activeRollouts.filter(rolloutNeedsAttention).length;
  const paused = activeRollouts.filter(isPaused).length;
  const parts = [`${activeRollouts.length} active`];
  if (attention > 0) parts.push(attention === 1 ? "1 needs attention" : `${attention} need attention`);
  if (paused > 0) parts.push(`${paused} paused`);
  return { label: parts.join(", "), tone: attention > 0 ? "attention" : "active" };
}

// ---------------------------------------------------------------------------
// Telemetry against baseline
// ---------------------------------------------------------------------------

const HS_PER_TH = 1e12;

// Server hashrates are in H/s; the shared formatter takes TH/s.
export function formatHashRateHs(hashRateHs: number): string {
  return formatHashrate(hashRateHs / HS_PER_TH) ?? "—";
}

export function formatPercentChange(percent: number): string {
  const sign = percent > 0 ? "+" : "";
  return `${sign}${percent.toFixed(1)}%`;
}

export type MetricKind = "hashrate" | "power" | "efficiency" | "temperature";

export const metricLabels: Record<MetricKind, string> = {
  hashrate: "Hashrate",
  power: "Power",
  efficiency: "Efficiency",
  temperature: "Temp",
};

export type DeltaIntent = "positive" | "negative" | "neutral";

export interface MetricDisplay {
  label: string;
  // Current reading, or "—" when there is no recent sample.
  value: string;
  // Signed change versus baseline; undefined when either half is missing.
  delta?: string;
  deltaIntent: DeltaIntent;
}

export function formatMetricValue(kind: MetricKind, value: number, temperatureUnit: TemperatureUnit): string {
  switch (kind) {
    case "hashrate":
      return formatHashRateHs(value);
    case "power":
      return formatPowerKwOrDash(value / 1000);
    case "efficiency":
      return formatEfficiency(value) ?? "—";
    case "temperature": {
      const shown = temperatureUnit === "F" ? convertCtoF(value) : value;
      return `${getDisplayValue(shown)} °${temperatureUnit}`;
    }
  }
}

// Whether a move in this metric is good news for the miner: more hashrate is
// good; more power, worse efficiency (J/TH) and higher temperature are bad.
function deltaIntent(kind: MetricKind, change: number): DeltaIntent {
  if (Math.abs(change) < 0.5) return "neutral";
  const up = change > 0;
  return (kind === "hashrate") === up ? "positive" : "negative";
}

// Structural so both generated MetricComparison messages and plain
// before/after pairs can be passed.
export type MetricPair = Pick<MetricComparison, "baseline" | "current">;

export function metricDisplay(
  kind: MetricKind,
  metric: MetricPair | undefined,
  temperatureUnit: TemperatureUnit,
): MetricDisplay {
  const label = metricLabels[kind];
  const current = metric?.current;
  const baseline = metric?.baseline;
  if (current === undefined) return { label, value: "—", deltaIntent: "neutral" };
  const value = formatMetricValue(kind, current, temperatureUnit);
  if (baseline === undefined) return { label, value, deltaIntent: "neutral" };
  if (kind === "temperature") {
    // Degrees, not percent: a 2° swing reads as a 2° swing.
    const degrees = temperatureUnit === "F" ? convertCtoF(current) - convertCtoF(baseline) : current - baseline;
    const sign = degrees > 0 ? "+" : "";
    return {
      label,
      value,
      delta: `${sign}${degrees.toFixed(1)} °${temperatureUnit}`,
      deltaIntent: deltaIntent(kind, degrees),
    };
  }
  if (baseline === 0) return { label, value, deltaIntent: "neutral" };
  const percent = ((current - baseline) / baseline) * 100;
  return { label, value, delta: formatPercentChange(percent), deltaIntent: deltaIntent(kind, percent) };
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
