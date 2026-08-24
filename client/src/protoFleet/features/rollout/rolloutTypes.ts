import type { JsonObject } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

/** Shared vocabulary for API-backed and fixture-driven rollout UI. */

/** The kind of uptime-impacting process a rollout is running. */
export type RolloutProcessType = "firmware" | "reboot" | "curtailment";

/** Which half of the curtailment lifecycle the telemetry describes. */
export type CurtailmentTelemetryPhase = "dispatch" | "restore";

/** How the rollout is paced across its targets. */
export type RolloutStrategy = "allAtOnce" | "batched" | "pilotThenContinue";

/**
 * Order targets are worked through. Only meaningful for a paced run; under
 * "single batch" there is no first or last target.
 */
export type RolloutOrder = "leastEfficientFirst" | "random";

/** Fixture-only scheduling vocabulary. Production rollout wiring omits it. */
export type RolloutScheduleType = "startNow" | "scheduleForLater";

/** Lifecycle states persisted by the generic rollout service. */
export type RolloutLifecycleState =
  | "created"
  | "running"
  | "paused"
  | "review"
  | "aborted"
  | "completed"
  | "completedWithFailures"
  | "reverting"
  | "reverted"
  | "unknown";

/**
 * Fixture-only states retained by the existing Storybook framework. Production
 * adapters use RolloutLifecycleState; production policy verdicts are modeled
 * separately by RolloutBatchEvidenceSummary.
 */
export type RolloutFixtureState =
  "scheduled" | "inProgress" | "stabilizingTelemetry" | "pausedAtPilotGate" | "pausedAtBatchReview";

/** Lifecycle state accepted by shared rollout surfaces. */
export type RolloutState = RolloutLifecycleState | RolloutFixtureState;

/**
 * Per-target phase, aggregated into the composition bar + counts.
 *
 * `retrying` remains for existing Storybook fixture processes. API-backed
 * firmware rollouts map ambiguous execution to `attentionRequired`.
 */
export type RolloutTargetPhase =
  | "done"
  | "inProgress"
  | "retrying"
  | "queued"
  | "failed"
  | "attentionRequired"
  | "cancelled"
  | "reverting"
  | "reverted"
  | "excluded";

/** Model-neutral progress for one independently measured outcome. */
export interface RolloutProgress {
  completed: number;
  total: number;
  failed?: number;
  attentionRequired?: number;
}

export type RolloutMemberState =
  | "pending"
  | "admitted"
  | "succeeded"
  | "failed"
  | "attentionRequired"
  | "cancelled"
  | "reverting"
  | "reverted"
  | "unknown";

export type RolloutBatchState = "pending" | "admitted" | "completed" | "cancelled" | "unknown";

export type RolloutEvidencePhase = "baseline" | "post" | "unknown";

export type RolloutEvidenceStatus =
  | "pending"
  | "collecting"
  | "unavailable"
  | "observing"
  | "healthy"
  | "held"
  | "stale"
  | "automationError"
  | "finalized"
  | "cancelled"
  | "unknown";

export interface RolloutHashratePolicy {
  maxDropBasisPoints: number;
  healthyDurationSeconds: number;
}

export interface RolloutBatchEvidenceSummary {
  status: RolloutEvidenceStatus;
  totalCount: bigint;
  pairedCount: bigint;
  cumulativeBaselineHashrateHs?: number;
  cumulativeCurrentHashrateHs?: number;
  cumulativeDeltaBasisPoints?: number;
  latestPolicyBucketHashrateHs?: number;
  latestPolicyBucketDeltaBasisPoints?: number;
  healthySince?: string;
  lastPolicyBucketBoundary?: string;
  evaluatedAt?: string;
  postWindowFinalized: boolean;
  postWindowFinalizedAt?: string;
  errorMessage?: string;
  cancellationReason?: string;
  cancelledAt?: string;
}

