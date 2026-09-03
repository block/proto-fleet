import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type MetricComparison,
  type ReleaseChannelModelGroup,
  type Rollout,
  type RolloutBehavior,
  RolloutCancelReason,
  type RolloutDevice,
  RolloutDevicePhase,
  RolloutMethod,
  RolloutOrder,
  RolloutStage,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { Segment } from "@/shared/components/CompositionBar";
import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF, formatEfficiency, formatHashrate, formatPowerKwOrDash } from "@/shared/utils/telemetryFormat";

// Operator-facing vocabulary for firmware updates. Labels follow the rollout
// framework design (PR #881): the engine is a "rollout" in code and a
// "firmware update" to operators; methods are Single batch / Multiple batches
// / Pilot batch, then remaining; miners are Updated / Updating / Retrying /
// Queued / Failed / Excluded.

// ---------------------------------------------------------------------------
// Method, order, state and phase labels
// ---------------------------------------------------------------------------

export const methodLabels: Record<RolloutMethod, string> = {
  [RolloutMethod.UNSPECIFIED]: "Single batch",
  [RolloutMethod.ALL_AT_ONCE]: "Single batch",
  [RolloutMethod.BATCHED]: "Multiple batches",
  [RolloutMethod.PILOT_THEN_CONTINUE]: "Pilot batch, then remaining",
};

export const methodHelpText: Record<RolloutMethod, string> = {
  [RolloutMethod.UNSPECIFIED]: "",
  [RolloutMethod.ALL_AT_ONCE]: "Every miner not on the new version updates at once.",
  [RolloutMethod.BATCHED]:
    "Miners update in batches of a fixed size. Review after each batch, or let batches follow one another.",
  [RolloutMethod.PILOT_THEN_CONTINUE]:
    "A pilot batch updates first. The remaining miners wait until the pilot is reviewed and the update continued.",
};

export const orderLabels: Record<RolloutOrder, string> = {
  [RolloutOrder.UNSPECIFIED]: "Least efficient first",
  [RolloutOrder.LEAST_EFFICIENT_FIRST]: "Least efficient first",
  [RolloutOrder.RANDOM]: "Random",
};

export const phaseLabels: Record<RolloutDevicePhase, string> = {
  [RolloutDevicePhase.UNSPECIFIED]: "",
  [RolloutDevicePhase.QUEUED]: "Queued",
  [RolloutDevicePhase.IN_PROGRESS]: "Updating",
  [RolloutDevicePhase.RETRYING]: "Retrying",
  [RolloutDevicePhase.DONE]: "Updated",
  [RolloutDevicePhase.FAILED]: "Failed",
  [RolloutDevicePhase.EXCLUDED]: "Excluded",
};

const cancelReasonLabels: Record<RolloutCancelReason, string> = {
  [RolloutCancelReason.UNSPECIFIED]: "Canceled",
  [RolloutCancelReason.SUPERSEDED]: "Superseded",
  [RolloutCancelReason.CANCELED_REMAINING]: "Canceled",
  [RolloutCancelReason.ROLLED_BACK]: "Rolled back",
  [RolloutCancelReason.CLEARED]: "Canceled",
};

// Outcome label for history rows: distinguishes a cancel from a rollout that
// was simply replaced by a newer assignment or a rollback.
export function rolloutOutcomeLabel(rollout: Rollout): string {
  switch (rollout.status) {
    case RolloutStatus.COMPLETED:
      return "Completed";
    case RolloutStatus.COMPLETED_WITH_FAILURES:
      return "Completed with failures";
    case RolloutStatus.CANCELED:
      return cancelReasonLabels[rollout.cancelReason];
    default:
      return "In progress";
  }
}

