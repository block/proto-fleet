import {
  type FirmwareTransitionMiner as ProtoFirmwareTransitionMiner,
  FirmwareTransitionState as ProtoFirmwareTransitionState,
  InitialFirmwareMatchStatus as ProtoInitialFirmwareMatchStatus,
  type PreviewRolloutLaneMembershipChangeResponse as ProtoMembershipChangePreview,
  type ListRolloutLaneMembersResponse as ProtoMembershipPage,
  type UpdateRolloutLaneMembershipResponse as ProtoMembershipUpdate,
  type Rollout as ProtoRollout,
  type RolloutBatch as ProtoRolloutBatch,
  type RolloutBatchEvidenceSummary as ProtoRolloutBatchEvidenceSummary,
  RolloutBatchState as ProtoRolloutBatchState,
  type RolloutCause as ProtoRolloutCause,
  type RolloutEvidence as ProtoRolloutEvidence,
  RolloutEvidencePhase as ProtoRolloutEvidencePhase,
  RolloutEvidenceStatus as ProtoRolloutEvidenceStatus,
  type RolloutHashratePolicy as ProtoRolloutHashratePolicy,
  type RolloutLane as ProtoRolloutLane,
  type RolloutLaneChannel as ProtoRolloutLaneChannel,
  type RolloutLaneMember as ProtoRolloutLaneMember,
  type RolloutLaneMembershipReassignment as ProtoRolloutLaneMembershipReassignment,
  type RolloutLanePreview as ProtoRolloutLanePreview,
  type RolloutMember as ProtoRolloutMember,
  RolloutMemberState as ProtoRolloutMemberState,
  RolloutState as ProtoRolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { timestampToIsoString } from "@/protoFleet/api/timestamps";
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
  RolloutHashratePolicy,
  RolloutLane,
  RolloutLaneChannel,
  RolloutLaneMembershipChangePreview,
  RolloutLaneMembershipMember,
  RolloutLaneMembershipPage,
  RolloutLaneMembershipUpdateResult,
  RolloutLanePreview,
  RolloutLaneReleaseTarget,
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
  preview: ProtoMembershipChangePreview,
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

export function mapRolloutLaneMembershipUpdate(result: ProtoMembershipUpdate): RolloutLaneMembershipUpdateResult {
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
    batches: rollout.batches.map(mapRolloutBatch),
    members: rollout.members.map(mapRolloutMember),
    causes: rollout.causes.map(mapRolloutCause),
    availableActions: getRolloutActionEligibility(state),
  };
}

function countMemberState(rollout: RolloutRecord, state: RolloutMemberState): number {
  return rollout.members.filter((member) => member.state === state).length;
}

function memberCountForBatch(rollout: RolloutRecord, batchId: bigint): number {
  return rollout.members.filter((member) => member.batchId === batchId).length;
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
  return [...rollout.batches].sort((a, b) => b.position - a.position).find((batch) => batch.state === "completed");
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
  const total = rollout.members.length;
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
  const batchCounts = rollout.batches.map((batch) => batch.members.length || memberCountForBatch(rollout, batch.id));
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
    autoContinueOnHealthyTelemetry: false,
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
