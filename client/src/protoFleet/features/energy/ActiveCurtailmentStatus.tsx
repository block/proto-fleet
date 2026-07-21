import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import {
  type ActiveCurtailmentCurtailProgress,
  type ActiveCurtailmentDisplayState,
  type ActiveCurtailmentRestoreProgress,
  type CurtailmentEventState,
  type CurtailmentTargetRollup,
  formatCurtailmentAppliesToSummary,
  formatCurtailmentElapsedDuration,
  formatCurtailmentKw as formatKw,
  formatCurtailmentMinerCount as formatMinerCount,
  getActiveCurtailmentCurtailProgress,
  getActiveCurtailmentDisplayState,
  getActiveCurtailmentMinerCompliance,
  getActiveCurtailmentRestoreProgress,
  getCurtailmentTargetKw as getTargetKw,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

export interface ActiveCurtailmentEvent {
  reason: string;
  state: CurtailmentEventState;
  scopeLabel: string;
  sourceLabel: string;
  isAutomationOwned: boolean;
  targetSiteCoverage?: ActiveCurtailmentTargetSiteCoverage;
  createdAt?: string;
  scheduledStartAt?: string;
  startedAt?: string;
  endedAt?: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  observedReductionKw: number;
  remainingPowerKw?: number;
  facilityFanDeviceCount?: number;
  fanOffSentAt?: string;
  fanOnSentAt?: string;
  fanLastError?: string;
  // Curtail dispatch pacing for the rough time-to-curtail estimate; absent
  // when the event has no explicit batch size (reconciler-side defaults).
  curtailBatchSize?: number;
  curtailBatchIntervalSec?: number;
  // Configured restore wave size; 0 means "up to the safety limit" per wave,
  // matching the reconciler's restore claim sizing.
  restoreBatchSize: number;
  restoreBatchIntervalSec: number;
  rollups: CurtailmentTargetRollup[];
  unavailableReasonCounts?: ActiveCurtailmentUnavailableReasonCount[];
}

export interface ActiveCurtailmentTargetSiteCoverage {
  complete: boolean;
  targetCount: number;
  mappedTargetCount: number;
  unknownTargetCount: number;
}

export interface ActiveCurtailmentUnavailableReasonCount {
  label: string;
  count: number;
}

interface ActiveCurtailmentStatusProps {
  event: ActiveCurtailmentEvent;
  className?: string;
  onDismissRestored?: () => void;
  onRequestEdit?: () => void;
  onRequestForceRelease?: () => void;
  onRequestRestore?: () => void;
  onRequestStop?: () => void;
  onRequestTerminateRecovery?: () => void;
}

interface ActiveCurtailmentActionButtonsProps {
  displayState: ActiveCurtailmentDisplayState;
  onDismissRestored?: () => void;
  onRequestEdit?: () => void;
  onRequestForceRelease?: () => void;
  onRequestRestore?: () => void;
  onRequestStop?: () => void;
  onRequestTerminateRecovery?: () => void;
}

interface SectionHeaderProps {
  title: string;
  children?: ReactNode;
}

interface StatBlockProps {
  label: string;
  value: string;
  detail?: string;
}

interface FormatActivePowerValueArgs {
  isRestoreFlow: boolean;
  observedReductionKw: number;
  targetKw: number;
}

interface RestoreEstimateArgs {
  selectedMinerCount: number;
  restoreBatchSize: number;
  restoreBatchIntervalSec: number;
}

interface RestoreTimeValueArgs {
  isRestored: boolean;
  remainingRestoreSeconds: number;
  totalRestoreSeconds: number;
}

interface StatusIconArgs {
  isCurtailmentComplete: boolean;
  isTerminalFailure: boolean;
  isRestored: boolean;
  isRestoreIncomplete: boolean;
}

interface ActiveCurtailmentDisplayFlags {
  isCurtailmentComplete: boolean;
  isRestored: boolean;
  isRestoreIncomplete: boolean;
  isRestoring: boolean;
  isRestoreFlow: boolean;
  isTerminalFailure: boolean;
}

const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});
const millisecondsPerSecond = 1000;
const unavailableTimeLabel = "Time unavailable";

