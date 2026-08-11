import type {
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
  allAtOnce: "All at once",
  batched: "In batches",
  pilotThenContinue: "Pilot group, then continue",
  delegated: "External policy",
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

const processStatusColumnLabels: Record<RolloutProcessType, string> = {
  firmware: "Update status",
  reboot: "Reboot status",
  curtailment: "Curtailment status",
};

export function rolloutStatusColumnLabel(processType: RolloutProcessType): string {
  return processStatusColumnLabels[processType];
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
    allAtOnce: `All in-scope miners ${verb} simultaneously. This has the highest uptime impact and is limited by the max miners offline setting.`,
    batched: `Miners ${verb} in fixed-size waves, pausing for the interval between each so a bounded number are ever offline at once.`,
    pilotThenContinue:
      "A small pilot wave runs first, then it pauses for your review before continuing to the rest in batches.",
    delegated: `Fleet tracks the ${verb} and target state. An external policy reviews health signals and tells Fleet when to continue.`,
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
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const remaining =
    rolloutPhaseCount(event.rollups, "inProgress") +
    rolloutPhaseCount(event.rollups, "retrying") +
    rolloutPhaseCount(event.rollups, "queued");

  return [
    { name: phaseLabel(event.processType, "done"), status: "OK" as const, count: done },
    { name: "Remaining", status: "WARNING" as const, count: remaining },
    { name: "Failed", status: "CRITICAL" as const, count: failed },
  ].filter((segment) => segment.count > 0);
}

export function rolloutPhaseCount(rollups: RolloutPhaseRollup[], phase: RolloutTargetPhase): number {
  return rollups.find((rollup) => rollup.phase === phase)?.count ?? 0;
}

/** Targets that will actually be acted on (total minus excluded). */
export function inScopeTargetCount(event: RolloutEvent): number {
  return Math.max(event.totalTargets - event.excludedTargets, 0);
}

export function rolloutCompletionPercent(event: RolloutEvent): number {
  const inScope = inScopeTargetCount(event);
  if (inScope <= 0) {
    return 0;
  }
  const done = rolloutPhaseCount(event.rollups, "done");
  return Math.round((done / inScope) * 100);
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
    "strategy" | "batchSize" | "batchIntervalSec" | "pilotSize" | "reviewAfterEachBatch" | "delegatedPolicyName"
  >;
}): string | null {
  const { inScopeCount, config } = args;
  if (inScopeCount <= 0) {
    return null;
  }

  if (config.strategy === "allAtOnce") {
    return `All ${inScopeCount.toLocaleString()} miners at once`;
  }

  const { batchSize, batchIntervalSec } = config;
  if (!batchSize || batchSize <= 0) {
    return null;
  }

  // Pilot runs first as its own gated wave; the rest continue in batches.
  const pilot = config.strategy === "pilotThenContinue" ? (config.pilotSize ?? 0) : 0;
  const remaining = Math.max(inScopeCount - pilot, 0);
  const batches = Math.ceil(remaining / batchSize);

  const durationSec = estimateRolloutSeconds({ inScopeCount: remaining, batchSize, batchIntervalSec });
  const over = durationSec && durationSec > 0 ? ` over ~${formatDuration(durationSec)}` : "";
  const batchWord = batches === 1 ? "batch" : "batches";
  const review =
    config.strategy === "delegated"
      ? `, gated by ${config.delegatedPolicyName || "external policy"}`
      : config.reviewAfterEachBatch
        ? ", review after each batch"
        : "";
  const batchPhrase = `≈ ${batches.toLocaleString()} ${batchWord}${over}${review}`;

  if (pilot > 0) {
    return `Pilot of ${pilot.toLocaleString()}, then ${batchPhrase}`;
  }
  return batchPhrase;
}

export function pacingSummary(
  event: Pick<RolloutEvent, "strategy" | "batchSize" | "batchIntervalSec" | "delegatedPolicyName">,
): string {
  if (event.strategy === "allAtOnce") {
    return "All at once";
  }
  if (event.strategy === "delegated" && event.batchSize && event.batchIntervalSec) {
    return `External policy, batches of ${event.batchSize.toLocaleString()} every ${event.batchIntervalSec}s`;
  }
  if (event.batchSize && event.batchIntervalSec) {
    return `Batches of ${event.batchSize.toLocaleString()} every ${event.batchIntervalSec}s`;
  }
  return strategyLabels[event.strategy];
}

