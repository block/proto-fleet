import type {
  RolloutEvent,
  RolloutOrder,
  RolloutPhaseRollup,
  RolloutProcessType,
  RolloutStrategy,
  RolloutTargetPhase,
} from "./rolloutTypes";
import type { Segment } from "@/shared/components/CompositionBar";

export const strategyLabels: Record<RolloutStrategy, string> = {
  allAtOnce: "Update all at once",
  batched: "Update in batches",
  pilotThenContinue: "Pilot group, then continue",
};

export const orderLabels: Record<RolloutOrder, string> = {
  lowestPerformersFirst: "Lowest performers first",
  highestPerformersFirst: "Highest performers first",
  random: "Random",
};

/**
 * Helper text for each strategy, surfaced through the strategy field's info
 * popover rather than inline copy — keeps the control compact and consistent
 * with how curtailment fields carry help.
 */
export const strategyHelpText: Record<RolloutStrategy, string> = {
  allAtOnce:
    "All in-scope miners update simultaneously. Fastest, but the highest uptime impact — bounded only by the max-offline ceiling.",
  batched:
    "Miners update in fixed-size waves, pausing for the interval between each so a bounded number are ever offline at once.",
  pilotThenContinue:
    "A small pilot wave runs first, then the rollout pauses for your review before continuing to the rest in batches.",
};

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

export function pacingSummary(event: Pick<RolloutEvent, "strategy" | "batchSize" | "batchIntervalSec">): string {
  if (event.strategy === "allAtOnce") {
    return "All at once";
  }
  if (event.batchSize && event.batchIntervalSec) {
    return `Batches of ${event.batchSize.toLocaleString()} every ${event.batchIntervalSec}s`;
  }
  return strategyLabels[event.strategy];
}

/**
 * The primary lockup headline — the process's *step*, not a miner count. This
 * is what leads the active card (matching curtailment, whose lockup leads with
 * the dispatch stage / power state rather than a raw count). The miner tally is
 * demoted to a supporting stat and the progress bar.
 */
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

/** Supporting line under the stage headline — orients the step within the plan
 * (target, pacing) without leading on the raw completed count. */
export function rolloutStageDetail(event: RolloutEvent): string {
  const inScope = inScopeTargetCount(event);
  const scope = event.scopeLabel ? `${event.scopeLabel}, ` : "";
  return `${scope}${inScope.toLocaleString()} miners, ${pacingSummary(event).toLowerCase()}`;
}

/** Lifecycle-action handlers a host can wire to a rollout. Each is optional —
 * omitting one hides its control (capability-flagging). */
export interface RolloutLifecycleHandlers {
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
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const actions: RolloutLifecycleAction[] = [];

  if (handlers.onContinueFromPilot && showPilotGate) {
    actions.push({
      key: "continue",
      text: "Continue rollout",
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
  if (handlers.onCancelRemaining && !isTerminal) {
    actions.push({ key: "cancel", text: "Cancel remaining", variant: "danger", onClick: handlers.onCancelRemaining });
  }
  return actions;
}
