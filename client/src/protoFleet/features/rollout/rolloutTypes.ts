/**
 * Shared vocabulary for the rollout framework — the process-agnostic layer
 * extracted (in spirit) from `features/energy`. These types describe what a
 * paced, plan-aware process (firmware update, reboot, curtailment, …) looks
 * like to the UI, independent of which backend process is running.
 *
 * This is presentational scaffolding: the Storybook stories drive these with
 * fixtures. Wiring to real RPCs (the reconciler-backed RolloutPlan) is a later
 * phase — see `PLANS/PROTO_FLEET_ROLLOUT_FRAMEWORK.md`.
 */

/** The kind of uptime-impacting process a rollout is running. */
export type RolloutProcessType = "firmware" | "reboot" | "curtailment";

/**
 * How the rollout is paced across its targets. Mirrors the curtailment
 * strategy vocabulary, generalized: curtailment paces in waves, this adds an
 * explicit pilot-approval gate.
 */
export type RolloutStrategy = "allAtOnce" | "batched" | "pilotThenContinue";

/** Order targets are worked through — reuses curtailment's ordering intent. */
export type RolloutOrder = "lowestPerformersFirst" | "highestPerformersFirst" | "random";

/** When the rollout begins. */
export type RolloutScheduleType = "startNow" | "scheduleForLater";

/** Lifecycle state of an in-flight or finished rollout. */
export type RolloutState =
  "scheduled" | "inProgress" | "pausedAtPilotGate" | "paused" | "completed" | "completedWithFailures";

/** Per-target phase, aggregated into the composition bar + counts. */
export type RolloutTargetPhase = "done" | "inProgress" | "queued" | "failed" | "excluded";

/** Config captured in the modal, previewed live in the summary rail. */
export interface RolloutPlanConfig {
  processType: RolloutProcessType;
  strategy: RolloutStrategy;
  order: RolloutOrder;
  /** Global ceiling — never take more than this many targets offline at once. */
  maxConcurrentOffline: number;
  /** Batched / pilot-then-continue only. */
  batchSize?: number;
  batchIntervalSec?: number;
  /** Pilot-then-continue only: size of the first, gated wave. */
  pilotSize?: number;
  scheduleType: RolloutScheduleType;
  /** ISO string when scheduleType is scheduleForLater. */
  scheduledStartAt?: string;
}

/** A phase rollup for the composition bar / legend. */
export interface RolloutPhaseRollup {
  phase: RolloutTargetPhase;
  count: number;
}

/** Grouped failure annotation, e.g. "timeout ×4". Mirrors curtailment's
 * unavailableReasonCounts. */
export interface RolloutIssueGroup {
  label: string;
  count: number;
}

/** The live/finished rollout an ActiveRolloutStatus card renders. */
export interface RolloutEvent {
  processType: RolloutProcessType;
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
  currentBatch?: number;
  totalBatches?: number;
  startedAt?: string;
  scheduledStartAt?: string;
  /** Seconds remaining, for the ETA line. */
  estimatedSecondsRemaining?: number;
  rollups: RolloutPhaseRollup[];
  issueGroups?: RolloutIssueGroup[];
}