/** Primary lockup headline for the current rollout step. */
export function rolloutStageLabel(event: RolloutEvent): string {
  switch (event.state) {
    case "scheduled":
      return "Scheduled";
    case "paused":
      return "Paused";
    case "pausedAtPilotGate":
      return "Pilot review";
    case "completed":
      return "Completed";
    case "completedWithFailures":
      return "Completed with failures";
    case "inProgress":
      if (event.strategy === "allAtOnce") {
        return "Updating all at once";
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
  onCancelRemaining?: () => void;
  onContinueFromPilot?: () => void;
  onRetryFailed?: () => void;
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

/**
 * Single source of truth for which lifecycle controls a rollout shows, given
 * its state + the available handlers. Ordering matches the card's top-right
 * group (primary/continue first, destructive last). Both `ActiveRolloutStatus`
 * and `ViewRolloutModal` consume this so the two never drift.
 */
export function rolloutLifecycleActions(
  event: RolloutEvent,
  handlers: RolloutLifecycleHandlers,
): RolloutLifecycleAction[] {
  const isRunning = event.state === "inProgress";
  const isTerminal = event.state === "completed" || event.state === "completedWithFailures";
  const showPilotGate = event.state === "pausedAtPilotGate";
  const isScheduled = event.state === "scheduled";
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const actions: RolloutLifecycleAction[] = [];

  // "Manage" edits the live plan and is available until the rollout is terminal.
  if (handlers.onManage && !isTerminal && !isScheduled) {
    actions.push({ key: "manage", text: "Manage", variant: "secondary", onClick: handlers.onManage });
  }
  if (handlers.onContinueFromPilot && showPilotGate) {
    actions.push({
      key: "continue",
      text: "Continue",
      variant: "primary",
      onClick: handlers.onContinueFromPilot,
    });
  }
  if (handlers.onResume && event.state === "paused") {
    actions.push({ key: "resume", text: "Resume", variant: "primary", onClick: handlers.onResume });
  }
  if (handlers.onRetryFailed && failed > 0 && isTerminal) {
    actions.push({ key: "retry", text: "Retry failed", variant: "secondary", onClick: handlers.onRetryFailed });
  }
  if (handlers.onPause && isRunning) {
    actions.push({ key: "pause", text: "Pause", variant: "secondary", onClick: handlers.onPause });
  }
  if (handlers.onCancelRemaining && !isTerminal && !isScheduled) {
    actions.push({
      key: "cancel",
      text: cancelRemainingActionText(event.processType),
      variant: "danger",
      onClick: handlers.onCancelRemaining,
    });
  }
  return actions;
}

// ---- Performance vs baseline -----------------------------------------------

/** Render a raw metric value for its unit using the shared telemetry formatters. */
export function formatRolloutMetric(metric: RolloutPerfMetric, temperatureUnit: TemperatureUnit): string {
  switch (metric.unit) {
    case "hashrate":
      return formatHashrate(metric.current) ?? "—";
    case "power":
      return formatPowerKwOrDash(metric.current);
    case "efficiency":
      return formatEfficiency(metric.current) ?? "—";
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

function isMetricIncreasePositive(unit: RolloutPerfMetric["unit"]): boolean {
  return unit === "hashrate";
}

/** Map a raw movement to the outcome it represents for the metric. */
export function rolloutMetricDeltaIntent(unit: RolloutPerfMetric["unit"], change: number): RolloutMetricDeltaIntent {
  if (change === 0) {
    return "neutral";
  }
  return change > 0 === isMetricIncreasePositive(unit) ? "positive" : "negative";
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
export function rolloutMetricDelta(metric: RolloutPerfMetric, temperatureUnit: TemperatureUnit): RolloutMetricDelta {
  const { baseline, current } = metric;
  const change = current - baseline;
  return {
    change,
    intent: rolloutMetricDeltaIntent(metric.unit, change),
    deltaText: formatRolloutMetricDelta(metric, temperatureUnit),
  };
}

export function rolloutErrorImpactCount(errors: RolloutErrorImpact[] | undefined): number {
  return errors?.reduce((count, error) => count + error.impactedMiners.length, 0) ?? 0;
}