export interface RolloutEvidence {
  id: bigint;
  phase: RolloutEvidencePhase;
  windowStart?: string;
  windowEnd?: string;
  observedAt?: string;
  avgHashrateHs?: number;
  avgPowerW?: number;
  avgTemperatureC?: number;
  errorCount?: bigint;
  sampleCount?: bigint;
}

export interface RolloutMember {
  id: bigint;
  batchId: bigint;
  deviceIdentifier: string;
  position: number;
  state: RolloutMemberState;
  revision: bigint;
  sourceSnapshot?: JsonObject;
  targetSnapshot?: JsonObject;
  revertSnapshot?: JsonObject;
  enforcementId?: bigint;
  commandBatchUuid?: string;
  lastError?: string;
  admittedAt?: string;
  settledAt?: string;
  evidence: RolloutEvidence[];
  modelIdentityKey?: string;
  modelIdentityValidatedAt?: string;
}

export interface RolloutBatch {
  id: bigint;
  position: number;
  label: string;
  state: RolloutBatchState;
  revision: bigint;
  members: RolloutMember[];
  completedAt?: string;
  evidenceSummary?: RolloutBatchEvidenceSummary;
  admissionAttempt?: number;
}

export interface RolloutCause {
  id: bigint;
  memberId?: bigint;
  operation: string;
  reason: string;
  actorUserId: bigint;
  fromState: RolloutLifecycleState;
  toState: RolloutLifecycleState;
  rolloutRevision: bigint;
  createdAt?: string;
}

export interface RolloutActionEligibility {
  admit: boolean;
  continue: boolean;
  pause: boolean;
  resume: boolean;
  abort: boolean;
  revert: boolean;
  complete: boolean;
}

/** Immutable firmware target attached to the lane's current physical channel. */
export interface RolloutLaneReleaseTarget {
  firmwareFileId: string;
  targetManufacturer: string;
  targetModel: string;
  firmwareVersion: string;
  sha256: string;
}

export type InitialFirmwareMatchStatus = "matching" | "mismatched" | "unknown";

export interface RolloutLanePreviewTarget {
  firmwareFileId: string;
  manufacturer: string;
  model: string;
  firmwareVersion: string;
}

export interface RolloutLanePreviewMiner {
  deviceIdentifier: string;
  manufacturer: string;
  model: string;
  currentFirmwareVersion?: string;
  targetFirmwareVersion: string;
  targetFirmwareFileId: string;
  status: InitialFirmwareMatchStatus;
}

export interface RolloutLanePreview {
  targets: RolloutLanePreviewTarget[];
  miners: RolloutLanePreviewMiner[];
  matchingCount: number;
  mismatchedCount: number;
  unknownCount: number;
  reassignments: RolloutLaneMembershipReassignment[];
  requiresReassignmentConfirmation: boolean;
  reassignmentConfirmationToken: string;
}

export interface PreviewModelDeclaration extends RolloutLanePreview {
  laneId: string;
}

export type FirmwareTransitionState = "pending" | "updating" | "verifying" | "confirmed" | "needsAttention";

/** Model-neutral firmware movement for one miner. */
export interface FirmwareTransitionMiner {
  deviceIdentifier: string;
  manufacturer: string;
  model: string;
  latestObservedFirmwareVersion?: string;
  targetFirmwareVersion: string;
  state: FirmwareTransitionState;
  lastError?: string;
  updatedAt?: Timestamp;
}

/** Aggregate and per-miner firmware movement shared by setup and rollout flows. */
export interface FirmwareTransitionProgress {
  totalCount: number;
  pendingCount: number;
  updatingCount: number;
  verifyingCount: number;
  confirmedCount: number;
  attentionCount: number;
  members: FirmwareTransitionMiner[];
}

export type RolloutLaneFirmwareConvergenceStatus = FirmwareTransitionProgress;

/** One miner currently managed by any physical channel in a rollout lane. */
export interface RolloutLaneMembershipMember {
  deviceIdentifier: string;
  manufacturer: string;
  model: string;
  observedFirmwareVersion?: string;
  channelId: bigint;
  channelPosition: number;
  onCurrentChannel: boolean;
  pinnedReleaseVersion: string;
  enforcement?: FirmwareTransitionMiner;
}

