import type {
  CurtailmentTelemetryPhase,
  RolloutErrorImpact,
  RolloutEvent,
  RolloutOrder,
  RolloutPerfMetric,
  RolloutPhaseRollup,
  RolloutPlanConfig,
  RolloutProcessType,
  RolloutStrategy,
  RolloutTargetPhase,
} from "./rolloutTypes";
import { formatCurtailmentElapsedDuration as formatDuration } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { Segment } from "@/shared/components/CompositionBar";
import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF, formatEfficiency, formatHashrate, formatPowerKwOrDash } from "@/shared/utils/telemetryFormat";

export const strategyLabels: Record<RolloutStrategy, string> = {
  allAtOnce: "Single batch",
  batched: "Multiple batches",
  pilotThenContinue: "Pilot batch, then remaining",
};

/** Action-specific noun for rollout CTA labels. */
const processActionNouns: Record<RolloutProcessType, string> = {
  firmware: "update",
  reboot: "reboot",
  curtailment: "curtailment",
};

export function rolloutActionNoun(processType: RolloutProcessType): string {
  return processActionNouns[processType];
}

const processDisplayLabels: Record<RolloutProcessType, string> = {
  firmware: "Firmware update",
  reboot: "Reboot",
  curtailment: "Curtailment",
};

export function rolloutProcessLabel(processType: RolloutProcessType): string {
  return processDisplayLabels[processType];
}

const processActiveSectionLabels: Record<RolloutProcessType, string> = {
  firmware: "Active firmware update",
  reboot: "Active reboot",
  curtailment: "Active curtailment",
};

export function rolloutActiveSectionLabel(processType: RolloutProcessType): string {
  return processActiveSectionLabels[processType];
}

export function rolloutActiveHeaderDetail(event: Pick<RolloutEvent, "title" | "scopeLabel">): string {
  return event.scopeLabel ? `${event.title} (Applies to ${event.scopeLabel})` : event.title;
}

const processStatusColumnLabels: Record<RolloutProcessType, string> = {
  firmware: "Update status",
  reboot: "Reboot status",
  curtailment: "Curtailment status",
};

export function rolloutStatusColumnLabel(processType: RolloutProcessType): string {
  return processStatusColumnLabels[processType];
}

const processActiveStatusLabels: Record<RolloutProcessType, string> = {
  firmware: "Update status",
  reboot: "Reboot status",
  curtailment: "Dispatch status",
};

export function rolloutActiveStatusLabel(processType: RolloutProcessType): string {
  return processActiveStatusLabels[processType];
}

/** Lowercase present-tense verb for strategy help text. */
const processPresentVerbs: Record<RolloutProcessType, string> = {
  firmware: "update",
  reboot: "reboot",
  curtailment: "curtail",
};

export function rolloutProcessVerb(processType: RolloutProcessType): string {
  return processPresentVerbs[processType];
}

/**
 * Section title for the behavior controls, action-prefixed, "Update behavior"
 * / "Reboot behavior" / "Curtail behavior".
 */
export function rolloutBehaviorLabel(processType: RolloutProcessType): string {
  const verb = processPresentVerbs[processType];
  return `${verb.charAt(0).toUpperCase()}${verb.slice(1)} behavior`;
}

/** The primary submit CTA for a config surface. */
export function rolloutSubmitLabel(processType: RolloutProcessType, isScheduled: boolean): string {
  return `${isScheduled ? "Schedule" : "Start"} ${rolloutActionNoun(processType)}`;
}

export const orderLabels: Record<RolloutOrder, string> = {
  leastEfficientFirst: "Least efficient first",
  random: "Random",
};

/**
 * Helper text for each strategy, surfaced through the strategy field's info
 * popover rather than inline copy. The action verb changes by process.
 */
export function strategyHelpText(processType: RolloutProcessType): Record<RolloutStrategy, string> {
  const verb = rolloutProcessVerb(processType);
  return {
    allAtOnce: `All in-scope miners ${verb} as one batch. The max miners offline setting still limits active work.`,
    batched: `Miners ${verb} in multiple batches, pausing for the configured interval between each batch.`,
    pilotThenContinue:
      "A pilot batch runs first, then pauses for review before the remaining miners continue as one batch.",
  };
}

