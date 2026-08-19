import type { FirmwareTransitionProgress, RolloutErrorImpact, RolloutEvent, RolloutPhaseRollup } from "./rolloutTypes";

interface FirmwareTransitionRolloutContext {
  scopeLabel: string;
  startedAt?: string;
}

function transitionState(progress: FirmwareTransitionProgress): RolloutEvent["state"] {
  const unsettledCount = progress.pendingCount + progress.updatingCount + progress.verifyingCount;
  if (unsettledCount > 0) {
    return "inProgress";
  }
  if (progress.attentionCount > 0) {
    return "completedWithFailures";
  }
  return "completed";
}

function transitionRollups(progress: FirmwareTransitionProgress): RolloutPhaseRollup[] {
  const rollups: RolloutPhaseRollup[] = [
    { phase: "done", count: progress.confirmedCount },
    { phase: "inProgress", count: progress.updatingCount + progress.verifyingCount },
    { phase: "queued", count: progress.pendingCount },
    { phase: "attentionRequired", count: progress.attentionCount },
  ];
  return rollups.filter((rollup) => rollup.count > 0);
}

function transitionErrors(progress: FirmwareTransitionProgress): RolloutErrorImpact[] | undefined {
  const minersByError = new Map<string, string[]>();
  progress.members.forEach((member) => {
    const message = member.lastError?.trim();
    if (!message) {
      return;
    }
    const impactedMiners = minersByError.get(message) ?? [];
    impactedMiners.push(member.deviceIdentifier);
    minersByError.set(message, impactedMiners);
  });

  if (minersByError.size === 0) {
    return undefined;
  }

  return Array.from(minersByError, ([message, impactedMiners], index) => ({
    id: `firmware-convergence-error-${index + 1}`,
    message,
    impactedMiners,
  }));
}

/** Presents backend-enforced firmware convergence through the standard rollout UI. */
export function mapFirmwareTransitionToRolloutEvent(
  progress: FirmwareTransitionProgress,
  context: FirmwareTransitionRolloutContext,
): RolloutEvent {
  return {
    processType: "firmware",
    state: transitionState(progress),
    title: "Firmware convergence",
    scopeLabel: context.scopeLabel,
    strategy: "allAtOnce",
    order: "random",
    totalTargets: progress.totalCount,
    excludedTargets: 0,
    startedAt: context.startedAt,
    errors: transitionErrors(progress),
    convergenceProgress: {
      completed: progress.confirmedCount + progress.attentionCount,
      total: progress.totalCount,
      attentionRequired: progress.attentionCount,
    },
    rollups: transitionRollups(progress),
  };
}
