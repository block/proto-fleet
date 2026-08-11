import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import {
  formatRolloutMetric,
  orderLabels,
  pacingSummary,
  phaseLabel,
  rolloutCompletionPercent,
  rolloutLifecycleActions,
  rolloutMetricDelta,
  type RolloutMetricDelta,
  type RolloutMetricDeltaIntent,
  rolloutPhaseCount,
  rolloutProgressSegments,
  rolloutStageLabel,
} from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

/**
 * Rollout progress colors follow the active curtailment card: done is primary,
 * remaining is accent, and failures are critical.
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
  /** Drop card chrome when the host already provides an elevated surface. */
  embedded?: boolean;
  /** Suppress lifecycle actions when the host renders them elsewhere. */
  hideActions?: boolean;
  /** Lifecycle actions. Missing handlers hide their controls. */
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromPilot?: () => void;
  onRetryFailed?: () => void;
  onViewMiners?: () => void;
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

/**
 * A single stat as a standard label/value table row. This follows the `SummaryRow` pattern
 * shared with `ActivityDetailModal`: label pinned left, value right-aligned, a
 * hairline divider between rows. Used in the modal (`embedded`) presentation,
 * where the four stats read better stacked as detail rows than as a stat grid.
 * `detail` (percent / elapsed) sits under the value, still right-aligned.
 */
function StatRow({ label, value, detail, divider }: StatBlockProps & { divider: boolean }): ReactElement {
  return (
    <Row compact divider={divider}>
      <div className="flex w-full items-start justify-between gap-4">
        <span className="shrink-0 text-300 text-text-primary-70">{label}</span>
        <span className="flex min-w-0 flex-col items-end text-right">
          <span className="min-w-0 text-300 break-words text-text-primary">{value}</span>
          {detail ? <span className="min-w-0 text-200 break-words text-text-primary-70">{detail}</span> : null}
        </span>
      </div>
    </Row>
  );
}

// Deltas show movement only. The UI does not judge whether the change is good
// or bad for the operator.
const deltaTextColor: Record<RolloutMetricDeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
};

/**
 * Signed metric delta rendered beside the current value.
 */
function DeltaChip({ delta }: { delta: RolloutMetricDelta }): ReactElement {
  return <span className={deltaTextColor[delta.intent]}>{delta.deltaText}</span>;
}

/**
 * Baseline-vs-current telemetry for pilot review.
 */
