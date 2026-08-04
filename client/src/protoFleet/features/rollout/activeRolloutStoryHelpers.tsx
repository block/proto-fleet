import { type ReactElement, useEffect, useMemo, useState } from "react";

import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import type { RolloutEvent, RolloutPhaseRollup } from "@/protoFleet/features/rollout/rolloutTypes";

/**
 * Shared Storybook glue for the per-process "active rollout" story files
 * (firmware update, reboot). Curtailment's `ActiveCurtailmentStatus.stories`
 * showcases each lifecycle state plus one live-animated lifecycle; these
 * helpers let the firmware and reboot story files do exactly the same thing
 * against the shipped `ActiveRolloutStatus` without duplicating (and drifting)
 * the animation logic between the two.
 *
 * Story-only: no product code lives here — it just wires fixtures and noop
 * handlers into the real card.
 */

const noop = (): void => undefined;

/**
 * Renders a single fixed rollout state through the real card, with the full set
 * of lifecycle handlers wired as noops. Each state then shows exactly the CTA
 * set `rolloutLifecycleActions` gates for it (Manage/Pause/Resume/Continue/
 * Retry/Cancel), so the per-state stories double as an action-set showcase.
 */
export function ActiveRolloutStatusCard({ event }: { event: RolloutEvent }): ReactElement {
  return (
    <ActiveRolloutStatus
      event={event}
      onManage={noop}
      onPause={noop}
      onResume={noop}
      onCancelRemaining={noop}
      onContinueFromPilot={noop}
      onRetryFailed={noop}
    />
  );
}

const animationStepPercent = 10;
const animationStepMs = 450;
const completedHoldMs = 2600;

/**
 * Derive an in-flight (or just-finished) rollout at a given completion percent
 * from a base in-progress fixture. Mirrors curtailment's `buildAnimatedEvent`:
 * the done count grows, one batch sits in progress, the rest stays queued, and
 * at 100% the event flips to `completed`. Excluded targets pass through
 * untouched (they're never in the bar).
 */
function buildAnimatedRolloutEvent(base: RolloutEvent, donePercent: number, startedAt: string): RolloutEvent {
  const inScope = Math.max(base.totalTargets - base.excludedTargets, 0);
  const done = Math.round((inScope * donePercent) / 100);
  const remaining = Math.max(inScope - done, 0);
  const isComplete = donePercent >= 100;
  const activeBatch = isComplete ? 0 : Math.min(base.batchSize ?? remaining, remaining);
  const queued = Math.max(remaining - activeBatch, 0);

  const rollups: RolloutPhaseRollup[] = [
    { phase: "done", count: done },
    { phase: "inProgress", count: activeBatch },
    { phase: "queued", count: queued },
  ];
  if (base.excludedTargets > 0) {
    rollups.push({ phase: "excluded", count: base.excludedTargets });
  }

  const batchSize = base.batchSize ?? inScope;
  const currentBatch =
    base.totalBatches && batchSize > 0
      ? Math.min(Math.floor(done / batchSize) + 1, base.totalBatches)
      : base.currentBatch;

  return {
    ...base,
    state: isComplete ? "completed" : "inProgress",
    startedAt,
    currentBatch,
    estimatedSecondsRemaining: isComplete ? 0 : base.estimatedSecondsRemaining,
    rollups,
  };
}

/**
 * A live, looping lifecycle: the base rollout ticks from 0% to 100% done, holds
 * briefly on the completed state, then restarts — the process-agnostic analog
 * of curtailment's `AnimatedCurtailmentLifecycle`. `startedAt` is reset each
 * loop so the card's elapsed timer counts up from zero.
 */
export function AnimatedRolloutLifecycle({ base }: { base: RolloutEvent }): ReactElement {
  const [donePercent, setDonePercent] = useState(0);
  const [startedAt, setStartedAt] = useState(() => new Date().toISOString());

  useEffect(() => {
    if (donePercent >= 100) {
      const timeoutId = window.setTimeout(() => {
        setDonePercent(0);
        setStartedAt(new Date().toISOString());
      }, completedHoldMs);
      return () => window.clearTimeout(timeoutId);
    }

    const intervalId = window.setInterval(() => {
      setDonePercent((current) => Math.min(current + animationStepPercent, 100));
    }, animationStepMs);
    return () => window.clearInterval(intervalId);
  }, [donePercent]);

  const event = useMemo(() => buildAnimatedRolloutEvent(base, donePercent, startedAt), [base, donePercent, startedAt]);

  return <ActiveRolloutStatusCard event={event} />;
}