export interface RolloutLaneMembershipReassignment {
  deviceIdentifier: string;
  sourceLaneId: string;
  sourceLaneLabel: string;
}

export interface RolloutLaneAssignment {
  deviceIdentifier: string;
  laneId: string;
  laneLabel: string;
}

export interface RolloutLaneMembershipPage {
  members: RolloutLaneMembershipMember[];
  nextPageToken: string;
  totalCount: number;
}

export interface RolloutLaneMembershipChangePreview {
  targetFirmwarePreview: RolloutLanePreview;
  reassignments: RolloutLaneMembershipReassignment[];
  removals: RolloutLaneMembershipMember[];
  requiresFirmwareConfirmation: boolean;
  requiresReassignmentConfirmation: boolean;
}

export interface RolloutLaneMembershipUpdateResult {
  lane: RolloutLane;
  transitionMembers: RolloutLaneMembershipMember[];
}

/** One physical version channel in a stable operator-facing lane. */
export interface RolloutLaneChannel {
  channelId: bigint;
  releaseSetId: bigint;
  position: number;
  rolloutId?: string;
  current: boolean;
  createdAt?: string;
}

export type RolloutLaneModelCompatibility = "compatible" | "targetUnavailable" | "unknown";

export interface RolloutLaneModelFirmwareTarget {
  releaseTargetId: bigint;
  releaseSetId: bigint;
  firmwareFileId: string;
  firmwareVersion: string;
  sha256: string;
}

export interface RolloutLaneModelChannel {
  channelId: bigint;
  position: number;
  current: boolean;
  firmwareTarget?: RolloutLaneModelFirmwareTarget;
  createdAt?: string;
}

export interface RolloutLaneModelBindingSummary {
  activeCount: number;
  historicalCount: number;
}

export interface RolloutLaneModel {
  id: string;
  modelIdentityKey: string;
  revision: bigint;
  manufacturer: string;
  model: string;
  currentChannelId: bigint;
  currentFirmwareTarget?: RolloutLaneModelFirmwareTarget;
  memberCount: number;
  memberIdentifiers?: string[];
  bindings: RolloutLaneModelBindingSummary;
  firmwareConvergence: RolloutLaneFirmwareConvergenceStatus;
  channels: RolloutLaneModelChannel[];
  compatibility: RolloutLaneModelCompatibility;
}

/**
 * Stable lane facade. Physical channels remain IDs here so production UI can
 * show release history without making channel churn the operator workflow.
 */
export interface RolloutLane {
  id: string;
  label: string;
  description: string;
  currentChannelId: bigint;
  revision: bigint;
  channels: RolloutLaneChannel[];
  memberCount: number;
  memberIdentifiers: string[];
  currentReleaseTargets: RolloutLaneReleaseTarget[];
  firmwareConvergence: RolloutLaneFirmwareConvergenceStatus;
  createdAt?: string;
  updatedAt?: string;
  models: RolloutLaneModel[];
  scalarProjectionAvailable: boolean;
  topologyEnabled: boolean;
}

export type RolloutLaneTopologyAnomalyType =
  | "nullIdentity"
  | "ambiguousTargetMatch"
  | "noTargetMatch"
  | "physicalMismatch"
  | "missingBinding"
  | "duplicateActiveBinding"
  | "unknown";

export type RolloutLaneTopologyRepairAction =
  | "confirmIdentity"
  | "selectDeclaration"
  | "repairPhysicalMembership"
  | "endStaleBinding"
  | "repairBinding"
  | "rerunBackfill"
  | "unknown";

export interface RolloutLaneTopologyAnomaly {
  id: string;
  laneId: string;
  deviceIdentifier: string;
  laneModelId?: string;
  laneModelRevision?: bigint;
  type: RolloutLaneTopologyAnomalyType;
  supportedRepairActions: RolloutLaneTopologyRepairAction[];
  details: JsonObject;
}