// The process step that leads the detail lockup: "Pilot batch", "Batch 2 of
// 5", "Pilot batch review", "Batch review", "Waiting for telemetry",
// "Remaining batch", "Paused", "Completed", "Completed with failures".
export function rolloutStageLabel(rollout: Rollout): string {
  switch (rollout.state) {
    case RolloutState.PAUSED:
      return "Paused";
    case RolloutState.STABILIZING_TELEMETRY:
      return "Waiting for telemetry";
    case RolloutState.PAUSED_AT_PILOT_GATE:
      return "Pilot batch review";
    case RolloutState.PAUSED_AT_BATCH_REVIEW:
      return "Batch review";
    case RolloutState.COMPLETED:
      return "Completed";
    case RolloutState.COMPLETED_WITH_FAILURES:
      return "Completed with failures";
    case RolloutState.CANCELED:
      return rolloutOutcomeLabel(rollout);
    default:
      break;
  }
  if (rollout.stage === RolloutStage.BATCH) {
    return isPilot(rollout) ? "Pilot batch" : `Batch ${rollout.currentBatch + 1} of ${rollout.batchCount}`;
  }
  if (rollout.stage === RolloutStage.WAITING) return "Waiting for the next batch";
  return isStaged(rollout) ? "Remaining batch" : "In progress";
}

// Short label for the batch under way or under review.
export function batchLabel(rollout: Rollout): string {
  if (isPilot(rollout)) return "Pilot batch";
  return `Batch ${rollout.currentBatch + 1} of ${rollout.batchCount}`;
}

// One line describing how an update is paced, for stat lockups and confirms.
export function pacingSummary(b: RolloutBehavior | undefined): string {
  if (!b || b.method === RolloutMethod.ALL_AT_ONCE || b.method === RolloutMethod.UNSPECIFIED) return "Single batch";
  if (b.method === RolloutMethod.PILOT_THEN_CONTINUE) {
    return `Pilot batch of ${b.pilotSize.toLocaleString()}, then remaining`;
  }
  const review = b.reviewAfterEachBatch
    ? b.autoContinueOnHealthyTelemetry
      ? "auto-continue when healthy"
      : "review after each batch"
    : b.waitBetweenBatchesSeconds > 0
      ? `${formatDurationSeconds(b.waitBetweenBatchesSeconds)} between batches`
      : "back to back";
  return `Batches of ${b.batchSize.toLocaleString()}, ${review}`;
}

// ---------------------------------------------------------------------------
// Rollout predicates
// ---------------------------------------------------------------------------

export function isStaged(rollout: Rollout): boolean {
  const method = rollout.behavior?.method;
  return method === RolloutMethod.BATCHED || method === RolloutMethod.PILOT_THEN_CONTINUE;
}

export function isPilot(rollout: Rollout): boolean {
  return rollout.behavior?.method === RolloutMethod.PILOT_THEN_CONTINUE;
}

export function isActive(rollout: Rollout): boolean {
  return rollout.status === RolloutStatus.ACTIVE;
}

export function isPaused(rollout: Rollout): boolean {
  return rollout.state === RolloutState.PAUSED;
}

// True while a staged rollout sits at its review gate (manual or waiting on
// telemetry): the batch is done and the remaining miners wait.
export function isAwaitingReview(rollout: Rollout): boolean {
  return isActive(rollout) && rollout.stage === RolloutStage.AWAITING_REVIEW;
}

// True at a gate that needs a human: auto-continue is off, or its
// conditions do not hold.
export function needsManualReview(rollout: Rollout): boolean {
  return rollout.state === RolloutState.PAUSED_AT_PILOT_GATE || rollout.state === RolloutState.PAUSED_AT_BATCH_REVIEW;
}

export function isBatchStage(rollout: Rollout): boolean {
  return isActive(rollout) && rollout.stage === RolloutStage.BATCH;
}

export function isTerminal(rollout: Rollout): boolean {
  return !isActive(rollout);
}

export function hasFailures(rollout: Rollout): boolean {
  return rollout.devices.some((device) => device.phase === RolloutDevicePhase.FAILED);
}

// An active rollout that is waiting on a human: at a manual gate, or with
// failed miners.
export function rolloutNeedsAttention(rollout: Rollout): boolean {
  return isActive(rollout) && (needsManualReview(rollout) || hasFailures(rollout));
}

