import { type ReactElement, type ReactNode, useEffect, useId, useState } from "react";
import clsx from "clsx";

import {
  orderLabels,
  pacingDetail,
  phaseLabel,
  rolloutActiveHeaderDetail,
  rolloutActiveSectionLabel,
  rolloutActiveStatusLabel,
  rolloutCompletedTargetCount,
  rolloutCompletionPercent,
  rolloutCompletionPhase,
  rolloutLifecycleActions,
  rolloutProgressSegments,
  rolloutStageLabel,
} from "./rolloutDisplayUtils";
import RolloutErrorCallout from "./RolloutErrorCallout";
import RolloutPerformanceStrip from "./RolloutPerformanceStrip";
import type { RolloutEvent, RolloutProgress } from "./rolloutTypes";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { Alert, Info, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
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
  canManage?: boolean;
  canControl?: boolean;
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onAbort?: () => void;
  onRevert?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromReview?: () => void;
  onCompleteWithFailures?: () => void;
  onRetryFailed?: () => void;
  onViewMiners?: () => void;
  onViewErrors?: () => void;
}

interface StatBlockProps {
  label: string;
  value: string;
  detail?: string;
}

interface SectionHeaderProps {
  title: string;
  children?: ReactNode;
}

function SectionHeader({ title, children }: SectionHeaderProps): ReactElement {
  return (
    <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
      <div className="min-w-0">
        <Header title={title} titleSize="text-heading-200" />
        {children ? <div className="mt-1 text-300 text-text-primary">{children}</div> : null}
      </div>
    </div>
  );
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

function telemetryStabilizingSubtitle(etaValue: string): string {
  if (etaValue === "Calculating") {
    return "Review becomes available after miners stabilize.";
  }
  return `Review becomes available in ${etaValue}.`;
}

function statusIcon(event: RolloutEvent): ReactNode {
  if (event.state === "completedWithFailures") {
    return <Alert className="text-intent-critical-fill" />;
  }
  if (event.state === "completed" || event.state === "reverted") {
    return <Success className="text-intent-success-fill" />;
  }
  if (event.state === "review" || event.state === "pausedAtPilotGate" || event.state === "pausedAtBatchReview") {
    return <Info className="text-text-primary" />;
  }
  if (event.state === "paused" || event.state === "aborted") {
    return <Alert className="text-core-accent-fill" />;
  }
  return <ProgressCircular indeterminate className="text-core-primary-fill" />;
}

function IndependentProgress({
  label,
  progress,
  testId,
}: {
  label: string;
  progress: RolloutProgress;
  testId: string;
}): ReactElement {
  const total = Math.max(progress.total, 0);
  const completed = Math.min(Math.max(progress.completed, 0), total);
  const percent = total > 0 ? Math.round((completed / total) * 100) : 0;
  const details = [
    progress.failed ? `${progress.failed.toLocaleString()} failed` : null,
    progress.attentionRequired ? `${progress.attentionRequired.toLocaleString()} needs attention` : null,
  ].filter(Boolean);

  return (
    <div className="grid gap-2" data-testid={testId}>
      <div className="flex flex-wrap justify-between gap-2 text-200">
        <span className="text-text-primary-70">{label}</span>
        <span className="text-text-primary">
          {completed.toLocaleString()} of {total.toLocaleString()} ({percent}%)
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-core-primary-10">
        <div
          className="h-full rounded-full bg-core-primary-fill"
          style={{ width: `${percent}%` }}
          role="progressbar"
          aria-label={label}
          aria-valuemin={0}
          aria-valuemax={total}
          aria-valuenow={completed}
        />
      </div>
      {details.length > 0 ? <div className="text-200 text-text-primary-70">{details.join(", ")}</div> : null}
    </div>
  );
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
  canManage = true,
  canControl = true,
  onManage,
  onPause,
  onResume,
  onAbort,
  onRevert,
  onCancelRemaining,
  onContinueFromReview,
  onCompleteWithFailures,
  onRetryFailed,
  onViewMiners,
  onViewErrors,
}: ActiveRolloutStatusProps): ReactElement {
  const detailsId = useId();
  const isReviewGate =
    event.state === "review" || event.state === "pausedAtPilotGate" || event.state === "pausedAtBatchReview";
  const detailsStateKey = isReviewGate ? `${event.state}-${event.currentBatch ?? "current"}` : "default";
  const [detailsState, setDetailsState] = useState(() => ({
    key: detailsStateKey,
    open: defaultDetailsOpen || isReviewGate,
  }));
  const detailsOpen =
    embedded || (detailsState.key === detailsStateKey ? detailsState.open : defaultDetailsOpen || isReviewGate);
  const isRunning = event.state === "running" || event.state === "inProgress";
  const isTelemetryStabilizing = event.state === "stabilizingTelemetry";
  const isTerminal =
    event.state === "aborted" ||
    event.state === "completed" ||
    event.state === "completedWithFailures" ||
    event.state === "reverted";
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const done = rolloutCompletedTargetCount(event);
  const percent = rolloutCompletionPercent(event);
  const segments = rolloutProgressSegments(event);
  const doneVerb = phaseLabel(event.processType, rolloutCompletionPhase(event)).toLowerCase();

  // Live-ticking elapsed timer while running, matching the curtailment card.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if ((!isRunning && !isTelemetryStabilizing) || !event.startedAt) {
      return;
    }
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [isRunning, isTelemetryStabilizing, event.startedAt]);
  const elapsedSeconds = event.startedAt
    ? Math.max(Math.floor((now - new Date(event.startedAt).getTime()) / 1000), 0)
    : 0;

  const etaValue =
    event.estimatedSecondsRemaining && event.estimatedSecondsRemaining > 0
      ? `~${formatElapsed(event.estimatedSecondsRemaining)}`
      : isTerminal
        ? "N/A"
        : "Calculating";
  const pacing = pacingDetail(event);

  const statItems: StatBlockProps[] = [
    ...(embedded ? [{ label: "Scope", value: event.scopeLabel || "N/A" }] : []),
    { label: pacing.method, value: pacing.value },
    // Order only applies to paced or pilot runs. Under "single batch" there's no first/last.
    ...(event.strategy === "allAtOnce" ? [] : [{ label: "Order", value: orderLabels[event.order] }]),
    { label: isTelemetryStabilizing ? "Review available" : "Est. time remaining", value: etaValue },
  ];

  // Progress summary + elapsed live in the progress section, rather than the stat grid.
  const progressSummary = `${done.toLocaleString()} of ${inScope.toLocaleString()} miners ${doneVerb} (${percent}%)`;
  const actions = hideActions
    ? []
    : rolloutLifecycleActions(
        event,
        {
          onManage,
          onPause,
          onResume,
          onAbort,
          onRevert,
          onCancelRemaining,
          onContinueFromReview,
          onCompleteWithFailures,
          onRetryFailed,
        },
        { canManage, canControl },
      );
  const visibleActions = actions.filter((action) => action.key !== "cancel" && action.key !== "abort");
  const overflowLifecycleActions = actions.filter((action) => action.key === "cancel" || action.key === "abort");
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
        <SectionHeader title={rolloutActiveSectionLabel(event.processType)}>
          <div className="max-w-xl">
            <div className="text-emphasis-300">{rolloutActiveHeaderDetail(event)}</div>
          </div>
        </SectionHeader>
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
            <div className="text-heading-50 text-text-primary-70">{rolloutActiveStatusLabel(event.processType)}</div>
            <div className="text-heading-300 text-text-primary">{rolloutStageLabel(event)}</div>
          </div>
        </div>

        <RolloutErrorCallout event={event} onReviewErrors={onViewErrors} />

        {isTelemetryStabilizing ? (
          <Callout
            className="mt-6"
            intent={intents.information}
            prefixIcon={<Info />}
            testId="active-rollout-telemetry-stabilizing-banner"
            title="Telemetry is stabilizing"
            subtitle={telemetryStabilizingSubtitle(etaValue)}
          />
        ) : null}

        {/* Progress stays visible in the collapsed card. Embedded modal details
            remain expanded because the modal is already the detail destination. */}
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

        {event.membershipProgress || event.convergenceProgress ? (
          <div className="mt-6 grid gap-5 border-t border-border-5 pt-5 tablet:grid-cols-2">
            {event.membershipProgress ? (
              <IndependentProgress
                label="Membership progress"
                progress={event.membershipProgress}
                testId="active-rollout-membership-progress"
              />
            ) : null}
            {event.convergenceProgress ? (
              <IndependentProgress
                label="Firmware convergence"
                progress={event.convergenceProgress}
                testId="active-rollout-convergence-progress"
              />
            ) : null}
          </div>
        ) : null}

        {embedded ? null : (
          <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-200">
            <button
              type="button"
              className="cursor-pointer text-text-primary underline underline-offset-2 hover:opacity-70 focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base focus-visible:outline-none"
              aria-expanded={detailsOpen}
              aria-controls={detailsId}
              data-testid="active-rollout-details-toggle"
              onClick={() => setDetailsState({ key: detailsStateKey, open: !detailsOpen })}
            >
              {detailsOpen ? "Hide details" : "View details"}
            </button>
          </div>
        )}

        {detailsOpen ? (
          <div
            id={detailsId}
            className={clsx(
              "border-t border-border-5",
              embedded ? "mt-4 pt-5" : "-mx-6 mt-6 px-6 pt-6 tablet:-mx-10 tablet:mt-10 tablet:px-10 tablet:pt-10",
            )}
            data-testid="active-rollout-details"
          >
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
              <div className="grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
                {statItems.map((item) => (
                  <StatBlock key={item.label} label={item.label} value={item.value} detail={item.detail} />
                ))}
              </div>
            )}

            {pacing.detail ? (
              <div className="mt-3 text-200 text-text-primary-70" data-testid="active-rollout-pacing-helper">
                {pacing.detail}
              </div>
            ) : null}

            {/* Baseline telemetry for running batches and review gates. */}
            <RolloutPerformanceStrip event={event} embedded={embedded} />
          </div>
        ) : null}
      </div>
    </section>
  );
}

export default ActiveRolloutStatus;