/**
 * Per-process verb for the phase labels. Curtailment "curtails" rather than
 * "updates", so the composition-bar legend and progress copy read naturally
 * for whichever process is running.
 */
const processVerbs: Record<RolloutProcessType, { done: string; active: string }> = {
  firmware: { done: "Updated", active: "Updating" },
  reboot: { done: "Rebooted", active: "Rebooting" },
  curtailment: { done: "Curtailed", active: "Curtailing" },
};

export function phaseLabel(processType: RolloutProcessType, phase: RolloutTargetPhase): string {
  const verbs = processVerbs[processType];
  switch (phase) {
    case "done":
      return verbs.done;
    case "inProgress":
      return verbs.active;
    case "retrying":
      return "Retrying";
    case "queued":
      return "Queued";
    case "failed":
      return "Failed";
    case "attentionRequired":
      return "Needs attention";
    case "cancelled":
      return "Cancelled";
    case "reverting":
      return "Reverting";
    case "reverted":
      return "Reverted";
    case "excluded":
      return "Excluded";
  }
}

/** In-flight column label per process. */
const columnActiveLabels: Record<RolloutProcessType, string> = {
  firmware: "Updating firmware",
  reboot: "Rebooting",
  curtailment: "Curtailing",
};

export function columnActiveLabel(processType: RolloutProcessType): string {
  return columnActiveLabels[processType];
}

/**
 * Map the `fleetmanagement.v1.DeviceStatus` enum (as a raw number, to avoid a
 * generated-proto import in this presentational util) onto a rollout phase:
 *   UPDATING (7) / REBOOT_REQUIRED (8) → inProgress   (mid-rollout activity)
 *   ERROR (4)                         → failed
 *   everything else                   → the caller's fallback (e.g. queued/done
 *   derived from firmware version), since a plain ONLINE miner isn't itself a
 *   rollout state.
 * Auto-retry state comes from the rollout plan's per-target rollup, not from
 * DeviceStatus.
 */
export function deviceStatusToRolloutPhase(deviceStatus: number): RolloutTargetPhase | null {
  switch (deviceStatus) {
    case 7: // DeviceStatus.UPDATING
    case 8: // DeviceStatus.REBOOT_REQUIRED
      return "inProgress";
    case 4: // DeviceStatus.ERROR
      return "failed";
    default:
      return null;
  }
}

/**
 * Simplified progress segments for the active-rollout bar + key, mirroring
 * curtailment's consolidated shape (`getCurtailProgressSegments`: Curtailed vs
 * a single Curtailing bucket). The per-target phases (in progress / retrying /
 * queued) are all "not done yet", so they collapse into one **Remaining**
 * bucket; **Done** and **Failed** stay distinct because they're the outcomes
 * that matter. Excluded targets are never in the bar. Zero-count buckets drop
 * out so the key only lists what's present.
 */
export function rolloutProgressSegments(event: RolloutEvent): Segment[] {
  const done = rolloutPhaseCount(event.rollups, "done");
  const reverted = rolloutPhaseCount(event.rollups, "reverted");
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const attentionRequired = rolloutPhaseCount(event.rollups, "attentionRequired");
  const cancelled = rolloutPhaseCount(event.rollups, "cancelled");
  const remaining =
    rolloutPhaseCount(event.rollups, "inProgress") +
    rolloutPhaseCount(event.rollups, "retrying") +
    rolloutPhaseCount(event.rollups, "queued") +
    rolloutPhaseCount(event.rollups, "reverting");

  return [
    { name: phaseLabel(event.processType, "done"), status: "OK" as const, count: done },
    { name: "Remaining", status: "WARNING" as const, count: remaining },
    { name: "Failed", status: "CRITICAL" as const, count: failed },
    { name: "Needs attention", status: "CRITICAL" as const, count: attentionRequired },
    { name: "Cancelled", status: "NA" as const, count: cancelled },
    { name: "Reverted", status: "OK" as const, count: reverted },
  ].filter((segment) => segment.count > 0);
}

export function rolloutPhaseCount(rollups: RolloutPhaseRollup[], phase: RolloutTargetPhase): number {
  return rollups.find((rollup) => rollup.phase === phase)?.count ?? 0;
}