export interface RolloutLaneTopologyReadiness {
  enabled: boolean;
  revision: bigint;
  anomalyCount: bigint;
  activeLegacyRolloutCount: bigint;
  anomalies: RolloutLaneTopologyAnomaly[];
  updatedAt?: string;
}

/** API-backed model that does not choose a rollout admission strategy. */
export interface RolloutRecord {
  id: string;
  name: string;
  strategyKey: string;
  state: RolloutLifecycleState;
  revision: bigint;
  sourceChannelId?: bigint;
  targetChannelId?: bigint;
  sourceReleaseSetId?: bigint;
  targetReleaseSetId?: bigint;
  sourceSnapshot?: JsonObject;
  targetSnapshot?: JsonObject;
  revertSnapshot?: JsonObject;
  hashratePolicy?: RolloutHashratePolicy;
  reason: string;
  startedAt?: string;
  pausedAt?: string;
  abortedAt?: string;
  completedAt?: string;
  revertingAt?: string;
  revertedAt?: string;
  createdAt?: string;
  updatedAt?: string;
  batches: RolloutBatch[];
  members: RolloutMember[];
  causes: RolloutCause[];
  availableActions: RolloutActionEligibility;
  parentId?: string;
  laneId?: string;
  laneModelId?: string;
  modelIdentityKey?: string;
  manufacturer?: string;
  model?: string;
}

export interface RolloutGroup {
  id: string;
  laneId: string;
  name: string;
  reason: string;
  resultRevision: bigint;
  terminalOutcome: "pending" | "successful" | "reverted" | "aborted" | "completedWithFailures" | "mixed" | "unknown";
  resultReady: boolean;
  lifecycle: "active" | "terminal" | "unknown";
  activity:
    | "failedAdmission"
    | "attentionRequired"
    | "review"
    | "paused"
    | "reverting"
    | "finalizing"
    | "running"
    | "created"
    | "settled"
    | "unknown";
  needsAction: boolean;
  evidenceReadiness: "pending" | "ready" | "unknown";
  createdAt?: string;
  updatedAt?: string;
  models: RolloutGroupModelSummary[];
  children: RolloutRecord[];
}

export interface RolloutGroupModelSummary {
  laneModelId: string;
  modelIdentityKey: string;
  manufacturer: string;
  model: string;
  sourceChannelId: bigint;
  targetChannelId: bigint;
  sourceReleaseTargetId: bigint;
  targetReleaseTargetId: bigint;
  memberCount: number;
  childRolloutId?: string;
}

/**
 * Config captured by the existing Storybook framework. Its scheduling and
 * four-metric thresholds remain fixture vocabulary. Production hashrate
 * automation uses RolloutHashratePolicy and server-derived batch evidence.
 */
export interface RolloutPlanConfig {
  processType: RolloutProcessType;
  strategy: RolloutStrategy;
  order: RolloutOrder;
  /** Global ceiling. Never take more than this many targets offline at once. */
  maxConcurrentOffline: number;
  /** Multiple-batch method only. */
  batchSize?: number;
  batchIntervalSec?: number;
  /** Pilot method only: size of the first, gated batch. */
  pilotSize?: number;
  /** Multiple-batch method only: pause after every batch for operator review. */
  reviewAfterEachBatch?: boolean;
  /** When true, healthy batches continue without a manual click. */
  autoContinueOnHealthyTelemetry?: boolean;
  automationThresholds?: RolloutAutomationThresholds;
  scheduleType: RolloutScheduleType;
  /** ISO string when scheduleType is scheduleForLater. */
  scheduledStartAt?: string;
}

/** A phase rollup for the composition bar / legend. */
export interface RolloutPhaseRollup {
  phase: RolloutTargetPhase;
  count: number;
}

/** Which telemetry a perf metric reports. */
export type RolloutMetricUnit = "hashrate" | "power" | "efficiency" | "temperature";

/** How the metric delta should be formatted. */
export type RolloutMetricDeltaMode = "percent" | "absolute";