const displayStateLabels: Record<ActiveCurtailmentDisplayState, string> = {
  cancelled: "Cancelled",
  curtailed: "Curtailed",
  curtailing: "Curtailing",
  failed: "Failed",
  pending: "Pending",
  restoreIncomplete: "Restore incomplete",
  restored: "Restored",
  restoring: "Restoring",
};

function getInfrastructureStatus(event: ActiveCurtailmentEvent, isRestoreFlow: boolean): string | null {
  if ((event.facilityFanDeviceCount ?? 0) <= 0) {
    return null;
  }

  if (event.fanLastError) {
    return isRestoreFlow ? "Restore failed" : "Curtail failed";
  }

  if (isRestoreFlow) {
    return event.fanOnSentAt ? "Restored" : "Restoring";
  }

  return event.fanOffSentAt ? "Curtailed" : "Curtailing";
}

const manageableDisplayStates = new Set<ActiveCurtailmentDisplayState>(["curtailed", "curtailing", "pending"]);
const forceReleaseDisplayStates = new Set<ActiveCurtailmentDisplayState>([
  "curtailed",
  "curtailing",
  "pending",
  "restoring",
]);
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

function getDateTime(value?: string): Date | undefined {
  if (!value) {
    return undefined;
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function formatDateTimeValue(date: Date): string {
  return dateTimeFormatter.format(date);
}

function formatDateTime(value?: string): string {
  const date = getDateTime(value);
  return date ? formatDateTimeValue(date) : unavailableTimeLabel;
}

function formatEstimatedCompletion(remainingSeconds: number, currentTime = new Date()): string {
  if (!Number.isFinite(remainingSeconds)) {
    return unavailableTimeLabel;
  }

  const currentTimeMs = currentTime.getTime();
  const estimatedCompletionMs = currentTimeMs + Math.max(remainingSeconds, 0) * millisecondsPerSecond;

  if (!Number.isFinite(currentTimeMs) || !Number.isFinite(estimatedCompletionMs)) {
    return unavailableTimeLabel;
  }

  const estimatedCompletionDate = new Date(estimatedCompletionMs);
  return Number.isNaN(estimatedCompletionDate.getTime())
    ? unavailableTimeLabel
    : formatDateTimeValue(estimatedCompletionDate);
}

function formatKwMagnitude(value: number): string {
  const finiteValue = Number.isFinite(value) ? value : 0;

  return finiteValue.toLocaleString(undefined, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  });
}

function formatPowerProgressValue(currentKw: number, targetKw: number): string {
  return `${formatKwMagnitude(currentKw)} of ${formatKw(targetKw)}`;
}

function formatActivePowerValue({ isRestoreFlow, observedReductionKw, targetKw }: FormatActivePowerValueArgs): string {
  if (isRestoreFlow) {
    return formatPowerProgressValue(Math.max(targetKw - observedReductionKw, 0), targetKw);
  }

  return formatPowerProgressValue(observedReductionKw, targetKw);
}

function getPowerLabel(displayFlags: ActiveCurtailmentDisplayFlags): string {
  if (displayFlags.isRestoreFlow) {
    return "Power restored";
  }

  return "Power shed";
}

function formatDurationLong(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds <= 0) {
    return "Immediate";
  }

  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  const parts: string[] = [];

  if (minutes > 0) {
    parts.push(`${minutes.toLocaleString()} ${minutes === 1 ? "minute" : "minutes"}`);
  }

  if (seconds > 0) {
    parts.push(`${seconds.toLocaleString()} ${seconds === 1 ? "second" : "seconds"}`);
  }

  return parts.join(", ");
}

