import { type ReactElement, type ReactNode, useEffect, useMemo, useState } from "react";

import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { inScopeTargetCount } from "@/protoFleet/features/rollout/rolloutDisplayUtils";
import RolloutPill from "@/protoFleet/features/rollout/RolloutPill";
import type { RolloutEvent, RolloutPhaseRollup } from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import { useFleetStore } from "@/protoFleet/store";

/**
 * Shared Storybook glue for the per-process "active rollout" lifecycle story
 * files (firmware update, reboot). Curtailment's `ActiveCurtailmentStatus`
 * stories showcase each lifecycle state plus one live-animated lifecycle; these
 * helpers do the same against the shipped rollout surfaces, but **in situ** —
 * inside the real app shell, with the shipped `RolloutPill` in a page header and
 * the shipped `ViewRolloutModal` opened on the rollout, so each state reads
 * where an operator actually meets it rather than as a bare card on a blank
 * canvas. Firmware and reboot share this so the two can't drift.
 *
 * Story-only: no product code lives here — it just wires fixtures and noop
 * handlers into the real components.
 */

const noop = (): void => undefined;

/**
 * The real app shell: the `NavigationMenu` sidebar (absolute, w-60) plus a
 * content column inset by that width. Seeds the fleet store with read +
 * settings permissions so the permission-gated primary nav renders (Storybook
 * has no auth session otherwise). Shared by every in-situ rollout story.
 */
export function AppShell({ children }: { children: ReactNode }): ReactElement {
  useEffect(() => {
    useFleetStore
      .getState()
      .auth.setPermissions([
        "fleet:read",
        "miner:read",
        "miner:firmware_update",
        "rack:read",
        "site:read",
        "pool:manage",
        "fleetnode:read",
        "schedule:manage",
        "curtailment:read",
        "curtailment:manage",
        "activity:read",
        "user:read",
        "apikey:manage",
        "serverlog:read",
      ]);
  }, []);
  return (
    <div className="relative min-h-screen bg-surface-base">
      <NavigationMenu items={primaryNavItems} />
      <div className="min-h-screen pl-60">{children}</div>
    </div>
  );
}

/**
 * A believable page header — the rollout's scope (its location) on the left and
 * the active rollout's `RolloutPill` on the right, exactly where the real
 * `PageHeader` shows the pill. Matches the `In Situ/In Progress` header bar
 * (h-14, px-6). The pill re-opens the detail via its shipped "View rollout"
 * action, so the state stays reachable after the modal is dismissed.
 */
function InSituPageHeader({ event, onView }: { event: RolloutEvent; onView: () => void }): ReactElement {
  return (
    <div className="flex h-14 items-center justify-between gap-4 border-b border-border-5 bg-surface-elevated-base px-6">
      <div className="min-w-0 truncate text-emphasis-300 text-text-primary">{event.scopeLabel}</div>
      <RolloutPill event={event} onViewRollout={onView} />
    </div>
  );
}

/**
 * Renders a rollout in situ: inside the app shell, with the state's pill in a
 * page header and the shipped `ViewRolloutModal` opened on the rollout — the
 * detail surface reachable from any page. Each per-state story passes a
 * different fixture, so the state also demonstrates the exact CTA set
 * `rolloutLifecycleActions` gates for it (Manage/Pause/Resume/Continue/Retry/
 * Cancel) in the modal's top bar.
 */
export function InSituRollout({ event }: { event: RolloutEvent }): ReactElement {
  const [open, setOpen] = useState(true);
  return (
    <AppShell>
      <InSituPageHeader event={event} onView={() => setOpen(true)} />
      <div className="p-8 text-300 text-text-primary-70">
        {`${event.title} — ${inScopeTargetCount(event).toLocaleString()} miners in scope.`}
      </div>
      <ViewRolloutModal
        event={open ? event : null}
        onDismiss={() => setOpen(false)}
        onManage={noop}
        onPause={noop}
        onResume={noop}
        onCancelRemaining={noop}
        onContinueFromPilot={noop}
        onRetryFailed={noop}
      />
    </AppShell>
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
 * A base rollout ticking from 0% to 100% done on a loop: it advances, holds
 * briefly on the completed state, then restarts — the process-agnostic analog
 * of curtailment's `AnimatedCurtailmentLifecycle`. `startedAt` resets each loop
 * so the card's elapsed timer counts up from zero.
 */
function useAnimatedRolloutEvent(base: RolloutEvent): RolloutEvent {
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

  return useMemo(() => buildAnimatedRolloutEvent(base, donePercent, startedAt), [base, donePercent, startedAt]);
}

/**
 * The animated lifecycle, shown in situ: the ticking rollout drives both the
 * header pill and the opened detail modal, so the whole in-product surface
 * updates as the rollout progresses.
 */
export function AnimatedInSituRollout({ base }: { base: RolloutEvent }): ReactElement {
  const event = useAnimatedRolloutEvent(base);
  return <InSituRollout event={event} />;
}
