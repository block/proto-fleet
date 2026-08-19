import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { CreateRolloutBatchInput, CreateRolloutMemberInput } from "@/protoFleet/api/useRolloutApi";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import type {
  FirmwareTransitionState,
  RolloutLane,
  RolloutLaneReleaseTarget,
  RolloutMemberState,
  RolloutRecord,
  RolloutStrategy,
} from "@/protoFleet/features/rollout/rolloutTypes";

export const BETWEEN_CHANNEL_STRATEGY_KEY = "between_channel";

export type TargetCompatibilityStatus = "compatible" | "missing" | "noOp";

export interface TargetCompatibilityRow {
  key: string;
  manufacturer: string;
  model: string;
  sourceVersion: string;
  targetFileId?: string;
  targetVersion?: string;
  status: TargetCompatibilityStatus;
}

export interface ManualBatchConfig {
  strategy: Extract<RolloutStrategy, "batched" | "pilotThenContinue">;
  batchSize?: number;
  pilotSize?: number;
}

interface RolloutLaneActionStatusOptions {
  canStart: boolean;
  canDelete: boolean;
  deletePermissionBlockedReason?: string;
}

const terminalMemberStates = new Set<RolloutMemberState>([
  "succeeded",
  "failed",
  "attentionRequired",
  "cancelled",
  "reverted",
]);
const terminalFailureStates = new Set<RolloutMemberState>(["failed", "attentionRequired", "cancelled"]);
const targetOrUnsettledOnSourceStates = new Set<RolloutMemberState>([
  "admitted",
  "succeeded",
  "attentionRequired",
  "reverting",
  "unknown",
]);

function hasUnsettledMembers(rollout: RolloutRecord): boolean {
  return rollout.members.some((member) => !terminalMemberStates.has(member.state));
}

function hasUnfinalizedLatestBatchEvidence(rollout: RolloutRecord): boolean {
  const latestCompletedBatch = [...rollout.batches]
    .sort((a, b) => b.position - a.position)
    .find((batch) => batch.state === "completed");
  return (
    latestCompletedBatch?.completedAt !== undefined &&
    latestCompletedBatch.evidenceSummary?.postWindowFinalized === false
  );
}

export function shouldMonitorRollout(rollout: RolloutRecord | undefined): boolean {
  if (!rollout) {
    return false;
  }
  if (
    rollout.state !== "completed" &&
    rollout.state !== "completedWithFailures" &&
    rollout.state !== "aborted" &&
    rollout.state !== "reverted"
  ) {
    return true;
  }
  if (rollout.state === "aborted") {
    return hasUnsettledMembers(rollout);
  }
  return (
    (rollout.state === "completed" || rollout.state === "completedWithFailures") &&
    hasUnfinalizedLatestBatchEvidence(rollout)
  );
}

export function canRevertRollout(rollout: RolloutRecord): boolean {
  return rollout.availableActions.revert && (rollout.state !== "aborted" || !hasUnsettledMembers(rollout));
}

function hasMemberOutsideCurrentChannel(lane: RolloutLane, rollout: RolloutRecord): boolean {
  if (rollout.members.length === 0) {
    return false;
  }
  if (lane.currentChannelId === rollout.sourceChannelId) {
    return rollout.members.some((member) => targetOrUnsettledOnSourceStates.has(member.state));
  }
  if (lane.currentChannelId === rollout.targetChannelId) {
    return rollout.members.some((member) => member.state !== "succeeded");
  }
  return true;
}

export function hasActiveFirmwareConvergence(lane: RolloutLane): boolean {
  const { totalCount, confirmedCount, attentionCount } = lane.firmwareConvergence;
  return totalCount > confirmedCount + attentionCount;
}

export function laneForRollout(lanes: RolloutLane[], rolloutId: string): RolloutLane | undefined {
  return lanes.find((lane) => lane.channels.some((channel) => channel.rolloutId === rolloutId));
}

export function firstActiveFirmwareConvergenceLane(lanes: RolloutLane[]): RolloutLane | undefined {
  // Array.find preserves the server-provided lane order as the deterministic tie-breaker.
  return lanes.find(hasActiveFirmwareConvergence);
}

export function dominantFirmwareConvergenceState(lane: RolloutLane): FirmwareTransitionState {
  const { attentionCount, verifyingCount, updatingCount, pendingCount } = lane.firmwareConvergence;
  if (attentionCount > 0) {
    return "needsAttention";
  }
  if (verifyingCount > 0) {
    return "verifying";
  }
  if (updatingCount > 0) {
    return "updating";
  }
  if (pendingCount > 0) {
    return "pending";
  }
  return "confirmed";
}

export function isFirmwareConvergenceReady(lane: RolloutLane): boolean {
  return (
    lane.firmwareConvergence.totalCount > 0 &&
    lane.firmwareConvergence.confirmedCount === lane.firmwareConvergence.totalCount
  );
}