function getRestoreEstimateSeconds({
  selectedMinerCount,
  restoreBatchSize,
  restoreBatchIntervalSec,
}: RestoreEstimateArgs): number {
  if (
    !Number.isFinite(selectedMinerCount) ||
    !Number.isFinite(restoreBatchSize) ||
    !Number.isFinite(restoreBatchIntervalSec) ||
    selectedMinerCount <= 0 ||
    restoreBatchSize <= 0 ||
    restoreBatchIntervalSec <= 0
  ) {
    return 0;
  }

  const batchCount = Math.ceil(selectedMinerCount / restoreBatchSize);
  return Math.max(batchCount - 1, 0) * restoreBatchIntervalSec;
}

function getRestoreRemainingSeconds(
  event: ActiveCurtailmentEvent,
  restoredCount: number,
  restoreFailedCount: number,
  totalCount: number,
): number {
  const remainingMiners = Math.max(totalCount - restoredCount - restoreFailedCount, 0);

  return getRestoreEstimateSeconds({
    selectedMinerCount: remainingMiners,
    restoreBatchSize: event.restoreBatchSize,
    restoreBatchIntervalSec: event.restoreBatchIntervalSec,
  });
}

function formatRestoreTimeValue({
  isRestored,
  remainingRestoreSeconds,
  totalRestoreSeconds,
}: RestoreTimeValueArgs): string {
  if (isRestored) {
    return formatDurationLong(totalRestoreSeconds);
  }

  return formatDurationLong(remainingRestoreSeconds);
}

function getDisplayFlags(displayState: ActiveCurtailmentDisplayState): ActiveCurtailmentDisplayFlags {
  const isRestored = displayState === "restored";
  const isRestoreIncomplete = displayState === "restoreIncomplete";
  const isRestoring = displayState === "restoring";
  const isTerminalFailure = displayState === "cancelled" || displayState === "failed";

  return {
    isCurtailmentComplete: displayState === "curtailed",
    isRestored,
    isRestoreIncomplete,
    isRestoring,
    isRestoreFlow: isRestoring || isRestored || isRestoreIncomplete,
    isTerminalFailure,
  };
}

const curtailProgressDisplayStates = new Set<ActiveCurtailmentDisplayState>(["pending", "curtailing", "curtailed"]);
const restoreProgressDisplayStates = new Set<ActiveCurtailmentDisplayState>([
  "restoring",
  "restored",
  "restoreIncomplete",
]);
const curtailProgressColorMap: Record<Segment["status"], string> = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-core-accent-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-primary-10",
};
const restoreProgressColorMap: Record<Segment["status"], string> = {
  ...curtailProgressColorMap,
  OK: "bg-intent-success-fill",
  NA: "bg-core-primary-fill",
};

// Ticks once per second so the SLA-facing elapsed readout moves even when
// polling snapshots are unchanged (equal snapshots skip re-renders). Lives in
// its own component so the per-second tick re-renders only this progress
// header value, not the whole card.
function ElapsedProgressValue({ since, until }: { since: string; until?: string }): ReactElement | null {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    if (until) {
      return undefined;
    }

    const intervalId = setInterval(() => setNowMs(Date.now()), millisecondsPerSecond);
    return () => clearInterval(intervalId);
  }, [until]);

  const sinceDate = getDateTime(since);
  if (!sinceDate) {
    return null;
  }

  const untilDate = until ? getDateTime(until) : undefined;
  const endMs = untilDate?.getTime() ?? nowMs;
  const elapsedSeconds = Math.max((endMs - sinceDate.getTime()) / millisecondsPerSecond, 0);
  return (
    <div className="text-right text-200 text-text-primary">
      {formatCurtailmentElapsedDuration(elapsedSeconds)} elapsed
    </div>
  );
}

