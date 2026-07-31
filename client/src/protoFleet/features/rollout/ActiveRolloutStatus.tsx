import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import {
  orderLabels,
  pacingSummary,
  phaseLabel,
  rolloutCompletionPercent,
  rolloutCompositionSegments,
  rolloutPhaseCount,
} from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface ActiveRolloutStatusProps {
  event: RolloutEvent;
  className?: string;
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
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
}: ActiveRolloutStatusProps): ReactElement {
  const isRunning = event.state === "inProgress";
  const isTerminal = event.state === "completed" || event.state === "completedWithFailures";
  const showPilotGate = event.state === "pausedAtPilotGate";
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const done = rolloutPhaseCount(event.rollups, "done");
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const percent = rolloutCompletionPercent(event);
  const segments = rolloutCompositionSegments(event);
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
  const batchValue =
    event.currentBatch && event.totalBatches ? `Batch ${event.currentBatch} of ${event.totalBatches}` : "—";

  return (
    <section className={clsx("grid gap-3", className)}>
      <div className="min-w-0">
        <Header title={event.title} titleSize="text-heading-200" />
        {event.scopeLabel ? (
          <div className="mt-1 text-emphasis-300 text-text-primary">Applies to {event.scopeLabel}</div>
        ) : null}
      </div>

      <div className="relative rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10">
        <div className="mb-8 flex shrink-0 justify-end gap-3 tablet:absolute tablet:top-10 tablet:right-10 tablet:mb-0">
          {onRetryFailed && failed > 0 && (isTerminal || showPilotGate) ? (
            <Button variant={variants.secondary} size={sizes.compact} text="Retry failed" onClick={onRetryFailed} />
          ) : null}
          {onContinueFromPilot && showPilotGate ? (
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Continue rollout"
              onClick={onContinueFromPilot}
            />
          ) : null}
          {onResume && event.state === "paused" ? (
            <Button variant={variants.primary} size={sizes.compact} text="Resume" onClick={onResume} />
          ) : null}
          {onPause && isRunning ? (
            <Button variant={variants.secondary} size={sizes.compact} text="Pause" onClick={onPause} />
          ) : null}
          {onCancelRemaining && !isTerminal ? (
            <Button
              variant={variants.danger}
              size={sizes.compact}
              text="Cancel remaining"
              onClick={onCancelRemaining}
            />
          ) : null}
        </div>

        <div className="grid gap-3 tablet:pr-32">
          <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">
            {statusIcon(event)}
          </div>
          <div data-testid="active-rollout-primary-lockup">
            <div className="text-heading-50 text-text-primary-70">Miners {doneVerb}</div>
            <div className="text-heading-300 text-text-primary">
              {done.toLocaleString()} of {inScope.toLocaleString()}
            </div>
          </div>
        </div>

        <div className="mt-12 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
          <StatBlock label="Status" value={statusHeadline(event)} />
          <StatBlock label="Strategy" value={pacingSummary(event)} detail={orderLabels[event.order]} />
          <StatBlock label="Batch" value={batchValue} />
          <StatBlock label="Estimated time remaining" value={etaValue} />
        </div>

        <div className="mt-8 grid gap-3" data-testid="active-rollout-progress">
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
            <div className="text-200 text-text-primary-50">
              {done.toLocaleString()} of {inScope.toLocaleString()} {doneVerb} ({percent}%)
            </div>
            {event.startedAt ? (
              <div className="text-200 text-text-primary">{formatElapsed(elapsedSeconds)} elapsed</div>
            ) : null}
          </div>
          <CompositionBar segments={segments} height={12} />
          <div className="flex flex-wrap items-start gap-x-5 gap-y-1 text-200 text-text-primary-70">
            {segments.map((segment) => (
              <span key={segment.name} className="flex items-start gap-2">
                <span
                  className={clsx(
                    "mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full",
                    segment.status === "OK"
                      ? "bg-intent-success-fill"
                      : segment.status === "WARNING"
                        ? "bg-intent-warning-fill"
                        : segment.status === "CRITICAL"
                          ? "bg-intent-critical-fill"
                          : "bg-grayscale-gray-50",
                  )}
                />
                {`${segment.name} (${(segment.count ?? 0).toLocaleString()})`}
              </span>
            ))}
          </div>
        </div>

        {showPilotGate ? (
          <div className="mt-6 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
            <div className="text-emphasis-300">Pilot group complete — review before continuing</div>
            <div className="mt-1 text-text-primary-70">
              {done.toLocaleString()} succeeded, {failed.toLocaleString()} failed in the pilot wave. Continue to the
              remaining {(inScope - done - failed).toLocaleString()} miners, or retry the failures first.
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}

export default ActiveRolloutStatus;
