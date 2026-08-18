import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { CreateRolloutBatchInput, CreateRolloutMemberInput } from "@/protoFleet/api/useRolloutApi";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import type {
  RolloutLane,
  RolloutLaneReleaseTarget,
  RolloutMemberState,
  RolloutRecord,
  RolloutStrategy,
} from "@/protoFleet/features/rollout/rolloutTypes";

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
  return rollout.state === "aborted" && hasUnsettledMembers(rollout);
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

export function hasActiveInitialEnforcement(lane: RolloutLane): boolean {
  const { totalCount, confirmedCount, attentionCount } = lane.initialEnforcement;
  return totalCount > confirmedCount + attentionCount;
}

export function isInitialFirmwareReady(lane: RolloutLane): boolean {
  return (
    lane.initialEnforcement.totalCount > 0 &&
    lane.initialEnforcement.confirmedCount === lane.initialEnforcement.totalCount
  );
}

export function rolloutLaneStartBlockedReason(lane: RolloutLane, rollout: RolloutRecord | undefined): string | null {
  if (lane.initialEnforcement.attentionCount > 0) {
    return "Resolve miners that need attention before starting a rollout.";
  }
  if (!isInitialFirmwareReady(lane)) {
    return "Wait for initial firmware setup to finish before starting a rollout.";
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