// SLA clock anchor. started_at is only stamped at the pending -> active
// transition — after targets confirm for open-loop events — which is too
// late for a timer covering the dispatch window. Fall back to the scheduled
// window start, then creation time ("when the operator pressed go"). The
// progress gate keeps this from rendering before any targets exist, so a
// scheduled event's pre-window wait never shows as elapsed time.
function getElapsedAnchor(
  event: Pick<ActiveCurtailmentEvent, "startedAt" | "scheduledStartAt" | "createdAt">,
): string | undefined {
  return event.startedAt ?? event.scheduledStartAt ?? event.createdAt;
}

function shouldShowCurtailProgress(
  displayState: ActiveCurtailmentDisplayState,
  progress: ActiveCurtailmentCurtailProgress,
): boolean {
  // dispatchableCount keeps rollup-less events hidden, while unavailableCount
  // lets all-unavailable live rollups still explain why no targets can move.
  return (
    curtailProgressDisplayStates.has(displayState) && (progress.dispatchableCount > 0 || progress.unavailableCount > 0)
  );
}

function shouldShowRestoreProgress(
  displayState: ActiveCurtailmentDisplayState,
  progress: ActiveCurtailmentRestoreProgress,
): boolean {
  // Same live-data gate as the curtail bar, keyed on the restorable total.
  return restoreProgressDisplayStates.has(displayState) && progress.restorableCount > 0;
}

// Rough time to finish dispatching sleep commands: remaining pending targets
// paced by the event's curtail batch settings. Before anything has been
// dispatched, the reconciler sends the first wave without waiting on the
// interval clock (curtailBatchIntervalElapsed is vacuously true), so that
// wave is free — matching the plan preview's (batches - 1) x interval math.
// Once any wave is out, every pending wave waits on the interval from the
// previous dispatch, so all of them are charged. Drifted targets count as
// prior-dispatch evidence too: they necessarily carry a CurtailPhase
// DispatchedAt, which is what the reconciler's interval gate checks.
function getCurtailRemainingSeconds(
  event: Pick<ActiveCurtailmentEvent, "curtailBatchSize" | "curtailBatchIntervalSec">,
  progress: ActiveCurtailmentCurtailProgress,
): number {
  const batchSize = event.curtailBatchSize ?? 0;
  const intervalSec = event.curtailBatchIntervalSec ?? 0;
  if (progress.pendingCount <= 0 || batchSize <= 0 || intervalSec <= 0) {
    return 0;
  }
  const hasPriorDispatch = progress.reachedCount > 0 || progress.driftedCount > 0;
  const pendingWaves = Math.ceil(progress.pendingCount / batchSize);
  const chargedWaves = hasPriorDispatch ? pendingWaves : pendingWaves - 1;
  return Math.max(chargedWaves, 0) * intervalSec;
}

function getCurtailProgressSegments(progress: ActiveCurtailmentCurtailProgress): Segment[] {
  if (progress.dispatchableCount <= 0) {
    return [];
  }

  return [
    { name: "Curtailed", status: "OK", count: progress.confirmedCount },
    {
      name: "Curtailing",
      status: "WARNING",
      count: progress.sentCount + progress.pendingCount + progress.driftedCount,
    },
  ];
}

function getCurtailProgressSummary(progress: ActiveCurtailmentCurtailProgress): string {
  if (progress.dispatchableCount <= 0 && progress.unavailableCount > 0) {
    return "No dispatchable miners";
  }

  if (progress.percent >= 100) {
    return `${formatMinerCount(progress.confirmedCount)} curtailed (${progress.percent}%)`;
  }

  return `${progress.confirmedCount.toLocaleString()} of ${formatMinerCount(
    progress.dispatchableCount,
  )} curtailed (${progress.percent}%)`;
}

function getRestoreProgressSummary(progress: ActiveCurtailmentRestoreProgress): string {
  if (progress.percent >= 100) {
    return `${formatMinerCount(progress.restoredCount)} restored (${progress.percent}%)`;
  }

  return `${progress.restoredCount.toLocaleString()} of ${formatMinerCount(
    progress.restorableCount,
  )} restored (${progress.percent}%)`;
}

