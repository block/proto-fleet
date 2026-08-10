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

/**
 * Order targets are worked through, on the efficiency axis (curtailment's
 * ordering intent — it sheds least-efficient miners first). Only meaningful for
 * a paced run (batched / pilot); under "all at once" there is no first/last, so
 * the control is hidden. Two options:
 *   leastEfficientFirst — default; risk your worst-efficiency miners first, so
 *     a bad change hits the ones you can most afford to lose.
 *   random — a representative pilot sample, for the truest read on a
 *     heterogeneous fleet before continuing.
 */
export type RolloutOrder = "leastEfficientFirst" | "random";

/** When the rollout begins. */
export type RolloutScheduleType = "startNow" | "scheduleForLater";

/** Lifecycle state of an in-flight or finished rollout. */
export type RolloutState =
  "scheduled" | "inProgress" | "pausedAtPilotGate" | "paused" | "completed" | "completedWithFailures";

/**
 * Per-target phase, aggregated into the composition bar + counts.
 *
 * `retrying` is the direct analog of curtailment's DRIFTED state — a target
 * that failed a step and is being automatically re-dispatched by the
 * reconciler. Transient failures self-heal through this phase; a target only
 * reaches `failed` once auto-retries are exhausted.
 */
export type RolloutTargetPhase = "done" | "inProgress" | "retrying" | "queued" | "failed" | "excluded";

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

/** Which telemetry a perf metric reports, so the card can format the raw value
 * with the shared telemetry formatters rather than carrying a formatter in the
 * (eventually RPC-backed) data. Temperature is display-unit-aware (°C/°F per the
 * user's preference); the other three are single-scale. */
export type RolloutMetricUnit = "hashrate" | "power" | "efficiency" | "temperature";

/**
 * One tracked metric for the pilot-review performance readout: its value at
 * rollout start (`baseline`, captured when the rollout began) against the
 * current pilot-cohort value, so an operator can see whether the change moved
 * the fleet before continuing. Per the design review (Caleb + Rongxin). The
 * delta is colored purely by sign (a rise reads positive, a drop negative) —
 * the readout shows the direction of movement and leaves the judgement to the
 * operator, so no per-metric "good direction" is carried.
 */
export interface RolloutPerfMetric {
  label: string;
  unit: RolloutMetricUnit;
  /** Value at rollout start, in the unit's base scale (hashrate TH, power kW,
   * efficiency J/TH, temperature °C — always Celsius, converted for display). */
  baseline: number;
  /** Current pilot-cohort value, same scale as `baseline`. */
  current: number;
}

/** Baseline-vs-current performance for a rollout's acted-on cohort. Optional on
 * a `RolloutEvent` — only live/pilot states that have captured a baseline carry
 * it; states without it render no readout. */
export interface RolloutPerformance {
  metrics: RolloutPerfMetric[];
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
  /** Baseline-vs-current telemetry for the acted-on cohort. Present only once a
   * rollout has captured a baseline (in-progress / pilot review); drives the
   * "Performance vs baseline" strip. */
  performance?: RolloutPerformance;
  rollups: RolloutPhaseRollup[];
}