// ---------------------------------------------------------------------------
// Device counts and progress
// ---------------------------------------------------------------------------

export interface RolloutDeviceCounts {
  updated: number;
  updating: number;
  retrying: number;
  queued: number;
  failed: number;
  excluded: number;
  // Miners in play: everything except excluded.
  total: number;
  percent: number;
}

function deviceCounts(devices: RolloutDevice[]): RolloutDeviceCounts {
  const counts: RolloutDeviceCounts = {
    updated: 0,
    updating: 0,
    retrying: 0,
    queued: 0,
    failed: 0,
    excluded: 0,
    total: 0,
    percent: 0,
  };
  for (const device of devices) {
    switch (device.phase) {
      case RolloutDevicePhase.DONE:
        counts.updated += 1;
        break;
      case RolloutDevicePhase.IN_PROGRESS:
        counts.updating += 1;
        break;
      case RolloutDevicePhase.RETRYING:
        counts.retrying += 1;
        break;
      case RolloutDevicePhase.FAILED:
        counts.failed += 1;
        break;
      case RolloutDevicePhase.EXCLUDED:
        counts.excluded += 1;
        break;
      default:
        counts.queued += 1;
    }
  }
  counts.total = devices.length - counts.excluded;
  counts.percent = counts.total === 0 ? 0 : Math.round((counts.updated / counts.total) * 100);
  return counts;
}

export function rolloutDeviceCounts(rollout: Rollout): RolloutDeviceCounts {
  return deviceCounts(rollout.devices);
}

// Devices in the batch currently in flight or under review.
export function currentBatchDevices(rollout: Rollout): RolloutDevice[] {
  return rollout.devices.filter((device) => device.batch === rollout.currentBatch + 1);
}

// Devices whose evidence governs the rollout right now: the current batch
// while batching, waiting or at the gate; everything otherwise.
export function scopeDevices(rollout: Rollout): RolloutDevice[] {
  return isActive(rollout) && rollout.stage !== RolloutStage.REST ? currentBatchDevices(rollout) : rollout.devices;
}

export function scopeCounts(rollout: Rollout): RolloutDeviceCounts {
  return deviceCounts(scopeDevices(rollout));
}

export function failedDevices(rollout: Rollout): RolloutDevice[] {
  return rollout.devices.filter((device) => device.phase === RolloutDevicePhase.FAILED);
}

// Progress colors follow the active curtailment card: done is primary,
// remaining is accent, failed is critical.
export const rolloutProgressColorMap: Record<Segment["status"], string> = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-core-accent-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-primary-10",
};

// Updated / Remaining / Failed, dropping empty buckets.
export function rolloutProgressSegments(counts: RolloutDeviceCounts): Segment[] {
  const remaining = counts.updating + counts.retrying + counts.queued;
  return [
    { name: "Updated", status: "OK" as const, count: counts.updated },
    { name: "Remaining", status: "WARNING" as const, count: remaining },
    { name: "Failed", status: "CRITICAL" as const, count: counts.failed },
  ].filter((segment) => segment.count > 0);
}

export function rolloutProgressSummary(counts: RolloutDeviceCounts): string {
  const minerNoun = counts.total === 1 ? "miner" : "miners";
  const failed = counts.failed > 0 ? `, ${counts.failed.toLocaleString()} failed` : "";
  if (counts.percent >= 100) {
    return `${counts.updated.toLocaleString()} ${minerNoun} updated (100%)${failed}`;
  }
  return `${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} ${minerNoun} updated (${counts.percent}%)${failed}`;
}

// "74 of 87 miners updated, 2 failed, Batch 5 of 6": the one-line summary for
// banners and the header pill.
export function activeUpdateSummary(rollout: Rollout): string {
  const counts = rolloutDeviceCounts(rollout);
  const failed = failedDevices(rollout).length;
  const parts = [`${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} miners updated`];
  if (failed > 0) parts.push(failed === 1 ? "1 failed" : `${failed.toLocaleString()} failed`);
  if (isPaused(rollout) || isAwaitingReview(rollout) || isBatchStage(rollout)) parts.push(rolloutStageLabel(rollout));
  return parts.join(", ");
}

