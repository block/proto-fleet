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
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

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
  onManage?: () => void;
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

/**
 * A single stat as a standard label/value table row — the `SummaryRow` pattern
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
          <span className="min-w-0 truncate text-300 text-text-primary" title={value}>
            {value}
          </span>
          {detail ? (
            <span className="min-w-0 truncate text-200 text-text-primary-70" title={detail}>
              {detail}
            </span>
          ) : null}
        </span>
      </div>
    </Row>
  );
}

// A metric's move off baseline colors its delta purely by sign: a rise reads
// success (the standard green, `-fill`), a drop reads critical (the standard
// red). No arrow icons, no "±" — just the signed "+"/"−" magnitude in the
// matching color, at the same size as the value it sits beside. The delta only
// shows which way the value moved — it does NOT judge good/bad or infer whether
// to continue (per the design review: show the numbers, don't decide the
// operator's action).
const deltaTextColor: Record<RolloutMetricDeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
};

/**
 * Small signed-delta beside a metric's current value: a "+" for a rise, a "−"
 * for a drop, followed by the magnitude, in the standard green for a rise and
 * red for a drop. No size class of its own — it inherits the value's
 * `text-emphasis-300` from the row so the sign reads at the same size as the
 * number it annotates. Just the sign and the number — no arrow glyph, no "±".
 */
function DeltaChip({ delta }: { delta: RolloutMetricDelta }): ReactElement {
  return <span className={deltaTextColor[delta.intent]}>{delta.deltaText}</span>;
}

/**
 * Baseline-vs-current performance readout — the Option-A strip from the design
 * review (Caleb's "confirm no adverse effects" + Rongxin's baseline capture).
 * Each tracked metric shows its current value (formatted with the shared
 * telemetry formatters) alongside a colored Δ-vs-baseline chip, so at the
 * pilot-review gate an operator can see whether the change moved the fleet
 * before continuing. It's a plain readout: no header label, no guardrail
 * verdict, no inferred continue/review action — the numbers, with the decision
 * left to the operator. The metric lockups match the card's `StatBlock` stat
 * grid so the readout reads as one more content grouping — and it shares the
 * grid's `tablet:grid-cols-5` / `gap-x-12` template so each metric column sits
 * directly under a stat column above (Hashrate under Scope, etc.) rather than
 * floating in its own out-of-register grid. Renders only when the event carries
 * a captured baseline.
 */
function PerformanceStrip({ event }: { event: RolloutEvent }): ReactElement | null {
  const temperatureUnit = useTemperatureUnit();
  if (!event.performance || event.performance.metrics.length === 0) {
    return null;
  }
  return (
    <div
      className="mt-6 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-5"
      data-testid="active-rollout-performance"
    >
      {event.performance.metrics.map((metric) => {
        const value = formatRolloutMetric(metric, temperatureUnit);
        return (
          <div key={metric.label} className="min-w-0">
            <div className="text-200 text-text-primary-50">{metric.label}</div>
            <div className="mt-1 flex items-baseline gap-2 text-emphasis-300 text-text-primary">
              <span className="min-w-0 truncate" title={value}>
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
  onManage,
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

  const statItems: StatBlockProps[] = [
    { label: "Scope", value: event.scopeLabel || "—" },
    { label: "Strategy", value: pacingSummary(event) },
    // Order only applies to a paced run — under "all at once" there's no
    // first/last, so omit it (mirrors the config control hiding the field).
    ...(event.strategy === "allAtOnce" ? [] : [{ label: "Order", value: orderLabels[event.order] }]),
    { label: "Est. time remaining", value: etaValue },
  ];

  // Progress summary + elapsed live in the progress section (curtailment's
  // ProgressSection precedent) — NOT as stat-block sub-details.
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

        {/* Stat lockups: in the modal (embedded) they read as standard
            label/value table rows; in the standalone card they use the same
            multi-column stat grid as ActiveCurtailmentStatus (grid-cols-5,
            gap-x-12). */}
        {embedded ? (
          <div className="mt-10 flex flex-col">
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
          <div className="mt-12 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-5">
            {statItems.map((item) => (
              <StatBlock key={item.label} label={item.label} value={item.value} detail={item.detail} />
            ))}
          </div>
        )}

        {/* Performance vs baseline (Option A): current hashrate / power /
            efficiency with a Δ-vs-baseline chip, so the operator can confirm
            the change isn't hurting the acted-on cohort — most consequential at
            the pilot-review gate. Renders only when a baseline was captured. */}
        <PerformanceStrip event={event} />

        {/* Progress section mirrors ActiveCurtailmentStatus' ProgressSection:
            summary line on the left + right-aligned elapsed above the bar,
            then the CompositionBar, then the legend. 24px (mt-6) above, matching
            the tightened content-grouping rhythm from the design review. */}
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
            {/* Excluded targets are never in the bar, so the legend annotates
                them separately (right-aligned) — the analog of curtailment's
                "N unavailable" annotation in ProgressSection. */}
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