function getRestoreProgressSegments(progress: ActiveCurtailmentRestoreProgress): Segment[] {
  const segments: Segment[] = [
    { name: "Restored", status: "OK", count: progress.restoredCount },
    { name: "Curtailed", status: "NA", count: progress.awaitingCount },
  ];

  if (progress.failedCount > 0) {
    segments.push({ name: "Failed to restore", status: "CRITICAL", count: progress.failedCount });
  }

  return segments;
}

interface ProgressSectionProps {
  summary: string;
  segments: Segment[];
  colorMap: Record<Segment["status"], string>;
  elapsedAnchor?: string;
  elapsedUntil?: string;
  unavailableCount: number;
  unavailableReasonCounts?: ActiveCurtailmentUnavailableReasonCount[];
}

function formatUnavailableAnnotation(
  unavailableCount: number,
  unavailableReasonCounts?: ActiveCurtailmentUnavailableReasonCount[],
): string | null {
  if (unavailableCount <= 0) {
    return null;
  }

  const reasonTotal = unavailableReasonCounts?.reduce((total, reason) => total + reason.count, 0) ?? 0;
  if (unavailableReasonCounts?.length && reasonTotal === unavailableCount) {
    const reasonSummary = unavailableReasonCounts
      .map((reason) => `${reason.count.toLocaleString()} ${reason.label}`)
      .join(", ");
    return `${unavailableCount.toLocaleString()} unavailable (${reasonSummary})`;
  }

  return `${unavailableCount.toLocaleString()} unavailable (details unavailable)`;
}

function ProgressSection({
  summary,
  segments,
  colorMap,
  elapsedAnchor,
  elapsedUntil,
  unavailableCount,
  unavailableReasonCounts,
}: ProgressSectionProps): ReactElement {
  const unavailableAnnotation = formatUnavailableAnnotation(unavailableCount, unavailableReasonCounts);
  const hasPositiveSegments = segments.some((segment) => (segment.count ?? 0) > 0);

  return (
    <div className="mt-8 grid gap-3" data-testid="active-curtailment-progress">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
        <div className="text-200 text-text-primary-50">{summary}</div>
        {elapsedAnchor ? <ElapsedProgressValue since={elapsedAnchor} until={elapsedUntil} /> : null}
      </div>
      {hasPositiveSegments ? <CompositionBar segments={segments} height={12} colorMap={colorMap} /> : null}
      <div className="flex flex-wrap items-start gap-x-5 gap-y-1 text-200 text-text-primary-70">
        {segments.map((segment) => (
          <span key={segment.name} className="flex items-start gap-2">
            <span className={clsx("mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full", colorMap[segment.status])} />
            {`${segment.name} (${(segment.count ?? 0).toLocaleString()})`}
          </span>
        ))}
        {unavailableAnnotation ? (
          <span className="ml-auto text-right text-text-primary-50">{unavailableAnnotation}</span>
        ) : null}
      </div>
    </div>
  );
}

function formatRestoreProfile(
  event: Pick<ActiveCurtailmentEvent, "restoreBatchSize" | "restoreBatchIntervalSec">,
): string {
  if (event.restoreBatchIntervalSec === 0) {
    if (event.restoreBatchSize === 0) {
      return "Up to safety limit immediately";
    }
    return `${formatMinerCount(event.restoreBatchSize)} with no wait`;
  }
  if (event.restoreBatchSize === 0) {
    return `Up to safety limit every ${event.restoreBatchIntervalSec.toLocaleString()}s`;
  }
  return `${formatMinerCount(event.restoreBatchSize)} every ${event.restoreBatchIntervalSec.toLocaleString()}s`;
}

function formatActiveCurtailmentHeaderDetail(event: ActiveCurtailmentEvent): string {
  return `${event.reason} (Applies to ${event.scopeLabel})`;
}

