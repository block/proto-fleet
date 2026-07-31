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
import type { RolloutEvent, RolloutTargetPhase } from "./rolloutTypes";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface ActiveRolloutStatusProps {
  event: RolloutEvent;
  className?: string;
  /** Lifecycle actions — each rendered only when its handler is supplied, so
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

const legendSwatch: Record<RolloutTargetPhase, string> = {
  done: "bg-intent-success-fill",
  inProgress: "bg-intent-warning-fill",
  queued: "bg-grayscale-gray-50",
  failed: "bg-intent-critical-fill",
  excluded: "bg-grayscale-gray-50",
};

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
 * Progress-against-plan detail card for a rollout, modeled directly on
 * curtailment's `ActiveCurtailmentStatus`: composition bar over the phase
 * rollups, a stat-block grid, a live elapsed timer, grouped-issue annotations,
 * and capability-gated lifecycle buttons. Process-agnostic — the copy adapts to
 * `event.processType` via `phaseLabel`.
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
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const done = rolloutPhaseCount(event.rollups, "done");
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const percent = rolloutCompletionPercent(event);
  const segments = rolloutCompositionSegments(event);

  // Live-ticking elapsed timer while running, mirroring the curtailment card's
  // per-second cadence.
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

  const showPilotGate = event.state === "pausedAtPilotGate";
  const showFailureActions = failed > 0 && (isTerminal || showPilotGate);

  return (
    <div className={clsx("relative rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10", className)}>
      <div className="mb-8 flex shrink-0 justify-end gap-3 tablet:absolute tablet:top-10 tablet:right-10 tablet:mb-0">
        {onRetryFailed && showFailureActions ? (
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
            variant={variants.secondaryDanger}
            size={sizes.compact}
            text="Cancel remaining"
            onClick={onCancelRemaining}
          />
        ) : null}
      </div>

      <div className="flex items-start gap-3">
        <div className="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-core-primary-5">
          {statusIcon(event)}
        </div>
        <div className="min-w-0">
          <div className="text-heading-50 text-text-primary-70">{phaseLabel(event.processType, "done")} miners</div>
          <Header title={`${done.toLocaleString()} of ${inScope.toLocaleString()}`} titleSize="text-heading-300" />
        </div>
      </div>

      <div className="mt-12 grid gap-8 tablet:grid-cols-4">
        <StatBlock label="Status" value={statusHeadline(event)} />
        <StatBlock label="Strategy" value={pacingSummary(event)} detail={orderLabels[event.order]} />
        <StatBlock label="Batch" value={batchValue} />
        <StatBlock label="Est. time remaining" value={etaValue} />
      </div>

      <div className="mt-8 grid gap-3">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="text-200 text-text-primary-50">
            {done.toLocaleString()} of {inScope.toLocaleString()} {phaseLabel(event.processType, "done").toLowerCase()}{" "}
            ({percent}%)
          </div>
          {event.startedAt ? (
            <div className="text-200 text-text-primary">{formatElapsed(elapsedSeconds)} elapsed</div>
          ) : null}
        </div>
        <CompositionBar segments={segments} height={12} />
        <div className="flex flex-wrap gap-5 text-200 text-text-primary-70">
          {segments.map((segment) => (
            <span key={segment.name} className="flex items-center gap-2">
              <span
                className={clsx(
                  "h-2 w-2 rounded-full",
                  segment.status === "OK"
                    ? legendSwatch.done
                    : segment.status === "WARNING"
                      ? legendSwatch.inProgress
                      : segment.status === "CRITICAL"
                        ? legendSwatch.failed
                        : legendSwatch.queued,
                )}
              />
              {segment.name} ({(segment.count ?? 0).toLocaleString()})
            </span>
          ))}
        </div>
      </div>

      {showPilotGate ? (
        <div className="mt-8 rounded-lg border border-core-accent-fill/30 bg-core-accent-fill/5 p-4">
          <div className="text-emphasis-300 text-text-primary">Pilot group complete — review before continuing</div>
          <div className="mt-1 text-300 text-text-primary-70">
            {done.toLocaleString()} succeeded, {failed.toLocaleString()} failed in the pilot wave. Continue the rollout
            to the remaining {(inScope - done - failed).toLocaleString()} miners, or retry the failures first.
          </div>
        </div>
      ) : null}

      {event.issueGroups && event.issueGroups.length > 0 ? (
        <div className="mt-8 grid gap-2">
          <div className="text-emphasis-200 text-text-primary-50">Grouped issues</div>
          <div className="flex flex-wrap gap-2">
            {event.issueGroups.map((group) => (
              <span
                key={group.label}
                className="flex items-center gap-1.5 rounded border border-border-5 px-2 py-1 text-200 text-text-primary-70"
              >
                <Alert className="text-intent-critical-fill" width="w-3" />
                {group.label} ×{group.count.toLocaleString()}
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default ActiveRolloutStatus;
