import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import {
  orderLabels,
  pacingSummary,
  phaseLabel,
  rolloutCompletionPercent,
  rolloutLifecycleActions,
  rolloutPhaseCount,
  rolloutProgressSegments,
  rolloutStageLabel,
} from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

/**
 * Progress-bar colors, matching curtailment's curtail-phase precedent exactly
 * (`curtailProgressColorMap` in ActiveCurtailmentStatus): a rollout is
 * "work moving forward" like a curtail dispatch, so **done** reads as the
 * primary fill (not success-green), **remaining** as the accent, **failed** as
 * critical. Passed to CompositionBar and reused for the legend dots so the bar
 * and its key never diverge.
 */
const rolloutProgressColorMap: Record<Segment["status"], string> = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-core-accent-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-primary-10",
};

interface ActiveRolloutStatusProps {
  event: RolloutEvent;
  className?: string;
  /** When true, drop the card's own elevated surface/shadow/padding — for when
   * the card is already inside an elevated container (e.g. the ViewRolloutModal). */
  embedded?: boolean;
  /** When true, suppress the card's own lifecycle button row — the host (e.g.
   * ViewRolloutModal) renders the CTAs in its top bar instead. */
  hideActions?: boolean;
  /** Lifecycle actions — each renders only when its handler is supplied, so
   * capability-flagging is just "pass the handler or don't". */
  onPause?: () => void;
  onResume?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromPilot?: () => void;
  onRetryFailed?: () => void;
}

interface StatBlockProps {
  label: string;
  value: string;
  detail?: string;
}

// Same lockup as ActiveCurtailmentStatus' StatBlock, so rollout detail reads
// consistently with curtailment detail.
function StatBlock({ label, value, detail }: StatBlockProps): ReactElement {
  return (
    <div className="min-w-0">
      <div className="text-200 text-text-primary-50">{label}</div>
      <div className="mt-1 truncate text-emphasis-300 text-text-primary" title={value}>
        {value}
      </div>
      {detail ? (
        <div className="mt-1 truncate text-200 text-text-primary-70" title={detail}>
          {detail}
        </div>
      ) : null}
    </div>
  );
}

function statusHeadline(event: RolloutEvent): string {
  switch (event.state) {
    case "scheduled":
      return "Scheduled";
    case "inProgress":
      return "In progress";
    case "pausedAtPilotGate":
      return "Paused — pilot review";
    case "paused":
      return "Paused";
    case "completed":
      return "Completed";
    case "completedWithFailures":
      return "Completed with failures";
  }
}

function statusIcon(event: RolloutEvent): ReactNode {
  if (event.state === "completedWithFailures") {
    return <Alert className="text-intent-critical-fill" />;
  }
  if (event.state === "completed") {
    return <Success className="text-core-primary-fill" />;
  }
  if (event.state === "paused" || event.state === "pausedAtPilotGate") {
    return <Alert className="text-core-accent-fill" />;
  }
  return <ProgressCircular indeterminate className="text-core-primary-fill" />;
}

/**
 * Progress-against-plan detail card for a rollout. Deliberately mirrors
 * `ActiveCurtailmentStatus`' layout vocabulary — a `SectionHeader`, the elevated
 * card, the big icon + primary lockup (`text-heading-300`), a stat-block grid,
 * one composition-bar progress section with legend + elapsed, and
 * top-right lifecycle buttons — without touching the curtailment implementation.
 * Process-agnostic: the phase copy adapts to `event.processType`.
 *
 * This is the detail surface an active rollout opens into (an Activity
 * rollout-detail area), the same home the active-curtailment card lives in.
 */