function formatIncompleteSiteCoverageWarning(coverage?: ActiveCurtailmentTargetSiteCoverage): string | null {
  if (!coverage || coverage.complete) {
    return null;
  }

  const unknownCount = coverage.unknownTargetCount;
  const targetLabel = unknownCount === 1 ? "target" : "targets";
  const verb = unknownCount === 1 ? "maps" : "map";
  if (unknownCount > 0) {
    return `${unknownCount.toLocaleString()} ${targetLabel} no longer ${verb} to a known site. Org admins can still restore or abort this event.`;
  }

  return "Some targets no longer map to a known site. Org admins can still restore or abort this event.";
}

function getForceReleaseButton(
  displayState: ActiveCurtailmentDisplayState,
  onRequestForceRelease?: () => void,
): ReactElement | null {
  const label = displayState === "restoring" ? "Abort restore" : "Abort curtailment";
  return onRequestForceRelease ? (
    <Button variant={variants.danger} size={sizes.compact} text={label} onClick={onRequestForceRelease} />
  ) : null;
}

function getActiveCurtailmentActionButton({
  displayState,
  onDismissRestored,
  onRequestRestore,
  onRequestStop,
  onRequestTerminateRecovery,
}: ActiveCurtailmentActionButtonsProps): ReactElement | null {
  switch (displayState) {
    case "restored":
    case "restoreIncomplete":
      return onDismissRestored ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Dismiss" onClick={onDismissRestored} />
      ) : null;
    case "cancelled":
    case "failed":
      return null;
    case "curtailed":
      return onRequestRestore ? (
        <Button variant={variants.primary} size={sizes.compact} text="Restore" onClick={onRequestRestore} />
      ) : null;
    case "pending":
    case "curtailing":
      return onRequestStop ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Restore now" onClick={onRequestStop} />
      ) : null;
    case "restoring":
      return onRequestTerminateRecovery ? (
        <Button
          variant={variants.secondaryDanger}
          size={sizes.compact}
          text="Stop restore"
          onClick={onRequestTerminateRecovery}
        />
      ) : null;
  }
}

function ActiveCurtailmentActionButtons({
  displayState,
  onDismissRestored,
  onRequestEdit,
  onRequestForceRelease,
  onRequestRestore,
  onRequestStop,
  onRequestTerminateRecovery,
}: ActiveCurtailmentActionButtonsProps): ReactElement | null {
  const actionButton = getActiveCurtailmentActionButton({
    displayState,
    onDismissRestored,
    onRequestRestore,
    onRequestStop,
    onRequestTerminateRecovery,
  });
  const showManageButton = Boolean(onRequestEdit && manageableDisplayStates.has(displayState));
  const forceReleaseButton =
    !forceReleaseDisplayStates.has(displayState) || (displayState === "restoring" && onRequestTerminateRecovery)
      ? null
      : getForceReleaseButton(displayState, onRequestForceRelease);

  if (!actionButton && !forceReleaseButton && !showManageButton) {
    return null;
  }

  return (
    <div className="mb-8 flex shrink-0 justify-end gap-3 tablet:absolute tablet:top-10 tablet:right-10 tablet:mb-0">
      {showManageButton ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Manage" onClick={onRequestEdit} />
      ) : null}
      {actionButton}
      {forceReleaseButton}
    </div>
  );
}

function getActiveCurtailmentStatusIcon({
  isTerminalFailure,
  isRestored,
  isRestoreIncomplete,
  isCurtailmentComplete,
}: StatusIconArgs): ReactNode {
  if (isRestoreIncomplete || isTerminalFailure) {
    return <Alert className="text-intent-critical-fill" />;
  }

  if (isRestored) {
    return <Success className="text-intent-success-fill" />;
  }

  if (isCurtailmentComplete) {
    return <Success className="text-core-primary-fill" />;
  }

  return <ProgressCircular indeterminate className="text-core-primary-fill" />;
}

