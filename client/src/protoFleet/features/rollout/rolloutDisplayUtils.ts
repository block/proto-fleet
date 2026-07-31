import type {
  RolloutEvent,
  RolloutOrder,
  RolloutPhaseRollup,
  RolloutProcessType,
  RolloutStrategy,
  RolloutTargetPhase,
} from "./rolloutTypes";
import type { Segment, Status } from "@/shared/components/CompositionBar";

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

/** Map a rollout phase to a CompositionBar status color. Retrying reads as
 * warning (in-flight recovery), the same amber the reconciler's drifted→
 * redispatch state carries. */
const phaseStatus: Record<RolloutTargetPhase, Status> = {
  done: "OK",
  inProgress: "WARNING",
  retrying: "WARNING",
  queued: "NA",
  failed: "CRITICAL",
  excluded: "NA",
};

/**
 * Build CompositionBar segments from the phase rollups, in a stable visual
 * order (done → in progress → retrying → queued → failed) so the bar doesn't
 * reshuffle as counts move between phases. Excluded targets are left out of the
 * bar (they were never in scope) and surfaced separately.
 */
export function rolloutCompositionSegments(event: RolloutEvent): Segment[] {
  const order: RolloutTargetPhase[] = ["done", "inProgress", "retrying", "queued", "failed"];
  const countByPhase = new Map<RolloutTargetPhase, number>(event.rollups.map((rollup) => [rollup.phase, rollup.count]));

  return order
    .filter((phase) => (countByPhase.get(phase) ?? 0) > 0)
    .map((phase) => ({
      name: phaseLabel(event.processType, phase),
      status: phaseStatus[phase],
      count: countByPhase.get(phase) ?? 0,
    }));
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
