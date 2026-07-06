import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import {
  type ActiveCurtailmentCurtailProgress,
  type ActiveCurtailmentDisplayState,
  type CurtailmentEventState,
  type CurtailmentTargetRollup,
  formatCurtailmentElapsedDuration,
  formatCurtailmentKw as formatKw,
  formatCurtailmentMinerCount as formatMinerCount,
  getActiveCurtailmentCurtailProgress,
  getActiveCurtailmentDisplayState,
  getActiveCurtailmentMinerCompliance,
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
  startedAt?: string;
  endedAt?: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  observedReductionKw: number;
  remainingPowerKw?: number;
  // Curtail dispatch pacing for the rough time-to-curtail estimate; absent
  // when the event has no explicit batch size (reconciler-side defaults).
  curtailBatchSize?: number;
  curtailBatchIntervalSec?: number;
  // Configured restore wave size; 0 means "up to the safety limit" per wave,
  // matching the reconciler's restore claim sizing.
  restoreBatchSize: number;
  restoreBatchIntervalSec: number;
  rollups: CurtailmentTargetRollup[];
}

export interface ActiveCurtailmentTargetSiteCoverage {
  complete: boolean;
  targetCount: number;
  mappedTargetCount: number;
  unknownTargetCount: number;
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
  isRestored: boolean;
  isRestoreIncomplete: boolean;
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

const manageableDisplayStates = new Set<ActiveCurtailmentDisplayState>(["curtailed", "curtailing", "pending"]);
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

function formatActivePowerValue({ isRestored, isRestoreIncomplete, targetKw }: FormatActivePowerValueArgs): string {
  if (isRestored) {
    return `${formatKw(targetKw)} restored`;
  }

  if (isRestoreIncomplete) {
    return `${formatKw(targetKw)} restore requested`;
  }

  return formatKw(targetKw);
}

function getPowerLabel(displayFlags: ActiveCurtailmentDisplayFlags): string {
  if (displayFlags.isRestored) {
    return "Power restored";
  }

  if (displayFlags.isRestoreFlow) {
    return "Power to restore";
  }

  return "Power to shed";
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
const curtailProgressColorMap = {
  OK: "bg-core-primary-fill",
  WARNING: "bg-core-accent-fill",
  NA: "bg-core-primary-10",
} as const;

// Ticks once per second so the SLA-facing elapsed readout moves even when
// polling snapshots are unchanged (equal snapshots skip re-renders). Lives in
// its own component so the per-second tick re-renders only this stat block,
// not the whole card.
function ElapsedStatBlock({ startedAt }: { startedAt: string }): ReactElement | null {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    const intervalId = setInterval(() => setNowMs(Date.now()), millisecondsPerSecond);
    return () => clearInterval(intervalId);
  }, []);

  const startedDate = getDateTime(startedAt);
  if (!startedDate) {
    return null;
  }

  const elapsedSeconds = Math.max((nowMs - startedDate.getTime()) / millisecondsPerSecond, 0);
  return <StatBlock label="Elapsed" value={formatCurtailmentElapsedDuration(elapsedSeconds)} />;
}

function shouldShowCurtailProgress(
  displayState: ActiveCurtailmentDisplayState,
  progress: ActiveCurtailmentCurtailProgress,
): boolean {
  // dispatchableCount > 0 doubles as the live-data gate: rollup-less events
  // (old servers, narrowed whole-org reads) and zeroed rollups derive an
  // all-zero progress shape and keep today's card unchanged.
  return curtailProgressDisplayStates.has(displayState) && progress.dispatchableCount > 0;
}

