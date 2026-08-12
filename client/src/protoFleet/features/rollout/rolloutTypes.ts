/**
 * Shared vocabulary for paced rollout UI.
 *
 * This is presentational scaffolding: the Storybook stories drive these with
 * fixtures. Wiring to real RPCs (the reconciler-backed RolloutPlan) is a later
 * phase. See `PLANS/PROTO_FLEET_ROLLOUT_FRAMEWORK.md`.
 */

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

/** When the rollout begins. */
export type RolloutScheduleType = "startNow" | "scheduleForLater";

/** Lifecycle state of an in-flight or finished rollout. */
export type RolloutState =
  | "scheduled"
  | "inProgress"
  | "stabilizingTelemetry"
  | "pausedAtPilotGate"
  | "pausedAtBatchReview"
  | "paused"
  | "completed"
  | "completedWithFailures";

/**
 * Per-target phase, aggregated into the composition bar + counts.
 *
 * `retrying` is a target that failed a step and is being automatically
 * re-dispatched by the reconciler. A target only reaches `failed` once
 * auto-retries are exhausted.
 */
export type RolloutTargetPhase = "done" | "inProgress" | "retrying" | "queued" | "failed" | "excluded";

/** Config captured in the modal, previewed live in the summary rail. */
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
  /** Authoritative error details used by summaries and miner-level views. */
  errors?: RolloutErrorImpact[];
  rollups: RolloutPhaseRollup[];
}