function PerformanceStrip({
  event,
  embedded = false,
}: {
  event: RolloutEvent;
  embedded?: boolean;
}): ReactElement | null {
  const temperatureUnit = useTemperatureUnit();
  if (!event.performance || event.performance.metrics.length === 0) {
    return null;
  }
  return (
    <div
      className={clsx(
        "mt-6 grid gap-y-5 text-text-primary",
        embedded ? "gap-x-8 tablet:grid-cols-2 laptop:grid-cols-4" : "gap-x-12 tablet:grid-cols-5",
      )}
      data-testid="active-rollout-performance"
    >
      {event.performance.metrics.map((metric) => {
        const value = formatRolloutMetric(metric, temperatureUnit);
        return (
          <div key={metric.label} className="min-w-0">
            <div className="text-200 text-text-primary-50">{metric.label}</div>
            <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-emphasis-300 text-text-primary">
              <span className={clsx("min-w-0", embedded ? "whitespace-nowrap" : "truncate")} title={value}>
                {value}
              </span>
              <DeltaChip delta={rolloutMetricDelta(metric)} />
            </div>
          </div>
        );
      })}
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
      return "Paused for pilot review";
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
 * Progress-against-plan detail card for active rollout work.
 */
function ActiveRolloutStatus({
  event,
  className,
  embedded = false,
  hideActions = false,
  onManage,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
  onViewMiners,
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

  const statItems: StatBlockProps[] = [
    { label: "Scope", value: event.scopeLabel || "—" },
    { label: "Strategy", value: pacingSummary(event) },
    // Order only applies to a paced run. Under "all at once" there's no first/last.
    ...(event.strategy === "allAtOnce" ? [] : [{ label: "Order", value: orderLabels[event.order] }]),
    { label: "Est. time remaining", value: etaValue },
  ];

  // Progress summary + elapsed live in the progress section, rather than the stat grid.
  const progressSummary = `${done.toLocaleString()} of ${inScope.toLocaleString()} miners ${doneVerb} (${percent}%)`;

  const actions = hideActions
    ? []
    : rolloutLifecycleActions(event, {
        onManage,
        onPause,
        onResume,
        onCancelRemaining,
        onContinueFromPilot,
        onRetryFailed,
      });
  const visibleActions = actions.filter((action) => action.key !== "cancel");
  const overflowLifecycleActions = actions.filter((action) => action.key === "cancel");
  const overflowMenuActions: RowAction[] = [];
  if (!hideActions && onViewMiners) {
    overflowMenuActions.push({
      label: "View miners",
      onClick: onViewMiners,
      showGroupDivider: overflowLifecycleActions.length > 0,
      testId: "active-rollout-view-miners-action",
    });
  }
  overflowLifecycleActions.forEach((action) => {
    if (!action.onClick) {
      return;
    }
    overflowMenuActions.push({
      label: action.text,
      onClick: action.onClick,
      danger: action.variant === "danger",
      testId: `active-rollout-${action.key}-action`,
    });
  });
  const hasTopActions = visibleActions.length > 0 || overflowMenuActions.length > 0;
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
          "relative",
          // Embedded in a modal: no card chrome, but match the modal's 24px
          // side inset with a 24px top gap so the status icon clears the sticky
          // top bar / header divider by the same margin.
          embedded ? "px-0 pt-6 pb-0" : "rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10",
        )}
      >
        {hasTopActions ? (
          <div className="mb-8 flex shrink-0 flex-wrap justify-end gap-3 tablet:absolute tablet:top-10 tablet:right-10 tablet:mb-0 tablet:max-w-[24rem]">
            {overflowMenuActions.length > 0 ? (
              <RowActionsMenu
                actions={overflowMenuActions}
                ariaLabel={`More actions for ${event.title}`}
                popoverTestId="active-rollout-more-actions-menu"
                testIdPrefix="active-rollout-more-actions"
                triggerClassName="!h-8 !w-8 !px-0 !py-0"
                triggerVariant={variants.secondary}
              />
            ) : null}
            {visibleActions.map((action) => (
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

        <div className={clsx("grid gap-3", hasTopActions && "tablet:pr-96")}>
          <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">
            {statusIcon(event)}
          </div>
          <div data-testid="active-rollout-primary-lockup">
            <div className="text-heading-50 text-text-primary-70">{statusHeadline(event)}</div>
            <div className="text-heading-300 text-text-primary">{rolloutStageLabel(event)}</div>
          </div>
        </div>

        {/* Stat lockups: in the modal (embedded) they read as standard
            label/value table rows; in the standalone card they use the same
            multi-column stat grid as ActiveCurtailmentStatus (grid-cols-5,
            gap-x-12). */}
        {embedded ? (
          <div className="mt-8 flex flex-col">
            {statItems.map((item, index) => (
              <StatRow
                key={item.label}
                label={item.label}
                value={item.value}
                detail={item.detail}
                divider={index < statItems.length - 1}
              />
            ))}
          </div>
        ) : (
          <div className="mt-8 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-5">
            {statItems.map((item) => (
              <StatBlock key={item.label} label={item.label} value={item.value} detail={item.detail} />
            ))}
          </div>
        )}

        {/* Baseline telemetry for pilot review. */}
        <PerformanceStrip event={event} embedded={embedded} />

        {/* Progress section: summary, elapsed time, bar, then legend. */}
        <div className="mt-6 grid gap-3" data-testid="active-rollout-progress">
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
            <div className="text-200 text-text-primary-50">{progressSummary}</div>
            {event.startedAt ? (
              <div className="text-right text-200 text-text-primary">{`${formatElapsed(elapsedSeconds)} elapsed`}</div>
            ) : null}
          </div>
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
            {/* Excluded targets sit outside the bar and appear as a separate legend item. */}
            {event.excludedTargets > 0 ? (
              <span className="ml-auto text-right text-text-primary-50">
                {`${event.excludedTargets.toLocaleString()} excluded`}
              </span>
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}

export default ActiveRolloutStatus;
