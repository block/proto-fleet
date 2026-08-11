import { type ReactElement, type ReactNode, useEffect, useId, useState } from "react";
import clsx from "clsx";

import {
  formatRolloutMetric,
  orderLabels,
  pacingSummary,
  phaseLabel,
  rolloutCompletionPercent,
  rolloutErrorImpactCount,
  rolloutLifecycleActions,
  type RolloutMetricDelta,
  rolloutMetricDelta,
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
  /** Start with the lower detail section expanded. */
  defaultDetailsOpen?: boolean;
  /** Lifecycle actions. Missing handlers hide their controls. */
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromPilot?: () => void;
  onRetryFailed?: () => void;
  onViewMiners?: () => void;
  onViewErrors?: () => void;
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
      <div className="mt-1 text-emphasis-300 break-words text-text-primary" title={value}>
        {value}
      </div>
      {detail ? (
        <div className="mt-1 text-200 break-words text-text-primary-70" title={detail}>
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

// Deltas keep the signed value, but color by outcome for the metric. A
// temperature increase is bad even though the sign is positive.
const deltaTextColor: Record<RolloutMetricDeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
  neutral: "text-text-primary-50",
};

/**
 * Signed metric delta rendered beside the current value.
 */
function DeltaChip({ delta }: { delta: RolloutMetricDelta }): ReactElement {
  return <span className={deltaTextColor[delta.intent]}>{delta.deltaText}</span>;
}

function errorCountLabel(count: number): string {
  return `${count.toLocaleString()} ${count === 1 ? "error" : "errors"}`;
}

/**
 * Baseline-vs-current telemetry for pilot review.
 */
function PerformanceStrip({
  event,
  embedded = false,
  onViewErrors,
}: {
  event: RolloutEvent;
  embedded?: boolean;
  onViewErrors?: () => void;
}): ReactElement | null {
  const temperatureUnit = useTemperatureUnit();
  const hasErrorSummary = event.performance?.errors !== undefined;
  const errorCount = rolloutErrorImpactCount(event.performance?.errors);
  if (!event.performance || (event.performance.metrics.length === 0 && !hasErrorSummary)) {
    return null;
  }

  return (
    <div className="mt-6" data-testid="active-rollout-performance">
      <div
        className={clsx(
          "grid gap-y-5 text-text-primary",
          embedded ? "gap-x-8 tablet:grid-cols-2 laptop:grid-cols-5" : "gap-x-12 tablet:grid-cols-5",
        )}
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
                <DeltaChip delta={rolloutMetricDelta(metric, temperatureUnit)} />
              </div>
            </div>
          );
        })}
        {hasErrorSummary ? (
          <div className="min-w-0">
            <div className="text-200 text-text-primary-50">Errors</div>
            {errorCount > 0 && onViewErrors ? (
              <button
                type="button"
                className="mt-1 cursor-pointer text-emphasis-300 text-text-primary underline underline-offset-2 hover:opacity-70 focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base focus-visible:outline-none"
                onClick={onViewErrors}
                data-testid="active-rollout-errors-link"
                aria-label={`View ${errorCount.toLocaleString()} errors`}
              >
                {errorCount.toLocaleString()}
              </button>
            ) : (
              <div className="mt-1 text-emphasis-300 text-text-primary">{errorCount.toLocaleString()}</div>
            )}
          </div>
        ) : null}
      </div>
      <div className="mt-3 text-200 text-text-primary-50">
        Compares the 30-minute pre-update baseline with post-update telemetry after miners stabilize.
      </div>
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

function ProgressLegend({ event, segments }: { event: RolloutEvent; segments: Segment[] }): ReactElement {
  return (
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
  );
}

/**
 * Progress-against-plan detail card for active rollout work.
 */
function ActiveRolloutStatus({
  event,
  className,
  embedded = false,
  hideActions = false,
  defaultDetailsOpen = false,
  onManage,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
  onViewMiners,
  onViewErrors,
}: ActiveRolloutStatusProps): ReactElement {
  const detailsId = useId();
  const [detailsOpen, setDetailsOpen] = useState(defaultDetailsOpen);
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
        ? "N/A"
        : "Calculating";

  const statItems: StatBlockProps[] = [
    { label: "Scope", value: event.scopeLabel || "N/A" },
    { label: "Method", value: pacingSummary(event) },
    // Order only applies to a paced run. Under "all at once" there's no first/last.
    ...(event.strategy === "allAtOnce" ? [] : [{ label: "Order", value: orderLabels[event.order] }]),
    { label: "Est. time remaining", value: etaValue },
  ];

  // Progress summary + elapsed live in the progress section, rather than the stat grid.
  const progressSummary = `${done.toLocaleString()} of ${inScope.toLocaleString()} miners ${doneVerb} (${percent}%)`;
  const errorCount = rolloutErrorImpactCount(event.performance?.errors);
  const hasCollapsedErrorLink = !detailsOpen && errorCount > 0;

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

        {/* Progress stays visible in the collapsed card; rollout setup and telemetry
            sit behind the disclosure below. */}
        <div className="mt-6 grid gap-3" data-testid="active-rollout-progress">
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
            <div className="text-200 text-text-primary-50">{progressSummary}</div>
            {event.startedAt ? (
              <div className="text-right text-200 text-text-primary">{`${formatElapsed(elapsedSeconds)} elapsed`}</div>
            ) : null}
          </div>
          <CompositionBar segments={segments} height={12} colorMap={rolloutProgressColorMap} />
          <ProgressLegend event={event} segments={segments} />
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-200">
          <button
            type="button"
            className="cursor-pointer text-text-primary underline underline-offset-2 hover:opacity-70 focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base focus-visible:outline-none"
            aria-expanded={detailsOpen}
            aria-controls={detailsId}
            data-testid="active-rollout-details-toggle"
            onClick={() => setDetailsOpen((open) => !open)}
          >
            {detailsOpen ? "Hide details" : "View details"}
          </button>
          {hasCollapsedErrorLink && onViewErrors ? (
            <button
              type="button"
              className="cursor-pointer text-intent-critical-fill underline underline-offset-2 hover:opacity-70 focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base focus-visible:outline-none"
              onClick={onViewErrors}
              data-testid="active-rollout-collapsed-errors-link"
              aria-label={`View ${errorCountLabel(errorCount)}`}
            >
              {errorCountLabel(errorCount)}
            </button>
          ) : hasCollapsedErrorLink ? (
            <span className="text-intent-critical-fill">{errorCountLabel(errorCount)}</span>
          ) : null}
        </div>

        {detailsOpen ? (
          <div id={detailsId} className="mt-6 border-t border-border-5 pt-6" data-testid="active-rollout-details">
            {/* Stat lockups: in the modal (embedded) they read as standard
                label/value table rows; in the standalone card they use the same
                multi-column stat grid as ActiveCurtailmentStatus (grid-cols-5,
                gap-x-12). */}
            {embedded ? (
              <div className="flex flex-col">
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
              <div className="grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-5">
                {statItems.map((item) => (
                  <StatBlock key={item.label} label={item.label} value={item.value} detail={item.detail} />
                ))}
              </div>
            )}

            {/* Baseline telemetry for pilot review. */}
            <PerformanceStrip event={event} embedded={embedded} onViewErrors={onViewErrors} />
          </div>
        ) : null}
      </div>
    </section>
  );
}

export default ActiveRolloutStatus;