/** Targets that will actually be acted on (total minus excluded). */
export function inScopeTargetCount(event: Pick<RolloutEvent, "totalTargets" | "excludedTargets">): number {
  return Math.max(event.totalTargets - event.excludedTargets, 0);
}

export function rolloutCompletionPhase(event: Pick<RolloutEvent, "state">): "done" | "reverted" {
  return event.state === "reverting" || event.state === "reverted" ? "reverted" : "done";
}

export function rolloutCompletedTargetCount(event: Pick<RolloutEvent, "state" | "rollups">): number {
  return rolloutPhaseCount(event.rollups, rolloutCompletionPhase(event));
}

export function rolloutCompletionPercent(event: RolloutEvent): number {
  const inScope = inScopeTargetCount(event);
  if (inScope <= 0) {
    return 0;
  }
  return Math.round((rolloutCompletedTargetCount(event) / inScope) * 100);
}

/**
 * Rough ETA for a batched plan: (batches - 1) × interval. Process-agnostic,
 * same math curtailment's plan preview uses. Returns seconds, or undefined
 * when the plan isn't paced.
 */
export function estimateRolloutSeconds(args: {
  inScopeCount: number;
  batchSize?: number;
  batchIntervalSec?: number;
}): number | undefined {
  const { inScopeCount, batchSize, batchIntervalSec } = args;
  if (!batchSize || batchSize <= 0 || !batchIntervalSec || batchIntervalSec <= 0) {
    return undefined;
  }
  const batches = Math.ceil(inScopeCount / batchSize);
  return Math.max(batches - 1, 0) * batchIntervalSec;
}

/**
 * Human-readable rollout-plan summary for the config control.
 * Returns null until the plan has enough inputs to summarize.
 */
export function rolloutPlanReadout(args: {
  inScopeCount: number;
  config: Pick<
    RolloutPlanConfig,
    | "strategy"
    | "batchSize"
    | "batchIntervalSec"
    | "pilotSize"
    | "reviewAfterEachBatch"
    | "autoContinueOnHealthyTelemetry"
  >;
}): string | null {
  const { inScopeCount, config } = args;
  if (inScopeCount <= 0) {
    return null;
  }

  if (config.strategy === "allAtOnce") {
    return `${inScopeCount.toLocaleString()} miners in a single batch`;
  }

  if (config.strategy === "pilotThenContinue") {
    const pilot = config.pilotSize ?? 0;
    if (pilot <= 0) {
      return null;
    }
    const remaining = Math.max(inScopeCount - pilot, 0);
    return `Pilot batch of ${pilot.toLocaleString()}, then ${remaining.toLocaleString()} remaining miners`;
  }

  const { batchSize, batchIntervalSec } = config;
  if (!batchSize || batchSize <= 0) {
    return null;
  }

  const batches = Math.ceil(inScopeCount / batchSize);

  const durationSec = estimateRolloutSeconds({ inScopeCount, batchSize, batchIntervalSec });
  const over = durationSec && durationSec > 0 ? ` over ~${formatDuration(durationSec)}` : "";
  const batchWord = batches === 1 ? "batch" : "batches";
  const review = config.reviewAfterEachBatch
    ? config.autoContinueOnHealthyTelemetry
      ? ", auto-continue when healthy"
      : ", review after each batch"
    : "";
  return `About ${batches.toLocaleString()} ${batchWord}${over}${review}`;
}

export function pacingSummary(
  event: Pick<
    RolloutEvent,
    | "strategy"
    | "batchSize"
    | "batchIntervalSec"
    | "pilotSize"
    | "reviewAfterEachBatch"
    | "autoContinueOnHealthyTelemetry"
  >,
): string {
  if (event.strategy === "allAtOnce") {
    return "Single batch";
  }
  const review =
    event.strategy === "batched" && event.reviewAfterEachBatch
      ? event.autoContinueOnHealthyTelemetry
        ? ", auto-continue when healthy"
        : ", review after each batch"
      : "";
  if (event.strategy === "pilotThenContinue") {
    const pilot = event.pilotSize ? `Pilot batch of ${event.pilotSize.toLocaleString()}` : "Pilot batch";
    return `${pilot}, then remaining`;
  }
  if (event.batchSize && event.batchIntervalSec) {
    return `Multiple batches, ${event.batchSize.toLocaleString()} miners every ${event.batchIntervalSec}s${review}`;
  }
  return strategyLabels[event.strategy];
}