export function rolloutLaneStartBlockedReason(lane: RolloutLane, rollout: RolloutRecord | undefined): string | null {
  if (lane.memberCount === 0) {
    return "Add miners before starting a rollout.";
  }
  if (lane.firmwareConvergence.attentionCount > 0) {
    return "Resolve miners that need attention before starting a rollout.";
  }
  if (!isFirmwareConvergenceReady(lane)) {
    return "Wait for firmware convergence to finish before starting a rollout.";
  }
  if (!rollout) {
    return null;
  }
  if (shouldMonitorRollout(rollout)) {
    return rollout.state === "aborted"
      ? "Wait for in-flight miners to settle, then revert or resolve the split before starting another rollout."
      : "Finish or abort the current rollout before starting another rollout.";
  }
  if (hasMemberOutsideCurrentChannel(lane, rollout)) {
    return "Revert or resolve miners left on a historical release before starting another rollout.";
  }
  return null;
}

export function rolloutLaneDeleteBlockedReason(lane: RolloutLane, rollout: RolloutRecord | undefined): string | null {
  if (hasActiveFirmwareConvergence(lane)) {
    return "Wait for firmware convergence to finish before deleting this lane.";
  }
  if (shouldMonitorRollout(rollout)) {
    return "Wait for rollout work to settle before deleting this lane.";
  }
  return null;
}

export function rolloutLaneMembershipBlockedReason(
  lane: RolloutLane,
  rollout: RolloutRecord | undefined,
): string | null {
  if (hasActiveFirmwareConvergence(lane)) {
    return "Wait for firmware updates to finish before changing miners.";
  }
  if (shouldMonitorRollout(rollout)) {
    return "Wait for rollout work to settle before changing miners.";
  }
  return null;
}

export function rolloutLaneActionStatus(
  lane: RolloutLane,
  rollout: RolloutRecord | undefined,
  { canStart, canDelete, deletePermissionBlockedReason }: RolloutLaneActionStatusOptions,
): string | null {
  if (!canStart && !canDelete) {
    return null;
  }
  if (hasActiveFirmwareConvergence(lane)) {
    return "Firmware convergence in progress.";
  }
  if (shouldMonitorRollout(rollout)) {
    return "Rollout in progress.";
  }
  if (canStart) {
    const startBlockedReason = rolloutLaneStartBlockedReason(lane, rollout);
    if (startBlockedReason) {
      return startBlockedReason;
    }
  }
  if (canDelete) {
    return deletePermissionBlockedReason ?? rolloutLaneDeleteBlockedReason(lane, rollout);
  }
  return null;
}

export function canCompleteWithFailures(rollout: RolloutRecord): boolean {
  return (
    rollout.state === "review" &&
    rollout.availableActions.complete &&
    !rollout.batches.some((batch) => batch.state === "pending") &&
    !hasUnsettledMembers(rollout) &&
    rollout.members.some((member) => terminalFailureStates.has(member.state))
  );
}

export function evaluateTargetCompatibility(
  sourceTargets: RolloutLaneReleaseTarget[],
  files: FirmwareFileInfo[],
  selectedFileByModel: Record<string, string>,
): TargetCompatibilityRow[] {
  const fileById = new Map(files.map((file) => [file.id, file]));
  return sourceTargets.map((source) => {
    const key = minerTargetKey(source.targetManufacturer, source.targetModel)!;
    const target = fileById.get(selectedFileByModel[key] ?? "");
    const targetVersion = target?.firmware_version?.trim();
    const noOp =
      target !== undefined &&
      (target.id === source.firmwareFileId ||
        (targetVersion !== undefined && targetVersion.length > 0 && targetVersion === source.firmwareVersion));
    return {
      key,
      manufacturer: source.targetManufacturer,
      model: source.targetModel,
      sourceVersion: source.firmwareVersion,
      targetFileId: target?.id,
      targetVersion,
      status: !target ? "missing" : noOp ? "noOp" : "compatible",
    };
  });
}

function members(deviceIdentifiers: string[]): CreateRolloutMemberInput[] {
  return deviceIdentifiers.map((deviceIdentifier) => ({ deviceIdentifier }));
}

export function buildManualBatches(deviceIdentifiers: string[], config: ManualBatchConfig): CreateRolloutBatchInput[] {
  if (deviceIdentifiers.length === 0) {
    return [];
  }
  if (config.strategy === "pilotThenContinue") {
    const pilotSize = Math.min(Math.max(config.pilotSize ?? 0, 0), deviceIdentifiers.length);
    if (pilotSize === 0) {
      return [];
    }
    const batches: CreateRolloutBatchInput[] = [
      { label: "Pilot", members: members(deviceIdentifiers.slice(0, pilotSize)) },
    ];
    if (pilotSize < deviceIdentifiers.length) {
      batches.push({ label: "Remaining", members: members(deviceIdentifiers.slice(pilotSize)) });
    }
    return batches;
  }

  const batchSize = Math.max(config.batchSize ?? 0, 0);
  if (batchSize === 0) {
    return [];
  }
  const batches: CreateRolloutBatchInput[] = [];
  for (let index = 0; index < deviceIdentifiers.length; index += batchSize) {
    batches.push({
      label: `Batch ${batches.length + 1}`,
      members: members(deviceIdentifiers.slice(index, index + batchSize)),
    });
  }
  return batches;
}
