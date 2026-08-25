import {
  type FirmwareTransitionMiner as ProtoFirmwareTransitionMiner,
  FirmwareTransitionState as ProtoFirmwareTransitionState,
  InitialFirmwareMatchStatus as ProtoInitialFirmwareMatchStatus,
  type PreviewRolloutLaneMembershipChangeResponse as ProtoMembershipChangePreview,
  type ListRolloutLaneMembersResponse as ProtoMembershipPage,
  type UpdateRolloutLaneMembershipResponse as ProtoMembershipUpdate,
  type PreviewRolloutLaneModelMembershipChangeResponse as ProtoModelMembershipChangePreview,
  type UpdateRolloutLaneModelMembershipResponse as ProtoModelMembershipUpdate,
  type Rollout as ProtoRollout,
  type RolloutBatch as ProtoRolloutBatch,
  type RolloutBatchEvidenceSummary as ProtoRolloutBatchEvidenceSummary,
  RolloutBatchState as ProtoRolloutBatchState,
  type RolloutBatchSummary as ProtoRolloutBatchSummary,
  type RolloutCause as ProtoRolloutCause,
  type RolloutEvidence as ProtoRolloutEvidence,
  RolloutEvidencePhase as ProtoRolloutEvidencePhase,
  RolloutEvidenceStatus as ProtoRolloutEvidenceStatus,
  type RolloutGroup as ProtoRolloutGroup,
  RolloutGroupActivity as ProtoRolloutGroupActivity,
  RolloutGroupEvidenceReadiness as ProtoRolloutGroupEvidenceReadiness,
  RolloutGroupLifecycle as ProtoRolloutGroupLifecycle,
  RolloutGroupTerminalOutcome as ProtoRolloutGroupTerminalOutcome,
  type RolloutHashratePolicy as ProtoRolloutHashratePolicy,
  type RolloutLane as ProtoRolloutLane,
  type RolloutLaneChannel as ProtoRolloutLaneChannel,
  type RolloutLaneMember as ProtoRolloutLaneMember,
  type RolloutLaneMembershipReassignment as ProtoRolloutLaneMembershipReassignment,
  type RolloutLaneModel as ProtoRolloutLaneModel,
  RolloutLaneModelCompatibility as ProtoRolloutLaneModelCompatibility,
  type RolloutLanePreview as ProtoRolloutLanePreview,
  RolloutLaneTopologyAnomalyType as ProtoRolloutLaneTopologyAnomalyType,
  type RolloutLaneTopologyReadiness as ProtoRolloutLaneTopologyReadiness,
  RolloutLaneTopologyRepairAction as ProtoRolloutLaneTopologyRepairAction,
  type RolloutMember as ProtoRolloutMember,
  RolloutMemberState as ProtoRolloutMemberState,
  RolloutState as ProtoRolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { timestampToIsoString } from "@/protoFleet/api/timestamps";
import { latestCompletedRolloutBatch } from "@/protoFleet/features/rollout/rolloutBatchSelectors";
import type {
  FirmwareTransitionMiner,
  FirmwareTransitionState,
  RolloutActionEligibility,
  RolloutBatch,
  RolloutBatchState,
  RolloutCause,
  RolloutEvent,
  RolloutEvidence,
  RolloutEvidencePhase,
  RolloutEvidenceStatus,
  RolloutGroup,
  RolloutHashratePolicy,
  RolloutLane,
  RolloutLaneChannel,
  RolloutLaneMembershipChangePreview,
  RolloutLaneMembershipMember,
  RolloutLaneMembershipPage,
  RolloutLaneMembershipUpdateResult,
  RolloutLaneModel,
  RolloutLaneModelCompatibility,
  RolloutLanePreview,
  RolloutLaneReleaseTarget,
  RolloutLaneTopologyAnomalyType,
  RolloutLaneTopologyReadiness,
  RolloutLaneTopologyRepairAction,
  RolloutLifecycleState,
  RolloutMember,
  RolloutMemberState,
  RolloutRecord,
  RolloutState,
  RolloutTargetPhase,
} from "@/protoFleet/features/rollout/rolloutTypes";

export interface MapRolloutLaneDetails {
  memberCount?: number;
  memberIdentifiers?: string[];
  releaseTargets?: RolloutLaneReleaseTarget[];
  modelMemberIdentifiers?: ReadonlyMap<string, string[]>;
}

export interface MapRolloutToEventOptions {
  laneLabel?: string;
}

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

export function mapInitialFirmwareStatus(
  status: ProtoInitialFirmwareMatchStatus,
): RolloutLanePreview["miners"][number]["status"] {
  switch (status) {
    case ProtoInitialFirmwareMatchStatus.MATCHING:
      return "matching";
    case ProtoInitialFirmwareMatchStatus.MISMATCHED:
      return "mismatched";
    case ProtoInitialFirmwareMatchStatus.UNKNOWN:
    case ProtoInitialFirmwareMatchStatus.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function mapFirmwareTransitionState(state: ProtoFirmwareTransitionState): FirmwareTransitionState {
  switch (state) {
    case ProtoFirmwareTransitionState.PENDING:
      return "pending";
    case ProtoFirmwareTransitionState.UPDATING:
      return "updating";
    case ProtoFirmwareTransitionState.VERIFYING:
      return "verifying";
    case ProtoFirmwareTransitionState.CONFIRMED:
      return "confirmed";
    case ProtoFirmwareTransitionState.NEEDS_ATTENTION:
    case ProtoFirmwareTransitionState.UNSPECIFIED:
    default:
      return "needsAttention";
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

export function mapRolloutEvidenceStatus(status: ProtoRolloutEvidenceStatus): RolloutEvidenceStatus {
  switch (status) {
    case ProtoRolloutEvidenceStatus.PENDING:
      return "pending";
    case ProtoRolloutEvidenceStatus.COLLECTING:
      return "collecting";
    case ProtoRolloutEvidenceStatus.UNAVAILABLE:
      return "unavailable";
    case ProtoRolloutEvidenceStatus.OBSERVING:
      return "observing";
    case ProtoRolloutEvidenceStatus.HEALTHY:
      return "healthy";
    case ProtoRolloutEvidenceStatus.HELD:
      return "held";
    case ProtoRolloutEvidenceStatus.STALE:
      return "stale";
    case ProtoRolloutEvidenceStatus.AUTOMATION_ERROR:
      return "automationError";
    case ProtoRolloutEvidenceStatus.FINALIZED:
      return "finalized";
    case ProtoRolloutEvidenceStatus.CANCELLED:
      return "cancelled";
    case ProtoRolloutEvidenceStatus.UNSPECIFIED:
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

function mapRolloutLaneChannel(channel: ProtoRolloutLaneChannel): RolloutLaneChannel {
  return {
    channelId: channel.channelId,
    releaseSetId: channel.releaseSetId,
    position: channel.position,
    rolloutId: channel.rolloutId,
    current: channel.current,
    createdAt: timestampToIsoString(channel.createdAt),
  };
}

function mapLaneModelCompatibility(compatibility: ProtoRolloutLaneModelCompatibility): RolloutLaneModelCompatibility {
  switch (compatibility) {
    case ProtoRolloutLaneModelCompatibility.COMPATIBLE:
      return "compatible";
    case ProtoRolloutLaneModelCompatibility.TARGET_UNAVAILABLE:
      return "targetUnavailable";
    case ProtoRolloutLaneModelCompatibility.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function mapLaneModelFirmwareTarget(target: NonNullable<ProtoRolloutLaneModel["currentFirmwareTarget"]>) {
  return {
    releaseTargetId: target.releaseTargetId,
    releaseSetId: target.releaseSetId,
    firmwareFileId: target.firmwareFileId,
    firmwareVersion: target.firmwareVersion,
    sha256: target.sha256,
  };
}

function mapRolloutLaneModel(model: ProtoRolloutLaneModel): RolloutLaneModel {
  return {
    id: model.laneModelId,
    modelIdentityKey: model.modelIdentityKey,
    revision: model.revision,
    manufacturer: model.manufacturer,
    model: model.model,
    currentChannelId: model.currentChannelId,
    currentFirmwareTarget: model.currentFirmwareTarget
      ? mapLaneModelFirmwareTarget(model.currentFirmwareTarget)
      : undefined,
    memberCount: model.memberCount,
    bindings: {
      activeCount: model.bindings?.activeCount ?? 0,
      historicalCount: model.bindings?.historicalCount ?? 0,
    },
    firmwareConvergence: {
      totalCount: model.firmwareConvergence?.totalCount ?? 0,
      pendingCount: model.firmwareConvergence?.pendingCount ?? 0,
      updatingCount: model.firmwareConvergence?.updatingCount ?? 0,
      verifyingCount: model.firmwareConvergence?.verifyingCount ?? 0,
      confirmedCount: model.firmwareConvergence?.confirmedCount ?? 0,
      attentionCount: model.firmwareConvergence?.attentionCount ?? 0,
      members: model.firmwareConvergence?.members.map(mapFirmwareTransitionMiner) ?? [],
    },
    channels: model.channels.map((channel) => ({
      channelId: channel.channelId,
      position: channel.position,
      current: channel.current,
      firmwareTarget: channel.firmwareTarget ? mapLaneModelFirmwareTarget(channel.firmwareTarget) : undefined,
      createdAt: timestampToIsoString(channel.createdAt),
    })),
    compatibility: mapLaneModelCompatibility(model.compatibility),
  };
}

export function mapRolloutLane(lane: ProtoRolloutLane, details: MapRolloutLaneDetails = {}): RolloutLane {
  const channels = lane.channels.map(mapRolloutLaneChannel);
  return {
    id: lane.laneId,
    label: lane.label,
    description: lane.description,
    currentChannelId: lane.currentChannelId,
    revision: lane.revision,
    channels,
    memberCount: details.memberCount ?? lane.memberCount,
    memberIdentifiers: [...(details.memberIdentifiers ?? [])],
    currentReleaseTargets: [...(details.releaseTargets ?? [])],
    firmwareConvergence: {
      totalCount: lane.firmwareConvergence?.totalCount ?? 0,
      pendingCount: lane.firmwareConvergence?.pendingCount ?? 0,
      updatingCount: lane.firmwareConvergence?.updatingCount ?? 0,
      verifyingCount: lane.firmwareConvergence?.verifyingCount ?? 0,
      confirmedCount: lane.firmwareConvergence?.confirmedCount ?? 0,
      attentionCount: lane.firmwareConvergence?.attentionCount ?? 0,
      members: lane.firmwareConvergence?.members.map(mapFirmwareTransitionMiner) ?? [],
    },
    createdAt: timestampToIsoString(lane.createdAt),
    updatedAt: timestampToIsoString(lane.updatedAt),
    models: lane.models.map((model) => ({
      ...mapRolloutLaneModel(model),
      memberIdentifiers: details.modelMemberIdentifiers?.get(model.laneModelId),
    })),
    scalarProjectionAvailable: lane.scalarProjectionAvailable,
    topologyEnabled: lane.topologyEnabled,
  };
}

function mapTopologyAnomalyType(type: ProtoRolloutLaneTopologyAnomalyType): RolloutLaneTopologyAnomalyType {
  switch (type) {
    case ProtoRolloutLaneTopologyAnomalyType.NULL_IDENTITY:
      return "nullIdentity";
    case ProtoRolloutLaneTopologyAnomalyType.AMBIGUOUS_TARGET_MATCH:
      return "ambiguousTargetMatch";
    case ProtoRolloutLaneTopologyAnomalyType.NO_TARGET_MATCH:
      return "noTargetMatch";
    case ProtoRolloutLaneTopologyAnomalyType.PHYSICAL_MISMATCH:
      return "physicalMismatch";
    case ProtoRolloutLaneTopologyAnomalyType.MISSING_BINDING:
      return "missingBinding";
    case ProtoRolloutLaneTopologyAnomalyType.DUPLICATE_ACTIVE_BINDING:
      return "duplicateActiveBinding";
    case ProtoRolloutLaneTopologyAnomalyType.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function mapTopologyRepairAction(action: ProtoRolloutLaneTopologyRepairAction): RolloutLaneTopologyRepairAction {
  switch (action) {
    case ProtoRolloutLaneTopologyRepairAction.CONFIRM_IDENTITY:
      return "confirmIdentity";
    case ProtoRolloutLaneTopologyRepairAction.SELECT_DECLARATION:
      return "selectDeclaration";
    case ProtoRolloutLaneTopologyRepairAction.REPAIR_PHYSICAL_MEMBERSHIP:
      return "repairPhysicalMembership";
    case ProtoRolloutLaneTopologyRepairAction.END_STALE_BINDING:
      return "endStaleBinding";
    case ProtoRolloutLaneTopologyRepairAction.REPAIR_BINDING:
      return "repairBinding";
    case ProtoRolloutLaneTopologyRepairAction.RERUN_BACKFILL:
      return "rerunBackfill";
    case ProtoRolloutLaneTopologyRepairAction.UNSPECIFIED:
    default:
      return "unknown";
  }
}

export function mapRolloutLaneTopologyReadiness(
  readiness: ProtoRolloutLaneTopologyReadiness,
): RolloutLaneTopologyReadiness {
  return {
    enabled: readiness.enabled,
    revision: readiness.revision,
    anomalyCount: readiness.anomalyCount,
    activeLegacyRolloutCount: readiness.activeLegacyRolloutCount,
    anomalies: readiness.anomalies.map((anomaly) => ({
      id: anomaly.anomalyId,
      laneId: anomaly.laneId,
      deviceIdentifier: anomaly.deviceIdentifier,
      laneModelId: anomaly.laneModelId || undefined,
      laneModelRevision: anomaly.laneModelRevision > 0n ? anomaly.laneModelRevision : undefined,
      type: mapTopologyAnomalyType(anomaly.type),
      supportedRepairActions: anomaly.supportedRepairActions.map(mapTopologyRepairAction),
      details: anomaly.details ?? {},
    })),
    nextAnomalyPageToken: readiness.nextAnomalyPageToken,
    updatedAt: timestampToIsoString(readiness.updatedAt),
  };
}

function mapFirmwareTransitionMiner(miner: ProtoFirmwareTransitionMiner): FirmwareTransitionMiner {
  return {
    deviceIdentifier: miner.deviceIdentifier,
    manufacturer: miner.manufacturer,
    model: miner.model,
    latestObservedFirmwareVersion: miner.latestObservedFirmwareVersion,
    targetFirmwareVersion: miner.targetFirmwareVersion,
    state: mapFirmwareTransitionState(miner.state),
    lastError: miner.lastError,
    updatedAt: miner.updatedAt,
  };
}

export function mapRolloutLaneMembershipMember(member: ProtoRolloutLaneMember): RolloutLaneMembershipMember {
  return {
    deviceIdentifier: member.deviceIdentifier,
    manufacturer: member.manufacturer,
    model: member.model,
    observedFirmwareVersion: member.observedFirmwareVersion,
    channelId: member.channelId,
    channelPosition: member.channelPosition,
    onCurrentChannel: member.onCurrentChannel,
    pinnedReleaseVersion: member.pinnedReleaseVersion,
    enforcement: member.enforcement ? mapFirmwareTransitionMiner(member.enforcement) : undefined,
  };
}

function mapRolloutLaneMembershipReassignment(reassignment: ProtoRolloutLaneMembershipReassignment) {
  return {
    deviceIdentifier: reassignment.deviceIdentifier,
    sourceLaneId: reassignment.sourceLaneId,
    sourceLaneLabel: reassignment.sourceLaneLabel,
  };
}

export function mapRolloutLaneMembershipPage(page: ProtoMembershipPage): RolloutLaneMembershipPage {
  return {
    members: page.members.map(mapRolloutLaneMembershipMember),
    nextPageToken: page.nextPageToken,
    totalCount: page.totalCount,
  };
}

export function mapRolloutLaneMembershipChangePreview(
  preview: ProtoMembershipChangePreview | ProtoModelMembershipChangePreview,
): RolloutLaneMembershipChangePreview {
  if (!preview.targetFirmwarePreview) {
    throw new Error("Rollout lane membership preview response is missing its target firmware preview.");
  }
  return {
    targetFirmwarePreview: mapRolloutLanePreview(preview.targetFirmwarePreview),
    reassignments: preview.reassignments.map(mapRolloutLaneMembershipReassignment),
    removals: preview.removals.map(mapRolloutLaneMembershipMember),
    requiresFirmwareConfirmation: preview.requiresFirmwareConfirmation,
    requiresReassignmentConfirmation: preview.requiresReassignmentConfirmation,
  };
}

export function mapRolloutLaneMembershipUpdate(
  result: ProtoMembershipUpdate | ProtoModelMembershipUpdate,
): RolloutLaneMembershipUpdateResult {
  if (!result.lane) {
    throw new Error("Rollout lane membership update response is missing its lane.");
  }
  return {
    lane: mapRolloutLane(result.lane),
    transitionMembers: result.transitionMembers.map(mapRolloutLaneMembershipMember),
  };
}

export function mapRolloutLanePreview(preview: ProtoRolloutLanePreview): RolloutLanePreview {
  return {
    targets: preview.targets.map((target) => ({
      firmwareFileId: target.firmwareFileId,
      manufacturer: target.manufacturer,
      model: target.model,
      firmwareVersion: target.firmwareVersion,
    })),
    miners: preview.miners.map((miner) => ({
      deviceIdentifier: miner.deviceIdentifier,
      manufacturer: miner.manufacturer,
      model: miner.model,
      currentFirmwareVersion: miner.currentFirmwareVersion,
      targetFirmwareVersion: miner.targetFirmwareVersion,
      targetFirmwareFileId: miner.targetFirmwareFileId,
      status: mapInitialFirmwareStatus(miner.status),
    })),
    matchingCount: preview.matchingCount,
    mismatchedCount: preview.mismatchedCount,
    unknownCount: preview.unknownCount,
    reassignments: preview.reassignments.map(mapRolloutLaneMembershipReassignment),
    requiresReassignmentConfirmation: preview.requiresReassignmentConfirmation,
    reassignmentConfirmationToken: preview.reassignmentConfirmationToken,
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
    modelIdentityKey: member.modelIdentityKey,
    modelIdentityValidatedAt: timestampToIsoString(member.modelIdentityValidatedAt),
  };
}

function mapRolloutHashratePolicy(policy: ProtoRolloutHashratePolicy): RolloutHashratePolicy {
  return {
    maxDropBasisPoints: policy.maxDropBasisPoints,
    healthyDurationSeconds: policy.healthyDurationSeconds,
  };
}

function mapRolloutBatchEvidenceSummary(summary: ProtoRolloutBatchEvidenceSummary): RolloutBatch["evidenceSummary"] {
  return {
    status: mapRolloutEvidenceStatus(summary.status),
    totalCount: summary.totalCount,
    pairedCount: summary.pairedCount,
    cumulativeBaselineHashrateHs: summary.cumulativeBaselineHashrateHs,
    cumulativeCurrentHashrateHs: summary.cumulativeCurrentHashrateHs,
    cumulativeDeltaBasisPoints: summary.cumulativeDeltaBasisPoints,
    latestPolicyBucketHashrateHs: summary.latestPolicyBucketHashrateHs,
    latestPolicyBucketDeltaBasisPoints: summary.latestPolicyBucketDeltaBasisPoints,
    healthySince: timestampToIsoString(summary.healthySince),
    lastPolicyBucketBoundary: timestampToIsoString(summary.lastPolicyBucketBoundary),
    evaluatedAt: timestampToIsoString(summary.evaluatedAt),
    postWindowFinalized: summary.postWindowFinalized,
    postWindowFinalizedAt: timestampToIsoString(summary.postWindowFinalizedAt),
    errorMessage: summary.errorMessage,
    cancellationReason: summary.cancellationReason,
    cancelledAt: timestampToIsoString(summary.cancelledAt),
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
    completedAt: timestampToIsoString(batch.completedAt),
    evidenceSummary: batch.evidenceSummary ? mapRolloutBatchEvidenceSummary(batch.evidenceSummary) : undefined,
    admissionAttempt: batch.admissionAttempt,
    memberCount: batch.members.length,
  };
}

function mapRolloutBatchSummary(batch: ProtoRolloutBatchSummary): RolloutBatch {
  return {
    id: batch.batchId,
    position: batch.position,
    label: batch.label,
    state: mapRolloutBatchState(batch.state),
    revision: batch.revision,
    members: [],
    completedAt: timestampToIsoString(batch.completedAt),
    evidenceSummary: batch.evidenceSummary ? mapRolloutBatchEvidenceSummary(batch.evidenceSummary) : undefined,
    admissionAttempt: batch.admissionAttempt,
    memberCount: batch.memberCount,
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
  const memberStateCounts = Object.fromEntries(
    rollout.memberStateCounts.map((entry) => [mapRolloutMemberState(entry.state), entry.count]),
  );
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
    hashratePolicy: rollout.hashratePolicy ? mapRolloutHashratePolicy(rollout.hashratePolicy) : undefined,
    reason: rollout.reason,
    startedAt: timestampToIsoString(rollout.startedAt),
    pausedAt: timestampToIsoString(rollout.pausedAt),
    abortedAt: timestampToIsoString(rollout.abortedAt),
    completedAt: timestampToIsoString(rollout.completedAt),
    revertingAt: timestampToIsoString(rollout.revertingAt),
    revertedAt: timestampToIsoString(rollout.revertedAt),
    createdAt: timestampToIsoString(rollout.createdAt),
    updatedAt: timestampToIsoString(rollout.updatedAt),
    batches:
      rollout.batchSummaries.length > 0
        ? rollout.batchSummaries.map(mapRolloutBatchSummary)
        : rollout.batches.map(mapRolloutBatch),
    members: rollout.members.map(mapRolloutMember),
    causes: rollout.causes.map(mapRolloutCause),
    availableActions: getRolloutActionEligibility(state),
    parentId: rollout.parentId,
    laneId: rollout.laneId,
    laneModelId: rollout.laneModelId,
    modelIdentityKey: rollout.modelIdentityKey,
    manufacturer: rollout.manufacturer,
    model: rollout.model,
    memberCount: rollout.memberCount || rollout.members.length,
    memberStateCounts: rollout.memberStateCounts.length > 0 ? memberStateCounts : undefined,
    summaryOnly: rollout.summaryOnly,
    failedAdmission: rollout.failedAdmission,
  };
}

export function mapRolloutGroup(group: ProtoRolloutGroup): RolloutGroup {
  return {
    id: group.parentId,
    laneId: group.laneId,
    name: group.name,
    reason: group.reason,
    resultRevision: group.resultRevision,
    terminalOutcome: mapRolloutGroupTerminalOutcome(group.terminalOutcome),
    resultReady: group.resultReady,
    lifecycle: mapRolloutGroupLifecycle(group.lifecycle),
    activity: mapRolloutGroupActivity(group.activity),
    needsAction: group.needsAction,
    evidenceReadiness: mapRolloutGroupEvidenceReadiness(group.evidenceReadiness),
    createdAt: timestampToIsoString(group.createdAt),
    updatedAt: timestampToIsoString(group.updatedAt),
    models: (group.models ?? []).map((model) => ({
      laneModelId: model.laneModelId,
      modelIdentityKey: model.modelIdentityKey,
      manufacturer: model.manufacturer,
      model: model.model,
      sourceChannelId: model.sourceChannelId,
      targetChannelId: model.targetChannelId,
      sourceReleaseTargetId: model.sourceReleaseTargetId,
      targetReleaseTargetId: model.targetReleaseTargetId,
      memberCount: model.memberCount,
      childRolloutId: model.childRolloutId,
    })),
    children: group.children.map(mapRollout),
  };
}

function mapRolloutGroupLifecycle(value: ProtoRolloutGroupLifecycle): RolloutGroup["lifecycle"] {
  switch (value) {
    case ProtoRolloutGroupLifecycle.ACTIVE:
      return "active";
    case ProtoRolloutGroupLifecycle.TERMINAL:
      return "terminal";
    case ProtoRolloutGroupLifecycle.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function mapRolloutGroupActivity(value: ProtoRolloutGroupActivity): RolloutGroup["activity"] {
  switch (value) {
    case ProtoRolloutGroupActivity.FAILED_ADMISSION:
      return "failedAdmission";
    case ProtoRolloutGroupActivity.ATTENTION_REQUIRED:
      return "attentionRequired";
    case ProtoRolloutGroupActivity.REVIEW:
      return "review";
    case ProtoRolloutGroupActivity.PAUSED:
      return "paused";
    case ProtoRolloutGroupActivity.REVERTING:
      return "reverting";
    case ProtoRolloutGroupActivity.FINALIZING:
      return "finalizing";
    case ProtoRolloutGroupActivity.RUNNING:
      return "running";
    case ProtoRolloutGroupActivity.CREATED:
      return "created";
    case ProtoRolloutGroupActivity.SETTLED:
      return "settled";
    case ProtoRolloutGroupActivity.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function mapRolloutGroupTerminalOutcome(value: ProtoRolloutGroupTerminalOutcome): RolloutGroup["terminalOutcome"] {
  switch (value) {
    case ProtoRolloutGroupTerminalOutcome.PENDING:
      return "pending";
    case ProtoRolloutGroupTerminalOutcome.SUCCESSFUL:
      return "successful";
    case ProtoRolloutGroupTerminalOutcome.REVERTED:
      return "reverted";
    case ProtoRolloutGroupTerminalOutcome.ABORTED:
      return "aborted";
    case ProtoRolloutGroupTerminalOutcome.COMPLETED_WITH_FAILURES:
      return "completedWithFailures";
    case ProtoRolloutGroupTerminalOutcome.MIXED:
      return "mixed";
    case ProtoRolloutGroupTerminalOutcome.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function mapRolloutGroupEvidenceReadiness(
  value: ProtoRolloutGroupEvidenceReadiness,
): RolloutGroup["evidenceReadiness"] {
  switch (value) {
    case ProtoRolloutGroupEvidenceReadiness.PENDING:
      return "pending";
    case ProtoRolloutGroupEvidenceReadiness.READY:
      return "ready";
    case ProtoRolloutGroupEvidenceReadiness.UNSPECIFIED:
    default:
      return "unknown";
  }
}

function countMemberState(rollout: RolloutRecord, state: RolloutMemberState): number {
  if (rollout.memberStateCounts?.[state] !== undefined) {
    return rollout.memberStateCounts[state] ?? 0;
  }
  return rollout.members.filter((member) => member.state === state).length;
}

function memberCountForBatch(rollout: RolloutRecord, batchId: bigint): number {
  const batch = rollout.batches.find((candidate) => candidate.id === batchId);
  return batch?.memberCount ?? rollout.members.filter((member) => member.batchId === batchId).length;
}

function rolloutErrors(rollout: RolloutRecord) {
  const impactedByMessage = new Map<string, string[]>();
  rollout.members.forEach((member) => {
    if (!member.lastError) {
      return;
    }
    const impacted = impactedByMessage.get(member.lastError) ?? [];
    impacted.push(member.deviceIdentifier);
    impactedByMessage.set(member.lastError, impacted);
  });
  return [...impactedByMessage.entries()].map(([message, impactedMiners]) => ({
    id: message,
    message,
    impactedMiners,
  }));
}

function latestCompletedBatch(rollout: RolloutRecord): RolloutBatch | undefined {
  if (
    rollout.state !== "running" &&
    rollout.state !== "paused" &&
    rollout.state !== "review" &&
    rollout.state !== "completed" &&
    rollout.state !== "completedWithFailures"
  ) {
    return undefined;
  }
  return latestCompletedRolloutBatch(rollout);
}

const HASHES_PER_TERAHASH = 1_000_000_000_000;

function rolloutPerformance(batch: RolloutBatch | undefined): RolloutEvent["performance"] {
  const baselineHs = batch?.evidenceSummary?.cumulativeBaselineHashrateHs;
  const currentHs = batch?.evidenceSummary?.cumulativeCurrentHashrateHs;
  if (
    baselineHs === undefined ||
    currentHs === undefined ||
    !Number.isFinite(baselineHs) ||
    !Number.isFinite(currentHs)
  ) {
    return undefined;
  }
  return {
    metrics: [
      {
        label: "Hashrate",
        unit: "hashrate",
        baseline: baselineHs / HASHES_PER_TERAHASH,
        current: currentHs / HASHES_PER_TERAHASH,
      },
    ],
  };
}

/** Adapts durable API data to the existing model-neutral rollout surfaces. */
export function mapRolloutToEvent(
  rollout: RolloutRecord,
  { laneLabel = "Rollout lane" }: MapRolloutToEventOptions = {},
): RolloutEvent {
  const succeeded = countMemberState(rollout, "succeeded");
  const failed = countMemberState(rollout, "failed");
  const attentionRequired = countMemberState(rollout, "attentionRequired");
  const reverted = countMemberState(rollout, "reverted");
  const total = rollout.memberCount ?? rollout.members.length;
  const membershipCompleted = rollout.state === "reverting" || rollout.state === "reverted" ? reverted : succeeded;
  const convergenceCompleted = succeeded + failed + attentionRequired + reverted;
  const memberStates: RolloutMemberState[] = [
    "pending",
    "admitted",
    "succeeded",
    "failed",
    "attentionRequired",
    "cancelled",
    "reverting",
    "reverted",
    "unknown",
  ];
  const rollups = memberStates
    .map((state) => ({
      phase: rolloutMemberStateToTargetPhase(state),
      count: countMemberState(rollout, state),
    }))
    .filter((rollup) => rollup.count > 0);
  const firstBatchLabel = rollout.batches[0]?.label.toLowerCase() ?? "";
  const strategy =
    rollout.batches.length <= 1
      ? ("allAtOnce" as const)
      : firstBatchLabel.includes("pilot")
        ? ("pilotThenContinue" as const)
        : ("batched" as const);
  const activeBatch =
    rollout.batches.find((batch) => batch.state === "admitted") ??
    rollout.batches.find((batch) => batch.state === "pending") ??
    rollout.batches[rollout.batches.length - 1];
  const batchCounts = rollout.batches.map(
    (batch) => (batch.memberCount ?? batch.members.length) || memberCountForBatch(rollout, batch.id),
  );
  const pilotSize = strategy === "pilotThenContinue" ? batchCounts[0] : undefined;
  const batchSize = strategy === "batched" && batchCounts.length > 0 ? Math.max(...batchCounts) : undefined;
  const evidenceBatch = latestCompletedBatch(rollout);
  const evidence = evidenceBatch?.evidenceSummary
    ? {
        ...evidenceBatch.evidenceSummary,
        batchId: evidenceBatch.id,
        batchLabel: evidenceBatch.label,
        policy: rollout.hashratePolicy,
      }
    : undefined;

  return {
    processType: "firmware",
    state: rollout.state,
    title: rollout.name,
    scopeLabel: laneLabel,
    strategy,
    order: "random",
    totalTargets: total,
    excludedTargets: 0,
    batchSize,
    pilotSize,
    reviewAfterEachBatch: rollout.batches.length > 1,
    autoContinueOnHealthyTelemetry: rollout.hashratePolicy !== undefined,
    currentBatch: activeBatch ? activeBatch.position + 1 : undefined,
    totalBatches: rollout.batches.length || undefined,
    startedAt: rollout.startedAt ?? rollout.createdAt,
    performance: rolloutPerformance(evidenceBatch),
    evidence,
    errors: rolloutErrors(rollout),
    membershipProgress: { completed: membershipCompleted, total },
    convergenceProgress: {
      completed: convergenceCompleted,
      total,
      failed,
      attentionRequired,
    },
    availableActions: rollout.availableActions,
    rollups,
  };
}