export default function ActiveCurtailmentStatus({
  event,
  className,
  onDismissRestored,
  onRequestEdit,
  onRequestForceRelease,
  onRequestRestore,
  onRequestStop,
  onRequestTerminateRecovery,
}: ActiveCurtailmentStatusProps): ReactElement {
  const targetKw = getTargetKw(event);
  const compliance = getActiveCurtailmentMinerCompliance(event);
  const displayState = getActiveCurtailmentDisplayState(event, { dispatchStartedAsCurtailing: true });
  const displayFlags = getDisplayFlags(displayState);
  const curtailProgress = getActiveCurtailmentCurtailProgress(event);
  const showCurtailProgress = shouldShowCurtailProgress(displayState, curtailProgress);
  const restoreProgress = getActiveCurtailmentRestoreProgress(event);
  const showRestoreProgress = shouldShowRestoreProgress(displayState, restoreProgress);
  const elapsedAnchor = showCurtailProgress || showRestoreProgress ? getElapsedAnchor(event) : undefined;
  // "Curtailed" means the shed goal is met, so pairing it with a time-to-
  // curtail estimate would contradict the headline state.
  const curtailRemainingSeconds =
    showCurtailProgress && displayState !== "curtailed" ? getCurtailRemainingSeconds(event, curtailProgress) : 0;
  const remainingRestoreSeconds = getRestoreRemainingSeconds(
    event,
    compliance.restoredCount,
    compliance.restoreFailedCount,
    compliance.totalCount,
  );
  const estimatedCompletion = formatEstimatedCompletion(remainingRestoreSeconds);
  const totalRestoreSeconds = getRestoreEstimateSeconds({
    selectedMinerCount: compliance.totalCount,
    restoreBatchSize: event.restoreBatchSize,
    restoreBatchIntervalSec: event.restoreBatchIntervalSec,
  });
  const powerLabel = getPowerLabel(displayFlags);
  const powerValue = formatActivePowerValue({
    isRestoreFlow: displayFlags.isRestoreFlow,
    observedReductionKw: event.observedReductionKw,
    targetKw,
  });
  const dispatchStatus = displayStateLabels[displayState];
  const isTerminalRestoreFlow = displayFlags.isRestored || displayFlags.isRestoreIncomplete;
  const restoreTimeLabel = isTerminalRestoreFlow ? "Time to restore" : "Estimated time to restore";
  const restoreTimeValue = formatRestoreTimeValue({
    isRestored: isTerminalRestoreFlow,
    remainingRestoreSeconds,
    totalRestoreSeconds,
  });
  const restoreCompletionLabel = displayFlags.isRestored ? "Completed" : "Estimated completion";
  const restoreCompletionValue =
    displayFlags.isRestored || event.endedAt ? formatDateTime(event.endedAt) : estimatedCompletion;
  const shouldRenderRestoreCompletion =
    displayFlags.isRestored ||
    Boolean(event.endedAt) ||
    (remainingRestoreSeconds > 0 && estimatedCompletion !== unavailableTimeLabel);
  const restoreFailureValue = formatMinerCount(compliance.restoreFailedCount);
  const statusIcon = getActiveCurtailmentStatusIcon({
    isTerminalFailure: displayFlags.isTerminalFailure,
    isRestored: displayFlags.isRestored,
    isRestoreIncomplete: displayFlags.isRestoreIncomplete,
    isCurtailmentComplete: displayFlags.isCurtailmentComplete,
  });
  const incompleteSiteCoverageWarning = formatIncompleteSiteCoverageWarning(event.targetSiteCoverage);
  const infrastructureStatus = getInfrastructureStatus(event, displayFlags.isRestoreFlow);

  return (
    <section className={clsx("grid gap-3", className)}>
      <SectionHeader title="Active curtailment">
        <div className="max-w-xl">
          <div className="text-emphasis-300">{formatActiveCurtailmentHeaderDetail(event)}</div>
        </div>
      </SectionHeader>

      <div className="relative rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10">
        <ActiveCurtailmentActionButtons
          displayState={displayState}
          onDismissRestored={onDismissRestored}
          onRequestEdit={onRequestEdit}
          onRequestForceRelease={onRequestForceRelease}
          onRequestRestore={onRequestRestore}
          onRequestStop={onRequestStop}
          onRequestTerminateRecovery={onRequestTerminateRecovery}
        />

        <div className="grid gap-3 tablet:pr-32">
          <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">{statusIcon}</div>
          <div data-testid="active-curtailment-primary-lockup">
            <div className="text-heading-50 text-text-primary-70">{powerLabel}</div>
            <div className="text-heading-300 text-text-primary">{powerValue}</div>
          </div>
        </div>

        <div className="mt-12 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-5">
          <StatBlock label="Dispatch status" value={dispatchStatus} />
          {displayFlags.isRestoreFlow ? (
            <>
              {infrastructureStatus ? <StatBlock label="Infrastructure" value={infrastructureStatus} /> : null}
              <StatBlock label="Restore" value={formatRestoreProfile(event)} />
              <StatBlock label={restoreTimeLabel} value={restoreTimeValue} />
              {displayFlags.isRestoreIncomplete ? (
                <StatBlock label="Failed to restore" value={restoreFailureValue} />
              ) : shouldRenderRestoreCompletion ? (
                <StatBlock label={restoreCompletionLabel} value={restoreCompletionValue} />
              ) : null}
            </>
          ) : (
            <>
              <StatBlock
                label="Applies to"
                value={formatCurtailmentAppliesToSummary(event.selectedMiners, event.facilityFanDeviceCount)}
              />
              {infrastructureStatus ? <StatBlock label="Infrastructure" value={infrastructureStatus} /> : null}
              <StatBlock label="Restore" value={formatRestoreProfile(event)} />
              {curtailRemainingSeconds > 0 ? (
                <StatBlock label="Estimated time to curtail" value={formatDurationLong(curtailRemainingSeconds)} />
              ) : null}
            </>
          )}
        </div>

        {showCurtailProgress ? (
          <ProgressSection
            summary={getCurtailProgressSummary(curtailProgress)}
            segments={getCurtailProgressSegments(curtailProgress)}
            colorMap={curtailProgressColorMap}
            elapsedAnchor={elapsedAnchor}
            elapsedUntil={event.endedAt}
            unavailableCount={curtailProgress.unavailableCount}
            unavailableReasonCounts={event.unavailableReasonCounts}
          />
        ) : null}

        {showRestoreProgress ? (
          <ProgressSection
            summary={getRestoreProgressSummary(restoreProgress)}
            segments={getRestoreProgressSegments(restoreProgress)}
            colorMap={restoreProgressColorMap}
            elapsedAnchor={elapsedAnchor}
            elapsedUntil={event.endedAt}
            unavailableCount={restoreProgress.unavailableCount}
            unavailableReasonCounts={event.unavailableReasonCounts}
          />
        ) : null}

        {event.isAutomationOwned ? (
          <div className="mt-6 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
            <div className="text-emphasis-300">Curtailment automation recovery</div>
            <div className="mt-1 text-text-primary-70">
              {event.sourceLabel} owns this event. Abort cancels this event and disables the owning automation rule so
              it cannot immediately curtail miners again.
            </div>
          </div>
        ) : null}

        {incompleteSiteCoverageWarning ? (
          <div className="mt-6 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
            <div className="flex items-start gap-3">
              <Alert className="mt-0.5 shrink-0" />
              <div>
                <div className="text-emphasis-300">Target site coverage incomplete</div>
                <div className="mt-1 text-text-primary-70">{incompleteSiteCoverageWarning}</div>
              </div>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