// ---------------------------------------------------------------------------
// Channel and model status
// ---------------------------------------------------------------------------

export type StatusTone = "neutral" | "progress" | "success" | "critical";

export const phaseTone = (phase: RolloutDevicePhase): StatusTone => {
  switch (phase) {
    case RolloutDevicePhase.DONE:
      return "success";
    case RolloutDevicePhase.IN_PROGRESS:
    case RolloutDevicePhase.RETRYING:
      return "progress";
    case RolloutDevicePhase.FAILED:
      return "critical";
    default:
      return "neutral";
  }
};

export const rolloutStatusTone = (rollout: Rollout): StatusTone => {
  switch (rollout.status) {
    case RolloutStatus.COMPLETED:
      return "success";
    case RolloutStatus.COMPLETED_WITH_FAILURES:
      return "critical";
    case RolloutStatus.ACTIVE:
      return "progress";
    default:
      return "neutral";
  }
};

// Update-status vocabulary for the release channels table, in the design's
// tones: attention (critical), active (primary), completed (success), none
// (muted).
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
  group: ReleaseChannelModelGroup,
  activeRollout: Rollout | undefined,
  lastFinished: Rollout | undefined,
): UpdateStatus {
  if (activeRollout) {
    const counts = scopeCounts(activeRollout);
    const progress = `${counts.updated} of ${counts.total}`;
    if (isPaused(activeRollout)) return { label: `Paused, ${progress}`, tone: "none" };
    if (counts.failed > 0) {
      return { label: `${counts.failed} failed, ${progress} updated`, tone: "attention" };
    }
    if (needsManualReview(activeRollout)) return { label: "Review needed", tone: "attention" };
    if (activeRollout.state === RolloutState.STABILIZING_TELEMETRY) {
      return { label: `Waiting for telemetry, ${progress}`, tone: "active" };
    }
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
  if (lastFinished?.status === RolloutStatus.COMPLETED_WITH_FAILURES && onTarget < group.miners.length) {
    return { label: `${group.miners.length - onTarget} failed to update`, tone: "attention" };
  }
  if (onTarget === group.miners.length) {
    const finished = lastFinished?.finishedAt ? shortDate(lastFinished.finishedAt) : "";
    return { label: finished ? `Updated ${finished}` : "Up to date", tone: "completed" };
  }
  return { label: `${onTarget} of ${group.miners.length} on target`, tone: "none" };
}

// Roll-up of a channel's active rollouts: "2 updating, 1 needs review".
export function channelUpdateStatus(activeRollouts: Rollout[]): UpdateStatus {
  if (activeRollouts.length === 0) return { label: "No active updates", tone: "none" };
  const attention = activeRollouts.filter(rolloutNeedsAttention).length;
  const paused = activeRollouts.filter(isPaused).length;
  const parts = [activeRollouts.length === 1 ? "1 updating" : `${activeRollouts.length} updating`];
  if (attention > 0) parts.push(attention === 1 ? "1 needs attention" : `${attention} need attention`);
  if (paused > 0) parts.push(`${paused} paused`);
  return { label: parts.join(", "), tone: attention > 0 ? "attention" : "active" };
}

// "current → target" while miners converge on the assignment, or just the
// assigned version once every miner reports it.
export const modelFirmwareLabel = (group: ReleaseChannelModelGroup): string => {
  if (group.firmwareVersion === "") return "—";
  const behind = [...new Set(group.miners.map((miner) => miner.firmwareVersion))].filter(
    (version) => version !== group.firmwareVersion,
  );
  if (behind.length === 0) return group.firmwareVersion;
  const current = behind.length === 1 ? behind[0] || "Unknown" : "Mixed";
  return `${current} → ${group.firmwareVersion}`;
};

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