// Rough time to finish dispatching sleep commands: remaining pending targets
// paced by the event's curtail batch settings (issue #660's approximation,
// ceil(pending / batch) x interval). Deliberately not the restore-estimate
// gap math: pending batches all wait on future reconciler waves, so the
// estimate must stay non-zero while any target is pending.
function getCurtailRemainingSeconds(
  event: Pick<ActiveCurtailmentEvent, "curtailBatchSize" | "curtailBatchIntervalSec">,
  progress: ActiveCurtailmentCurtailProgress,
): number {
  const batchSize = event.curtailBatchSize ?? 0;
  const intervalSec = event.curtailBatchIntervalSec ?? 0;
  if (progress.pendingCount <= 0 || batchSize <= 0 || intervalSec <= 0) {
    return 0;
  }
  return Math.ceil(progress.pendingCount / batchSize) * intervalSec;
}

function getCurtailProgressSegments(progress: ActiveCurtailmentCurtailProgress): Segment[] {
  return [
    { name: "Confirmed quiet", status: "OK", count: progress.confirmedCount },
    { name: "Command sent", status: "WARNING", count: progress.sentCount },
    { name: "Pending", status: "NA", count: progress.pendingCount },
  ];
}

interface CurtailProgressSectionProps {
  progress: ActiveCurtailmentCurtailProgress;
}

function CurtailProgressSection({ progress }: CurtailProgressSectionProps): ReactElement {
  const segments = getCurtailProgressSegments(progress);
  const reachedSummary = `${progress.reachedCount.toLocaleString()} of ${formatMinerCount(
    progress.dispatchableCount,
  )} reached (${progress.percent}%)`;

  return (
    <div className="mt-8 grid gap-3" data-testid="active-curtailment-progress">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <div className="text-200 text-text-primary-50">Curtail progress</div>
        <div className="text-emphasis-200 text-text-primary">{reachedSummary}</div>
      </div>
      <CompositionBar segments={segments} height={12} colorMap={curtailProgressColorMap} />
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-200 text-text-primary-70">
        {segments.map((segment) => (
          <span key={segment.name} className="flex items-center gap-2">
            <span
              className={clsx(
                "inline-block h-2 w-2 shrink-0 rounded-full",
                curtailProgressColorMap[segment.status as keyof typeof curtailProgressColorMap],
              )}
            />
            {`${segment.name} (${(segment.count ?? 0).toLocaleString()})`}
          </span>
        ))}
        {progress.unavailableCount > 0 ? (
          <span className="text-text-primary-50">
            {progress.unavailableCount.toLocaleString()} unavailable (excluded)
          </span>
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
    return `${unknownCount.toLocaleString()} ${targetLabel} no longer ${verb} to a known site. Org admins can still stop or abort this event.`;
  }

  return "Some targets no longer map to a known site. Org admins can still stop or abort this event.";
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
        <Button variant={variants.danger} size={sizes.compact} text="Stop" onClick={onRequestStop} />
      ) : null;
    case "restoring":
      return onRequestTerminateRecovery ? (
        <Button
          variant={variants.secondaryDanger}
          size={sizes.compact}
          text="Terminate recovery"
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
  const forceReleaseButton = getForceReleaseButton(displayState, onRequestForceRelease);

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
    isRestored: displayFlags.isRestored,
    isRestoreIncomplete: displayFlags.isRestoreIncomplete,
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
            <div className="text-heading-50 text-text-primary-70">Dispatch status</div>
            <div className="text-heading-300 text-text-primary">{dispatchStatus}</div>
          </div>
        </div>

        <div className="mt-12 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
          <StatBlock label={powerLabel} value={powerValue} />
          {displayFlags.isRestoreFlow ? (
            <>
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
              <StatBlock label="Applies to" value={formatMinerCount(event.selectedMiners)} />
              <StatBlock label="Restore" value={formatRestoreProfile(event)} />
              {showCurtailProgress && event.startedAt ? <ElapsedStatBlock startedAt={event.startedAt} /> : null}
              {curtailRemainingSeconds > 0 ? (
                <StatBlock label="Est. time to curtail" value={formatDurationLong(curtailRemainingSeconds)} />
              ) : null}
            </>
          )}
        </div>

        {showCurtailProgress ? <CurtailProgressSection progress={curtailProgress} /> : null}

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