function ActiveRolloutStatus({
  event,
  className,
  embedded = false,
  hideActions = false,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
}: ActiveRolloutStatusProps): ReactElement {
  const isRunning = event.state === "inProgress";
  const isTerminal = event.state === "completed" || event.state === "completedWithFailures";
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const done = rolloutPhaseCount(event.rollups, "done");
  const percent = rolloutCompletionPercent(event);
  const segments = rolloutProgressSegments(event);
  const doneVerb = phaseLabel(event.processType, "done").toLowerCase();

  // Live-ticking elapsed timer while running, matching the curtailment card.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!isRunning || !event.startedAt) {
      return;
    }
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [isRunning, event.startedAt]);
  const elapsedSeconds = event.startedAt
    ? Math.max(Math.floor((now - new Date(event.startedAt).getTime()) / 1000), 0)
    : 0;

  const etaValue =
    event.estimatedSecondsRemaining && event.estimatedSecondsRemaining > 0
      ? `~${formatElapsed(event.estimatedSecondsRemaining)}`
      : isTerminal
        ? "—"
        : "Calculating…";

  const actions = hideActions
    ? []
    : rolloutLifecycleActions(event, { onPause, onResume, onCancelRemaining, onContinueFromPilot, onRetryFailed });
  const buttonVariant = {
    primary: variants.primary,
    secondary: variants.secondary,
    danger: variants.danger,
  } as const;

  return (
    <section className={clsx("grid gap-3", className)}>
      {embedded ? null : (
        <div className="min-w-0">
          <Header title={event.title} titleSize="text-heading-200" />
        </div>
      )}
      <div
        className={clsx(
          "@container relative",
          // Embedded in a modal: no card chrome, but match the modal's 24px
          // side inset with a 24px top gap so the status icon clears the sticky
          // top bar / header divider by the same margin.
          embedded ? "px-0 pt-6 pb-0" : "rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10",
        )}
      >
        {actions.length > 0 ? (
          <div className="mb-8 flex shrink-0 justify-end gap-3 tablet:absolute tablet:top-10 tablet:right-10 tablet:mb-0">
            {actions.map((action) => (
              <Button
                key={action.key}
                variant={buttonVariant[action.variant]}
                size={sizes.compact}
                text={action.text}
                onClick={action.onClick}
              />
            ))}
          </div>
        ) : null}

        <div className="grid gap-3 tablet:pr-32">
          <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">
            {statusIcon(event)}
          </div>
          <div data-testid="active-rollout-primary-lockup">
            <div className="text-heading-50 text-text-primary-70">{statusHeadline(event)}</div>
            <div className="text-heading-300 text-text-primary">{rolloutStageLabel(event)}</div>
          </div>
        </div>

        {/* Stat lockups reflow on the CARD's width (container query), not the
            viewport — so they stay 2-up in the standard modal (~592px) and go
            4-up only when the card is hosted in a wide standalone pane. */}
        <div className="mt-12 grid grid-cols-1 gap-x-8 gap-y-5 text-text-primary @xs:grid-cols-2 @3xl:grid-cols-4">
          <StatBlock label="Scope" value={event.scopeLabel || "—"} />
          <StatBlock label="Strategy" value={pacingSummary(event)} detail={orderLabels[event.order]} />
          <StatBlock
            label={`Miners ${doneVerb}`}
            value={`${done.toLocaleString()} of ${inScope.toLocaleString()}`}
            detail={`${percent}%`}
          />
          <StatBlock
            label="Est. time remaining"
            value={etaValue}
            detail={event.startedAt ? `${formatElapsed(elapsedSeconds)} elapsed` : undefined}
          />
        </div>

        <div className="mt-8 grid gap-3" data-testid="active-rollout-progress">
          <CompositionBar segments={segments} height={12} colorMap={rolloutProgressColorMap} />
          <div className="flex flex-wrap items-start gap-x-5 gap-y-1 text-200 text-text-primary-70">
            {segments.map((segment) => (
              <span key={segment.name} className="flex items-start gap-2">
                <span
                  className={clsx(
                    "mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full",
                    rolloutProgressColorMap[segment.status],
                  )}
                />
                {`${segment.name} (${(segment.count ?? 0).toLocaleString()})`}
              </span>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

export default ActiveRolloutStatus;