export interface PacingDetail {
  method: string;
  value: string;
  detail?: string;
}

function reviewPacingDetail(
  event: Pick<RolloutEvent, "reviewAfterEachBatch" | "autoContinueOnHealthyTelemetry">,
): string | undefined {
  if (!event.reviewAfterEachBatch) {
    return undefined;
  }
  return event.autoContinueOnHealthyTelemetry ? "Auto-continue when healthy" : "Review after each batch";
}

export function pacingDetail(
  event: Pick<
    RolloutEvent,
    | "strategy"
    | "batchSize"
    | "batchIntervalSec"
    | "pilotSize"
    | "reviewAfterEachBatch"
    | "autoContinueOnHealthyTelemetry"
    | "totalTargets"
    | "excludedTargets"
  >,
): PacingDetail {
  if (event.strategy === "allAtOnce") {
    return {
      method: strategyLabels.allAtOnce,
      value: `${inScopeTargetCount(event).toLocaleString()} miners in one batch`,
    };
  }

  if (event.strategy === "pilotThenContinue") {
    const inScope = inScopeTargetCount(event);
    const pilotSize = event.pilotSize ?? 0;
    const remaining = Math.max(inScope - pilotSize, 0);
    return {
      method: strategyLabels.pilotThenContinue,
      value: pilotSize > 0 ? `${pilotSize.toLocaleString()} miners in pilot batch` : "Pilot batch first",
      detail:
        pilotSize > 0 ? `${remaining.toLocaleString()} remaining miners after review` : "Remaining miners after review",
    };
  }

  const detail = reviewPacingDetail(event);
  if (event.batchSize && event.batchIntervalSec) {
    return {
      method: strategyLabels.batched,
      value: `${event.batchSize.toLocaleString()} miners every ${event.batchIntervalSec}s`,
      detail,
    };
  }

  if (event.batchSize) {
    return {
      method: strategyLabels.batched,
      value: `${event.batchSize.toLocaleString()} miners per batch`,
      detail,
    };
  }

  return {
    method: strategyLabels.batched,
    value: "Batch size not set",
    detail,
  };
}

/** Primary lockup headline for the current rollout step. */
export function rolloutStageLabel(event: RolloutEvent): string {
  switch (event.state) {
    case "created":
      return "Created";
    case "scheduled":
      return "Scheduled";
    case "running":
      return "In progress";
    case "stabilizingTelemetry":
      return "Waiting for telemetry";
    case "paused":
      return "Paused";
    case "review":
      return "Review";
    case "pausedAtPilotGate":
      return "Pilot batch review";
    case "pausedAtBatchReview":
      if (event.currentBatch && event.totalBatches) {
        return `Batch ${event.currentBatch} review`;
      }
      return "Batch review";
    case "completed":
      return event.processType === "curtailment" ? "Curtailed" : "Completed";
    case "completedWithFailures":
      return "Completed with failures";
    case "aborted":
      return "Aborted";
    case "reverting":
      return "Reverting";
    case "reverted":
      return "Reverted";
    case "unknown":
      return "Status unavailable";
    case "inProgress":
      if (event.strategy === "allAtOnce") {
        return phaseLabel(event.processType, "inProgress");
      }
      if (event.strategy === "pilotThenContinue") {
        return event.currentBatch && event.currentBatch > 1 ? "Remaining batch" : "Pilot batch";
      }
      if (event.currentBatch && event.totalBatches) {
        return `Batch ${event.currentBatch} of ${event.totalBatches}`;
      }
      return "In progress";
  }
}

/** Supporting line under the stage headline. */
export function rolloutStageDetail(event: RolloutEvent): string {
  const inScope = inScopeTargetCount(event);
  const scope = event.scopeLabel ? `${event.scopeLabel}, ` : "";
  return `${scope}${inScope.toLocaleString()} miners, ${pacingSummary(event).toLowerCase()}`;
}

