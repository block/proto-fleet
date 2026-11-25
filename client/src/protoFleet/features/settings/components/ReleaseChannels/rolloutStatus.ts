import { create } from "@bufbuild/protobuf";
import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import { firmwareComponentLabel } from "@/protoFleet/api/firmwareComponent";
import {
  type MetricComparison,
  type RolloutDeviceCounts as PbRolloutDeviceCounts,
  type Rollout,
  type RolloutBehavior,
  RolloutCancelReason,
  type RolloutDevice,
  RolloutDeviceCountsSchema,
  RolloutDevicePhase,
  RolloutMethod,
  RolloutOrder,
  RolloutStage,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { isCT1FirmwareTarget } from "@/protoFleet/api/useFirmwareApi";
import type {
  ChannelView,
  ChannelModelGroupView as ReleaseChannelModelGroup,
} from "@/protoFleet/api/useReleaseChannels";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
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
  [RolloutMethod.DELEGATED]: "Controlled externally",
};

/** What operators call a set of devices: miners, containers, or devices when it holds both. */
export interface DeviceNouns {
  singular: string;
  plural: string;
}

export const MINER_NOUNS: DeviceNouns = { singular: "miner", plural: "miners" };
export const CONTAINER_NOUNS: DeviceNouns = { singular: "container", plural: "containers" };
export const DEVICE_NOUNS: DeviceNouns = { singular: "device", plural: "devices" };

export const methodHelp = (method: RolloutMethod, nouns: DeviceNouns = MINER_NOUNS): string => {
  switch (method) {
    case RolloutMethod.BATCHED:
      return `${capitalize(nouns.plural)} update in batches of a fixed size. Review after each batch, or let batches follow one another.`;
    case RolloutMethod.PILOT_THEN_CONTINUE:
      return `A pilot batch updates first. The remaining ${nouns.plural} wait until the pilot is reviewed and the update continued.`;
    case RolloutMethod.DELEGATED:
      return `An external controller decides which ${nouns.plural} update and when, through the API.`;
    default:
      return `Every ${nouns.singular} not on the new version updates at once.`;
  }
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
  [RolloutDevicePhase.SKIPPED]: "Skipped",
};

// Finished history retains these phases; once the rollout is canceled the
// miner is no longer queued for dispatch or actively monitored.
export const isUnfinishedPhase = (phase: RolloutDevicePhase): boolean =>
  phase === RolloutDevicePhase.QUEUED ||
  phase === RolloutDevicePhase.IN_PROGRESS ||
  phase === RolloutDevicePhase.RETRYING;

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
    case RolloutState.WAITING_FOR_CONTROLLER:
      return "Waiting for controller";
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
  if (b.method === RolloutMethod.DELEGATED) return "Controlled externally";
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

export function failedCount(rollout: Rollout): number {
  return rollout.deviceCounts?.failed ?? 0;
}

export function hasFailures(rollout: Rollout): boolean {
  return failedCount(rollout) > 0;
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
  skipped: number;
  // Miners in play: everything except the neutral excluded and skipped.
  total: number;
  percent: number;
}

function finishCounts(
  counts: Omit<RolloutDeviceCounts, "total" | "percent">,
  snapshotted: number,
): RolloutDeviceCounts {
  const total = snapshotted - counts.excluded - counts.skipped;
  // Rounded progress must not report completion while any miner remains.
  const percent =
    total === 0 ? 0 : Math.min(counts.updated === total ? 100 : 99, Math.round((counts.updated / total) * 100));
  return { ...counts, total, percent };
}

// The server's per-phase tally, in the adapter's vocabulary.
function countsFromSummary(summary: PbRolloutDeviceCounts | undefined): RolloutDeviceCounts {
  const c = summary ?? create(RolloutDeviceCountsSchema);
  return finishCounts(
    {
      updated: c.done,
      updating: c.inProgress,
      retrying: c.retrying,
      queued: c.queued,
      failed: c.failed,
      excluded: c.excluded,
      skipped: c.skipped,
    },
    c.queued + c.inProgress + c.retrying + c.done + c.failed + c.excluded + c.skipped,
  );
}