/**
 * One tracked metric for the pilot-review performance readout. Deltas are
 * colored by outcome for the metric, not by sign alone.
 */
export interface RolloutPerfMetric {
  label: string;
  unit: RolloutMetricUnit;
  deltaMode?: RolloutMetricDeltaMode;
  /** Value at rollout start, in the unit's base scale (hashrate TH, power kW,
   * efficiency J/TH, temperature °C).
   * Temperature is converted for display. */
  baseline: number;
  /** Current pilot-cohort value, same scale as `baseline`. */
  current: number;
}

/** One error string and the miners currently impacted by it. */
export interface RolloutErrorImpact {
  id: string;
  message: string;
  impactedMiners: string[];
}

/** Baseline-vs-current performance for a rollout's acted-on cohort. */
export interface RolloutPerformance {
  metrics: RolloutPerfMetric[];
}

/** Server-derived evidence for the latest completed batch represented by a rollout card. */
export interface RolloutEventEvidence extends RolloutBatchEvidenceSummary {
  batchId: bigint;
  batchLabel: string;
  policy?: RolloutHashratePolicy;
}

/** Thresholds used when Fleet can continue a reviewed batch automatically. */
export interface RolloutAutomationThresholds {
  maxHashrateDropPercent: number;
  maxEfficiencyIncreasePercent: number;
  maxTemperatureIncreaseCelsius: number;
  maxErrors: number;
}

/** A telemetry value plus its optional change from the rollout baseline. */
export interface RolloutMinerTelemetryValue {
  value: string;
  delta?: string;
}

/** One miner row in a rollout detail drill-in. */
export interface RolloutMinerRow {
  id: string;
  name: string;
  type: string;
  ipAddress: string;
  phase: RolloutTargetPhase;
  hashrate: RolloutMinerTelemetryValue;
  power: RolloutMinerTelemetryValue;
  efficiency: RolloutMinerTelemetryValue;
  temperature: RolloutMinerTelemetryValue;
}

/** The live/finished rollout an ActiveRolloutStatus card renders. */
export interface RolloutEvent {
  processType: RolloutProcessType;
  /** Curtailment only. Restoration evaluates hashrate like normal operation,
   * rather than treating a reduction as the desired outcome. */
  curtailmentTelemetryPhase?: CurtailmentTelemetryPhase;
  state: RolloutState;
  /** Human title, e.g. "Firmware update to 5.1.0". */
  title: string;
  /** Where it applies, e.g. "Building B". */
  scopeLabel: string;
  strategy: RolloutStrategy;
  order: RolloutOrder;
  totalTargets: number;
  excludedTargets: number;
  batchSize?: number;
  batchIntervalSec?: number;
  /** Pilot method only: size of the first, gated batch. */
  pilotSize?: number;
  /** Multiple-batch method only: pause after every batch for operator review. */
  reviewAfterEachBatch?: boolean;
  /** When true, healthy batches continue without a manual click. */
  autoContinueOnHealthyTelemetry?: boolean;
  currentBatch?: number;
  totalBatches?: number;
  startedAt?: string;
  scheduledStartAt?: string;
  /** Seconds remaining, for the ETA line. */
  estimatedSecondsRemaining?: number;
  /** Baseline-vs-current telemetry for the acted-on cohort. Present once a
   * rollout has captured a baseline; drives the performance strip for running
   * batches and review gates. */
  performance?: RolloutPerformance;
  /** Authoritative hashrate evidence for the most relevant completed batch. */
  evidence?: RolloutEventEvidence;
  /** Authoritative error details used by summaries and miner-level views. */
  errors?: RolloutErrorImpact[];
  /** Optional progress for strategy-defined membership changes. */
  membershipProgress?: RolloutProgress;
  /** Optional progress for firmware or health convergence. */
  convergenceProgress?: RolloutProgress;
  /** Server-derived control eligibility for API-backed rollouts. */
  availableActions?: RolloutActionEligibility;
  rollups: RolloutPhaseRollup[];
}
