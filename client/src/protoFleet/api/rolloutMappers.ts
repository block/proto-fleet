import {
  type Rollout as ProtoRollout,
  type RolloutBatch as ProtoRolloutBatch,
  RolloutBatchState as ProtoRolloutBatchState,
  type RolloutCause as ProtoRolloutCause,
  type RolloutEvidence as ProtoRolloutEvidence,
  RolloutEvidencePhase as ProtoRolloutEvidencePhase,
  type RolloutMember as ProtoRolloutMember,
  RolloutMemberState as ProtoRolloutMemberState,
  RolloutState as ProtoRolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { timestampToIsoString } from "@/protoFleet/api/timestamps";
import type {
  RolloutActionEligibility,
  RolloutBatch,
  RolloutBatchState,
  RolloutCause,
  RolloutEvidence,
  RolloutEvidencePhase,
  RolloutLifecycleState,
  RolloutMember,
  RolloutMemberState,
  RolloutRecord,
  RolloutState,
  RolloutTargetPhase,
} from "@/protoFleet/features/rollout/rolloutTypes";

export function mapRolloutState(state: ProtoRolloutState): RolloutLifecycleState {
  switch (state) {
    case ProtoRolloutState.CREATED:
      return "created";
    case ProtoRolloutState.RUNNING:
      return "running";
    case ProtoRolloutState.PAUSED:
      return "paused";
    case ProtoRolloutState.REVIEW:
      return "review";
    case ProtoRolloutState.ABORTED:
      return "aborted";
    case ProtoRolloutState.COMPLETED:
      return "completed";
    case ProtoRolloutState.COMPLETED_WITH_FAILURES:
      return "completedWithFailures";
    case ProtoRolloutState.REVERTING:
      return "reverting";
    case ProtoRolloutState.REVERTED:
      return "reverted";
    case ProtoRolloutState.UNSPECIFIED:
    default:
      return "unknown";
  }
}

export function mapRolloutStateToProto(state: RolloutLifecycleState): ProtoRolloutState {
  switch (state) {
    case "created":
      return ProtoRolloutState.CREATED;
    case "running":
      return ProtoRolloutState.RUNNING;
    case "paused":
      return ProtoRolloutState.PAUSED;
    case "review":
      return ProtoRolloutState.REVIEW;
    case "aborted":
      return ProtoRolloutState.ABORTED;
    case "completed":
      return ProtoRolloutState.COMPLETED;
    case "completedWithFailures":
      return ProtoRolloutState.COMPLETED_WITH_FAILURES;
    case "reverting":
      return ProtoRolloutState.REVERTING;
    case "reverted":
      return ProtoRolloutState.REVERTED;
    case "unknown":
      return ProtoRolloutState.UNSPECIFIED;
  }
}

export function mapRolloutMemberState(state: ProtoRolloutMemberState): RolloutMemberState {
  switch (state) {
    case ProtoRolloutMemberState.PENDING:
      return "pending";
    case ProtoRolloutMemberState.ADMITTED:
      return "admitted";
    case ProtoRolloutMemberState.SUCCEEDED:
      return "succeeded";
    case ProtoRolloutMemberState.FAILED:
      return "failed";
    case ProtoRolloutMemberState.ATTENTION_REQUIRED:
      return "attentionRequired";
    case ProtoRolloutMemberState.CANCELLED:
      return "cancelled";
    case ProtoRolloutMemberState.REVERTING:
      return "reverting";
    case ProtoRolloutMemberState.REVERTED:
      return "reverted";
    case ProtoRolloutMemberState.UNSPECIFIED:
    default:
      return "unknown";
  }
}

export function rolloutMemberStateToTargetPhase(state: RolloutMemberState): RolloutTargetPhase {
  switch (state) {
    case "pending":
    case "unknown":
      return "queued";
    case "admitted":
      return "inProgress";
    case "succeeded":
      return "done";
    case "failed":
      return "failed";
    case "attentionRequired":
      return "attentionRequired";
    case "cancelled":
      return "cancelled";
    case "reverting":
      return "reverting";
    case "reverted":
      return "reverted";
  }
}

export function mapRolloutBatchState(state: ProtoRolloutBatchState): RolloutBatchState {
  switch (state) {
    case ProtoRolloutBatchState.PENDING:
      return "pending";
    case ProtoRolloutBatchState.ADMITTED:
      return "admitted";
    case ProtoRolloutBatchState.COMPLETED:
      return "completed";
    case ProtoRolloutBatchState.CANCELLED:
      return "cancelled";
    case ProtoRolloutBatchState.UNSPECIFIED:
    default:
      return "unknown";
  }
}

export function mapRolloutEvidencePhase(phase: ProtoRolloutEvidencePhase): RolloutEvidencePhase {
  switch (phase) {
    case ProtoRolloutEvidencePhase.BASELINE:
      return "baseline";
    case ProtoRolloutEvidencePhase.POST:
      return "post";
    case ProtoRolloutEvidencePhase.UNSPECIFIED:
    default:
      return "unknown";
  }
}

export function getRolloutActionEligibility(state: RolloutState): RolloutActionEligibility {
  return {
    admit: state === "created" || state === "running" || state === "review",
    continue: state === "review" || state === "pausedAtPilotGate" || state === "pausedAtBatchReview",
    pause: state === "running" || state === "inProgress" || state === "review",
    resume: state === "paused",
    abort:
      state === "created" ||
      state === "running" ||
      state === "inProgress" ||
      state === "paused" ||
      state === "review" ||
      state === "pausedAtPilotGate" ||
      state === "pausedAtBatchReview" ||
      state === "stabilizingTelemetry",
    revert: state === "aborted" || state === "completed" || state === "completedWithFailures",
    complete: state === "running" || state === "inProgress" || state === "review" || state === "reverting",
  };
}

function mapRolloutEvidence(evidence: ProtoRolloutEvidence): RolloutEvidence {
  return {
    id: evidence.evidenceId,
    phase: mapRolloutEvidencePhase(evidence.phase),
    windowStart: timestampToIsoString(evidence.windowStart),
    windowEnd: timestampToIsoString(evidence.windowEnd),
    observedAt: timestampToIsoString(evidence.observedAt),
    avgHashrateHs: evidence.avgHashrateHs,
    avgPowerW: evidence.avgPowerW,
    avgTemperatureC: evidence.avgTemperatureC,
    errorCount: evidence.errorCount,
    sampleCount: evidence.sampleCount,
  };
}

function mapRolloutMember(member: ProtoRolloutMember): RolloutMember {
  return {
    id: member.memberId,
    batchId: member.batchId,
    deviceIdentifier: member.deviceIdentifier,
    position: member.position,
    state: mapRolloutMemberState(member.state),
    revision: member.revision,
    sourceSnapshot: member.sourceSnapshot,
    targetSnapshot: member.targetSnapshot,
    revertSnapshot: member.revertSnapshot,
    enforcementId: member.enforcementId,
    commandBatchUuid: member.commandBatchUuid,
    lastError: member.lastError,
    admittedAt: timestampToIsoString(member.admittedAt),
    settledAt: timestampToIsoString(member.settledAt),
    evidence: member.evidence.map(mapRolloutEvidence),
  };
}

function mapRolloutBatch(batch: ProtoRolloutBatch): RolloutBatch {
  return {
    id: batch.batchId,
    position: batch.position,
    label: batch.label,
    state: mapRolloutBatchState(batch.state),
    revision: batch.revision,
    members: batch.members.map(mapRolloutMember),
  };
}

function mapRolloutCause(cause: ProtoRolloutCause): RolloutCause {
  return {
    id: cause.causeId,
    memberId: cause.memberId,
    operation: cause.operation,
    reason: cause.reason,
    actorUserId: cause.actorUserId,
    fromState: mapRolloutState(cause.fromState),
    toState: mapRolloutState(cause.toState),
    rolloutRevision: cause.rolloutRevision,
    createdAt: timestampToIsoString(cause.createdAt),
  };
}

export function mapRollout(rollout: ProtoRollout): RolloutRecord {
  const state = mapRolloutState(rollout.state);
  return {
    id: rollout.rolloutId,
    name: rollout.name,
    strategyKey: rollout.strategyKey,
    state,
    revision: rollout.revision,
    sourceChannelId: rollout.sourceChannelId,
    targetChannelId: rollout.targetChannelId,
    sourceReleaseSetId: rollout.sourceReleaseSetId,
    targetReleaseSetId: rollout.targetReleaseSetId,
    sourceSnapshot: rollout.sourceSnapshot,
    targetSnapshot: rollout.targetSnapshot,
    revertSnapshot: rollout.revertSnapshot,
    reason: rollout.reason,
    startedAt: timestampToIsoString(rollout.startedAt),
    pausedAt: timestampToIsoString(rollout.pausedAt),
    abortedAt: timestampToIsoString(rollout.abortedAt),
    completedAt: timestampToIsoString(rollout.completedAt),
    revertingAt: timestampToIsoString(rollout.revertingAt),
    revertedAt: timestampToIsoString(rollout.revertedAt),
    createdAt: timestampToIsoString(rollout.createdAt),
    updatedAt: timestampToIsoString(rollout.updatedAt),
    batches: rollout.batches.map(mapRolloutBatch),
    members: rollout.members.map(mapRolloutMember),
    causes: rollout.causes.map(mapRolloutCause),
    availableActions: getRolloutActionEligibility(state),
  };
}