// Tally a device list a client fetched itself (ListRolloutDevices).
export function deviceCounts(devices: RolloutDevice[]): RolloutDeviceCounts {
  const counts = { updated: 0, updating: 0, retrying: 0, queued: 0, failed: 0, excluded: 0, skipped: 0 };
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
      case RolloutDevicePhase.SKIPPED:
        counts.skipped += 1;
        break;
      default:
        counts.queued += 1;
    }
  }
  return finishCounts(counts, devices.length);
}

// Counts over every miner snapshotted into the rollout.
export function rolloutDeviceCounts(rollout: Rollout): RolloutDeviceCounts {
  return countsFromSummary(rollout.deviceCounts);
}

// Progress covers the current batch while batching, waiting or at the gate.
// REST progress covers every target, including earlier batches that drifted.
export function scopedToBatch(rollout: Rollout): boolean {
  return isActive(rollout) && rollout.batchCount > 0 && rollout.stage !== RolloutStage.REST;
}

// Progress counts, not telemetry evidence counts: REST evidence omits earlier
// batches, but those targets can still need work and belong in overall progress.
export function scopeCounts(rollout: Rollout): RolloutDeviceCounts {
  return scopedToBatch(rollout) ? countsFromSummary(rollout.currentBatchCounts) : rolloutDeviceCounts(rollout);
}

// Evidence follows the current batch until REST, then only the unbatched
// targets. Non-staged rollouts have only unbatched targets. Terminal rollouts
// have no live evidence; their device lists describe the whole update.
export function scopeDevices(rollout: Rollout, devices: RolloutDevice[]): RolloutDevice[] {
  if (scopedToBatch(rollout)) return devices.filter((device) => device.batch === rollout.currentBatch + 1);
  if (isActive(rollout) && rollout.stage === RolloutStage.REST && rollout.batchCount > 0) {
    return devices.filter((device) => device.batch === 0);
  }
  return devices;
}

export function evidenceScopeLabel(rollout: Rollout): string {
  if (scopedToBatch(rollout)) return batchLabel(rollout);
  const { plural } = deviceNouns(rollout);
  if (isActive(rollout) && rollout.stage === RolloutStage.REST && rollout.batchCount > 0) return `Remaining ${plural}`;
  return `All ${plural}`;
}