/** Lifecycle-action handlers a host can wire to a rollout. Missing handlers hide their controls. */
export interface RolloutLifecycleHandlers {
  /** Edit the live plan while the rollout is active. */
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onAbort?: () => void;
  onRevert?: () => void;
  /** Fixture compatibility. API-backed surfaces use onAbort. */
  onCancelRemaining?: () => void;
  onContinueFromReview?: () => void;
  onRetryFailed?: () => void;
}

export interface RolloutLifecycleOptions {
  canManage?: boolean;
  canControl?: boolean;
}

/** A normalized lifecycle action descriptor, rendered either as the card's own
 * buttons or (embedded) as the host Modal's top-bar buttons. */
export interface RolloutLifecycleAction {
  key: string;
  text: string;
  variant: "primary" | "secondary" | "danger";
  onClick?: () => void;
}

function cancelRemainingActionText(processType: RolloutProcessType): string {
  return processType === "curtailment" ? "Abort curtailment" : "Cancel remaining";
}

function rolloutStateEligibility(event: RolloutEvent) {
  if (event.availableActions) {
    return event.availableActions;
  }

  const state = event.state;
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

/**
 * Single source of truth for which lifecycle controls a rollout shows, given
 * its state + the available handlers. Ordering matches the card's top-right
 * group (primary/continue first, destructive last). Both `ActiveRolloutStatus`
 * and `ViewRolloutModal` consume this so the two never drift.
 */
export function rolloutLifecycleActions(
  event: RolloutEvent,
  handlers: RolloutLifecycleHandlers,
  options: RolloutLifecycleOptions = {},
): RolloutLifecycleAction[] {
  const eligibility = rolloutStateEligibility(event);
  const isTerminal =
    event.state === "aborted" ||
    event.state === "completed" ||
    event.state === "completedWithFailures" ||
    event.state === "reverted";
  const isScheduled = event.state === "scheduled";
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const attentionRequired = rolloutPhaseCount(event.rollups, "attentionRequired");
  const actions: RolloutLifecycleAction[] = [];
  const canManage = options.canManage ?? true;
  const canControl = options.canControl ?? true;

  // "Manage" edits the live plan and is available until the rollout is terminal.
  if (canManage && handlers.onManage && !isTerminal && !isScheduled) {
    actions.push({ key: "manage", text: "Manage", variant: "secondary", onClick: handlers.onManage });
  }
  if (canControl && handlers.onContinueFromReview && eligibility.continue) {
    actions.push({
      key: "continue",
      text: "Continue",
      variant: "primary",
      onClick: handlers.onContinueFromReview,
    });
  }
  if (canControl && handlers.onResume && eligibility.resume) {
    actions.push({ key: "resume", text: "Resume", variant: "primary", onClick: handlers.onResume });
  }
  if (
    canControl &&
    handlers.onRetryFailed &&
    failed > 0 &&
    attentionRequired === 0 &&
    (event.state === "completed" || event.state === "completedWithFailures")
  ) {
    actions.push({ key: "retry", text: "Retry failed", variant: "secondary", onClick: handlers.onRetryFailed });
  }
  if (canControl && handlers.onPause && eligibility.pause) {
    actions.push({ key: "pause", text: "Pause", variant: "secondary", onClick: handlers.onPause });
  }
  if (canControl && handlers.onAbort && eligibility.abort) {
    actions.push({
      key: "abort",
      text: "Abort rollout",
      variant: "danger",
      onClick: handlers.onAbort,
    });
  } else if (canControl && handlers.onCancelRemaining && eligibility.abort && !isScheduled) {
    actions.push({
      key: "cancel",
      text: cancelRemainingActionText(event.processType),
      variant: "danger",
      onClick: handlers.onCancelRemaining,
    });
  }
  if (canControl && handlers.onRevert && eligibility.revert) {
    actions.push({
      key: "revert",
      text: "Revert",
      variant: "danger",
      onClick: handlers.onRevert,
    });
  }
  return actions;
}

// ---- Performance vs baseline -----------------------------------------------

/** Render a raw metric value for its unit using the shared telemetry formatters. */
export function formatRolloutMetric(metric: RolloutPerfMetric, temperatureUnit: TemperatureUnit): string {
  switch (metric.unit) {
    case "hashrate":
      return formatHashrate(metric.current) ?? "N/A";
    case "power":
      return formatPowerKwOrDash(metric.current);
    case "efficiency":
      return formatEfficiency(metric.current) ?? "N/A";
    case "temperature": {
      const displayValue = temperatureUnit === "F" ? convertCtoF(metric.current) : metric.current;
      return `${getDisplayValue(displayValue)} °${temperatureUnit}`;
    }
  }
}

/** How a metric's move off baseline is colored. */
export type RolloutMetricDeltaIntent = "positive" | "negative" | "neutral";

export interface RolloutMetricDelta {
  /** Signed raw change vs baseline. */
  change: number;
  intent: RolloutMetricDeltaIntent;
  /** Signed change: a "+" prefix for a rise, a "−" for a drop ("+1.3%" /
   * "−0.4%"). No arrows or "±", just the sign, colored green/red. */
  deltaText: string;
}

function isMetricIncreasePositive(
  unit: RolloutPerfMetric["unit"],
  processType?: RolloutProcessType,
  curtailmentTelemetryPhase?: CurtailmentTelemetryPhase,
): boolean {
  const isCurtailmentDispatch = processType === "curtailment" && curtailmentTelemetryPhase !== "restore";
  return unit === "hashrate" && !isCurtailmentDispatch;
}

/** Map a raw movement to the outcome it represents for the metric. */
export function rolloutMetricDeltaIntent(
  unit: RolloutPerfMetric["unit"],
  change: number,
  processType?: RolloutProcessType,
  curtailmentTelemetryPhase?: CurtailmentTelemetryPhase,
): RolloutMetricDeltaIntent {
  if (change === 0) {
    return "neutral";
  }
  return change > 0 === isMetricIncreasePositive(unit, processType, curtailmentTelemetryPhase)
    ? "positive"
    : "negative";
}

function metricDeltaMode(metric: RolloutPerfMetric): NonNullable<RolloutPerfMetric["deltaMode"]> {
  if (metric.deltaMode) {
    return metric.deltaMode;
  }
  if (metric.unit === "temperature") {
    return "absolute";
  }
  return "percent";
}

function metricAbsoluteChange(metric: RolloutPerfMetric, temperatureUnit: TemperatureUnit): number {
  if (metric.unit === "temperature" && temperatureUnit === "F") {
    return convertCtoF(metric.current) - convertCtoF(metric.baseline);
  }
  return metric.current - metric.baseline;
}

function formatRolloutMetricDelta(metric: RolloutPerfMetric, temperatureUnit: TemperatureUnit): string {
  const rawChange = metric.current - metric.baseline;
  const movedUp = rawChange >= 0;
  const sign = movedUp ? "+" : "−";

  switch (metricDeltaMode(metric)) {
    case "absolute": {
      const absoluteChange = metricAbsoluteChange(metric, temperatureUnit);
      const unit = metric.unit === "temperature" ? ` °${temperatureUnit}` : "";
      return `${sign}${getDisplayValue(Math.abs(absoluteChange))}${unit}`;
    }
    case "percent": {
      const percent = metric.baseline === 0 ? 0 : (rawChange / metric.baseline) * 100;
      return `${sign}${Math.abs(percent).toFixed(1)}%`;
    }
  }
}

/** Compare a metric's current value to its captured baseline. */
export function rolloutMetricDelta(
  metric: RolloutPerfMetric,
  temperatureUnit: TemperatureUnit,
  processType?: RolloutProcessType,
  curtailmentTelemetryPhase?: CurtailmentTelemetryPhase,
): RolloutMetricDelta {
  const { baseline, current } = metric;
  const change = current - baseline;
  return {
    change,
    intent: rolloutMetricDeltaIntent(metric.unit, change, processType, curtailmentTelemetryPhase),
    deltaText: formatRolloutMetricDelta(metric, temperatureUnit),
  };
}

export function rolloutErrorImpactCount(errors: RolloutErrorImpact[] | undefined): number {
  return new Set(errors?.flatMap((error) => error.impactedMiners) ?? []).size;
}

export function rolloutErrorCount(errors: RolloutErrorImpact[] | undefined): number {
  return errors?.length ?? 0;
}
