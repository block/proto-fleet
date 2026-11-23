import { type Rollout, RolloutCancelReason, RolloutState, RolloutStatus } from "./generated/rollout/v1/rollout_pb";

import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";

type RolloutAssignment = Pick<Rollout, "channelId" | "manufacturer" | "model" | "assignmentGeneration"> & {
  component?: string;
};
type AssignmentChannel = {
  id: bigint;
  modelGroups: readonly Omit<RolloutAssignment, "channelId">[];
};

// Each firmware component of a model has its own assignment and generations,
// so two rollouts concern the same assignment only when both name the same
// model and the same component.
const assignmentTargetKey = (
  assignment: Omit<RolloutAssignment, "channelId" | "assignmentGeneration">,
): string | null => {
  const key = minerTargetKey(assignment.manufacturer, assignment.model);
  return key === null ? null : JSON.stringify([key, assignment.component ?? ""]);
};

// A rollback changes the assignment generation and cancels its active run,
// which may differ from the historical rollout used to request the rollback.
export function isRollbackAcknowledged(rollout: RolloutAssignment, sources: readonly Rollout[]): boolean {
  const key = assignmentTargetKey(rollout);
  return (
    key !== null &&
    sources.some(
      (source) =>
        source.channelId === rollout.channelId &&
        source.assignmentGeneration >= rollout.assignmentGeneration &&
        assignmentTargetKey(source) === key,
    )
  );
}

export function isRolloutSuperseded(
  rollout: RolloutAssignment,
  channels: readonly AssignmentChannel[],
  observedRollouts: readonly Rollout[],
): boolean {
  const key = assignmentTargetKey(rollout);
  const channel = channels.find((channel) => channel.id === rollout.channelId);
  return (
    key !== null &&
    (channel?.modelGroups.some(
      (group) => group.assignmentGeneration > rollout.assignmentGeneration && assignmentTargetKey(group) === key,
    ) ||
      observedRollouts.some(
        (observed) =>
          observed.channelId === rollout.channelId &&
          observed.assignmentGeneration > rollout.assignmentGeneration &&
          assignmentTargetKey(observed) === key,
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