export function failedDevices(devices: RolloutDevice[]): RolloutDevice[] {
  return devices.filter((device) => device.phase === RolloutDevicePhase.FAILED);
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
export function rolloutProgressSegments(counts: RolloutDeviceCounts, status?: RolloutStatus): Segment[] {
  const remaining = counts.updating + counts.retrying + counts.queued;
  return [
    { name: "Updated", status: "OK" as const, count: counts.updated },
    status === RolloutStatus.CANCELED
      ? { name: "Canceled", status: "NA" as const, count: remaining }
      : { name: "Remaining", status: "WARNING" as const, count: remaining },
    { name: "Failed", status: "CRITICAL" as const, count: counts.failed },
  ].filter((segment) => segment.count > 0);
}

export function rolloutProgressSummary(
  counts: RolloutDeviceCounts,
  pair: { manufacturer: string; model: string },
): string {
  const { singular, plural } = deviceNouns(pair);
  const noun = counts.total === 1 ? singular : plural;
  const failed = counts.failed > 0 ? `, ${counts.failed.toLocaleString()} failed` : "";
  if (counts.total > 0 && counts.updated === counts.total) {
    return `${counts.updated.toLocaleString()} ${noun} updated (100%)${failed}`;
  }
  return `${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} ${noun} updated (${counts.percent}%)${failed}`;
}

// "74 of 87 miners updated, 2 failed, Batch 5 of 6": the one-line summary for
// banners and the header pill.
export function activeUpdateSummary(rollout: Rollout): string {
  const counts = rolloutDeviceCounts(rollout);
  const failed = failedCount(rollout);
  const parts = [
    `${counts.updated.toLocaleString()} of ${rollout.deviceCount.toLocaleString()} ${deviceNouns(rollout).plural} updated`,
  ];
  if (failed > 0) parts.push(failed === 1 ? "1 failed" : `${failed.toLocaleString()} failed`);
  if (counts.skipped > 0) parts.push(`${counts.skipped.toLocaleString()} skipped`);
  if (counts.excluded > 0) parts.push(`${counts.excluded.toLocaleString()} excluded`);
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

// An unavailable upload does not clear the checksum-backed assignment. Use
// the server's availability hint rather than a separately fetched file catalog.
export const hasUnavailableAssignedFirmware = (group: ReleaseChannelModelGroup): boolean =>
  !group.rollbackPending && group.firmwareChecksum !== "" && !group.firmwareAvailable;

// Summary and model-group pages are separate reads. The group's active ID is
// authoritative; another active rollout for its pair may belong to an older scan.
export function activeRolloutForGroup(
  channelId: bigint,
  group: ReleaseChannelModelGroup,
  rolloutsById: ReadonlyMap<bigint, Rollout>,
): Rollout | undefined {
  const rollout = group.activeRolloutId > 0n ? rolloutsById.get(group.activeRolloutId) : undefined;
  return rollout &&
    !group.rollbackPending &&
    isActive(rollout) &&
    rollout.channelId === channelId &&
    rollout.assignmentGeneration === group.assignmentGeneration &&
    pairKey(rollout) === pairKey(group)
    ? rollout
    : undefined;
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
  if (group.rollbackPending) return { label: "Rollback saved; refreshing assignment", tone: "none" };
  if (hasUnavailableAssignedFirmware(group)) return { label: "Assigned firmware unavailable", tone: "attention" };
  if (activeRollout) {
    const counts = scopeCounts(activeRollout);
    const progress = `${counts.updated} of ${counts.total}`;
    if (isPaused(activeRollout)) return { label: `Paused, ${progress}`, tone: "none" };
    if (counts.failed > 0) {
      return { label: `${counts.failed} failed, ${progress} updated`, tone: "attention" };
    }
    if (needsManualReview(activeRollout)) return { label: "Review needed", tone: "attention" };
    if (activeRollout.state === RolloutState.WAITING_FOR_CONTROLLER) {
      return { label: "Waiting for controller", tone: "active" };
    }
    if (activeRollout.state === RolloutState.STABILIZING_TELEMETRY) {
      return { label: `Waiting for telemetry, ${progress}`, tone: "active" };
    }
    if (activeRollout.stage === RolloutStage.WAITING) {
      return { label: "Waiting for the next batch", tone: "active" };
    }
    return {
      label: isBatchStage(activeRollout)
        ? `${batchLabel(activeRollout)}: updating ${progress}`
        : `Updating, ${progress}`,
      tone: "active",
    };
  }
  if (group.activeRolloutId > 0n) return { label: "Refreshing update status", tone: "active" };
  if (group.firmwareVersion === "") return { label: "No firmware assigned", tone: "none" };
  if (group.minerCount === 0) return { label: `No ${deviceNouns(group).plural}`, tone: "none" };
  const onTarget = group.onTargetCount;
  const currentFinished = lastFinished?.assignmentGeneration === group.assignmentGeneration ? lastFinished : undefined;
  if (currentFinished?.status === RolloutStatus.COMPLETED_WITH_FAILURES && onTarget < group.minerCount) {
    const failed = failedCount(currentFinished);
    if (failed > 0) return { label: `${failed} failed to update`, tone: "attention" };
  }
  if (onTarget === group.minerCount) {
    const finished = currentFinished?.finishedAt ? shortDate(currentFinished.finishedAt) : "";
    return { label: finished ? `Updated ${finished}` : "Up to date", tone: "completed" };
  }
  return { label: `${onTarget} of ${group.minerCount} on target`, tone: "none" };
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

// One firmware component of a manufacturer/model pair: a model group, an
// assignment or a rollout. The empty component is the model's main firmware.
type FirmwarePair = { manufacturer: string; model: string; component?: string };

// A manufacturer/model pair as the UI names it: the observed identity with
// unknown halves left out.
export const modelLabel = (pair: { manufacturer: string; model: string }): string =>
  `${pair.manufacturer} ${pair.model}`.trim() || "Unknown model";

// A pair's firmware component as the UI names it: "Proto CT1" for its main
// firmware, "Proto CT1 PSU" for its PSU firmware.
export const pairLabel = (pair: FirmwarePair): string => {
  const component = firmwareComponentLabel(pair.component);
  return component ? `${modelLabel(pair)} ${component}` : modelLabel(pair);
};

// Proto Containers are grouped and worded apart from miners. The channel API
// reports no device class, so a container is recognized by its model pair.
export const isContainerPair = (pair: { manufacturer: string; model: string }): boolean =>
  isCT1FirmwareTarget({ targetManufacturer: pair.manufacturer, targetModel: pair.model });

/** What operators call the devices of one model pair: miners, or containers. */
export const deviceNouns = (pair: { manufacturer: string; model: string }): DeviceNouns =>
  isContainerPair(pair) ? CONTAINER_NOUNS : MINER_NOUNS;

/**
 * What operators call the devices of several model pairs at once, such as a
 * channel's or a scope preview's: "devices" only when miners and containers
 * are both among them. With no pairs to go by, the fallback applies.
 */
export function mixedDeviceNouns(
  pairs: ReadonlyArray<{ manufacturer: string; model: string }>,
  fallback: DeviceNouns = MINER_NOUNS,
): DeviceNouns {
  const containers = pairs.some(isContainerPair);
  const miners = pairs.some((pair) => !isContainerPair(pair));
  if (containers && miners) return DEVICE_NOUNS;
  if (containers) return CONTAINER_NOUNS;
  return miners ? MINER_NOUNS : fallback;
}

/** "1 miner" or "2,000 devices": a count worded with the given nouns. */
export const nounCountLabel = (nouns: DeviceNouns, count: number): string =>
  `${count.toLocaleString()} ${count === 1 ? nouns.singular : nouns.plural}`;

/** "1 miner" or "2,000 containers": a count of one model pair's devices. */
export const deviceCountLabel = (pair: { manufacturer: string; model: string }, count: number): string =>
  nounCountLabel(deviceNouns(pair), count);

export function capitalize(text: string): string {
  return `${text.charAt(0).toUpperCase()}${text.slice(1)}`;
}

/**
 * "6 miners, 2 containers": a channel's members by device type. The total is
 * the server's; containers are the members of its container model groups.
 * A model's other components list the same members, so only its main
 * firmware group counts.
 */
export function channelMembersLabel(
  total: number,
  groups: ReadonlyArray<FirmwarePair & { minerCount: number }>,
): string {
  const inContainerGroups = groups
    .filter((group) => isContainerPair(group) && !group.component)
    .reduce((sum, group) => sum + group.minerCount, 0);
  const containers = Math.min(total, inContainerGroups);
  const miners = total - containers;
  if (containers === 0) return nounCountLabel(MINER_NOUNS, miners);
  if (miners === 0) return nounCountLabel(CONTAINER_NOUNS, containers);
  return `${nounCountLabel(MINER_NOUNS, miners)}, ${nounCountLabel(CONTAINER_NOUNS, containers)}`;
}

// Device identifiers can also be names inherited from Object.prototype.
export const minerLabel = (deviceIdentifier: string, names: Readonly<Record<string, string>>): string =>
  Object.prototype.hasOwnProperty.call(names, deviceIdentifier)
    ? names[deviceIdentifier] || deviceIdentifier
    : deviceIdentifier;

// Key joining a model group to the rollouts and assignments of its pair:
// trimmed and ASCII-folded like the server, so the observed "proto Rig"
// meets the canonical "Proto Rig". Unknown identities never match anything.
// A model's controller software target and its main firmware share this key.
export const modelKey = (pair: { manufacturer: string; model: string }): string =>
  minerTargetKey(pair.manufacturer, pair.model) ?? JSON.stringify(["unknown", pair.manufacturer, pair.model]);

// Key of one firmware component of a pair. Each component has its own
// assignment, generation and rollouts, so the key keeps them apart.
export const pairKey = (pair: FirmwarePair): string =>
  pair.component ? JSON.stringify([modelKey(pair), pair.component]) : modelKey(pair);

// Test id suffix of a group's row: its model, and its component when the row
// is not for the model's main firmware.
export const groupTestId = (group: { model: string; component?: string }): string =>
  group.component ? `${group.model}-${group.component}` : group.model;

// How many hardware models the groups cover; a model's components count once.
export const modelCount = (groups: readonly { manufacturer: string; model: string }[]): number =>
  new Set(groups.map(modelKey)).size;

// Finished outcomes belong to one assignment, even when the same artifact
// is selected again later. Raw model variants still share its canonical key.
export const assignmentKey = (pair: FirmwarePair & { assignmentGeneration: bigint }): string =>
  JSON.stringify([pairKey(pair), pair.assignmentGeneration.toString()]);

export const channelAssignmentKey = (channelId: bigint, pair: ReleaseChannelModelGroup | Rollout): string =>
  `${channelId.toString()}:${assignmentKey(pair)}`;

// Completion history follows finish time, independently of creation order.
// Canceled runs do not replace an assignment's completed outcome.
export function lastFinishedByChannelAssignment(rollouts: Rollout[]): Map<string, Rollout> {
  const latest = new Map<string, Rollout>();
  for (const rollout of rollouts) {
    const finished =
      rollout.status === RolloutStatus.COMPLETED || rollout.status === RolloutStatus.COMPLETED_WITH_FAILURES;
    if (!finished || !rollout.finishedAt) continue;
    const key = channelAssignmentKey(rollout.channelId, rollout);
    const current = latest.get(key);
    if (!current?.finishedAt || timestampMs(rollout.finishedAt) > timestampMs(current.finishedAt)) {
      latest.set(key, rollout);
    }
  }
  return latest;
}

// The current assignment generation of a rollout's pair, from the polled
// channels; undefined when the channel or group is not known (yet).
export function pairGeneration(channels: ChannelView[], rollout: Rollout): bigint | undefined {
  const channel = channels.find((c) => c.id === rollout.channelId);
  const key = pairKey(rollout);
  const group = channel?.modelGroups.find((group) => pairKey(group) === key);
  return group?.rollbackPending ? undefined : group?.assignmentGeneration;
}

// Retry covers every suppressed member of the pair's assignment generation,
// including failed, skipped or canceled work in earlier rollouts. This run's
// counts cannot tell whether that set is empty; the server safely does nothing
// when it is. Active retries reuse the rollout. A finished retry starts a new
// one, so its assignment must still be current and its pair must have no active run.
export function canRetryRemaining(rollout: Rollout, channels: ChannelView[], rollouts: Rollout[]): boolean {
  if (isActive(rollout)) return true;
  const canceledRemaining =
    rollout.status === RolloutStatus.CANCELED && rollout.cancelReason === RolloutCancelReason.CANCELED_REMAINING;
  if (
    rollout.status !== RolloutStatus.COMPLETED &&
    rollout.status !== RolloutStatus.COMPLETED_WITH_FAILURES &&
    !canceledRemaining
  ) {
    return false;
  }
  const key = pairKey(rollout);
  const channel = channels.find((candidate) => candidate.id === rollout.channelId);
  const group = channel?.modelGroups.find((candidate) => pairKey(candidate) === key);
  return (
    group !== undefined &&
    !group.rollbackPending &&
    group.assignmentGeneration === rollout.assignmentGeneration &&
    group.firmwareChecksum !== "" &&
    group.firmwareChecksum === rollout.firmwareChecksum &&
    group.activeRolloutId === 0n &&
    !rollouts.some(
      (candidate) => isActive(candidate) && candidate.channelId === rollout.channelId && pairKey(candidate) === key,
    )
  );
}

// Rollback reverses a rollout's assignment lineage while the rollout is
// current: its generation is the pair's current one. An empty lineage
// clears the assignment; a nonempty one restores the previous version.
export function canRollBack(rollout: Rollout, currentGeneration: bigint | undefined): boolean {
  return currentGeneration !== undefined && rollout.assignmentGeneration === currentGeneration;
}

export function rollbackLabel(rollout: Rollout): string {
  return rollout.previousFirmwareVersion === ""
    ? "Roll back and clear firmware"
    : `Roll back to ${rollout.previousFirmwareVersion}`;
}

// "current → target" while miners converge on the assignment, or just the
// assigned version once every miner reports it.
export const modelFirmwareLabel = (group: ReleaseChannelModelGroup): string => {
  if (group.rollbackPending) return "Refreshing assignment";
  if (group.firmwareVersion === "") return "—";
  const behind = group.reportedVersions.filter((version) => version !== group.firmwareVersion);
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
