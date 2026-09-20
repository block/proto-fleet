import { type Rollout, RolloutCancelReason, RolloutState, RolloutStatus } from "./generated/rollout/v1/rollout_pb";

import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";

type RolloutAssignment = Pick<Rollout, "channelId" | "manufacturer" | "model" | "assignmentGeneration">;
type AssignmentChannel = {
  id: bigint;
  modelGroups: readonly Omit<RolloutAssignment, "channelId">[];
};

// A rollback changes the assignment generation and cancels its active run,
// which may differ from the historical rollout used to request the rollback.
export function isRollbackAcknowledged(rollout: RolloutAssignment, sources: readonly Rollout[]): boolean {
  const key = minerTargetKey(rollout.manufacturer, rollout.model);
  return (
    key !== null &&
    sources.some(
      (source) =>
        source.channelId === rollout.channelId &&
        source.assignmentGeneration >= rollout.assignmentGeneration &&
        minerTargetKey(source.manufacturer, source.model) === key,
    )
  );
}

export function isRolloutSuperseded(
  rollout: RolloutAssignment,
  channels: readonly AssignmentChannel[],
  observedRollouts: readonly Rollout[],
): boolean {
  const key = minerTargetKey(rollout.manufacturer, rollout.model);
  const channel = channels.find((channel) => channel.id === rollout.channelId);
  return (
    key !== null &&
    (channel?.modelGroups.some(
      (group) =>
        group.assignmentGeneration > rollout.assignmentGeneration &&
        minerTargetKey(group.manufacturer, group.model) === key,
    ) ||
      observedRollouts.some(
        (observed) =>
          observed.channelId === rollout.channelId &&
          observed.assignmentGeneration > rollout.assignmentGeneration &&
          minerTargetKey(observed.manufacturer, observed.model) === key,
      ))
  );
}

// Project cancellation established by an acknowledged rollback or a newer
// assignment observed in channel data or a returned/polled rollout.
// Real revisions, timestamps and historical terminal outcomes remain intact.
export function acknowledgeRollout(
  rollout: Rollout,
  sources: readonly Rollout[],
  channels: readonly AssignmentChannel[],
  observedRollouts: readonly Rollout[],
): Rollout {
  if (rollout.status !== RolloutStatus.ACTIVE) return rollout;
  let cancelReason: RolloutCancelReason;
  if (isRollbackAcknowledged(rollout, sources)) {
    cancelReason = RolloutCancelReason.ROLLED_BACK;
  } else {
    if (!isRolloutSuperseded(rollout, channels, observedRollouts)) return rollout;
    cancelReason = RolloutCancelReason.SUPERSEDED;
  }
  return { ...rollout, status: RolloutStatus.CANCELED, state: RolloutState.CANCELED, cancelReason };
}
