import { type ReactElement, type ReactNode } from "react";
import clsx from "clsx";

import type { CurtailmentEventState } from "@/protoFleet/features/energy/CurtailmentHistory";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

export type CurtailmentTargetState =
  | "pending"
  | "dispatched"
  | "confirmed"
  | "drifted"
  | "resolved"
  | "released"
  | "restoreFailed";

export interface CurtailmentTargetRollup {
  state: CurtailmentTargetState;
  count: number;
}

export interface ActiveCurtailmentEvent {
  reason: string;
  state: CurtailmentEventState;
  scopeLabel: string;
  endedAt?: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  observedReductionKw: number;
  remainingPowerKw?: number;
  restoreBatchSize: number;
  restoreBatchIntervalSec: number;
  rollups: CurtailmentTargetRollup[];
}

interface ActiveCurtailmentStatusProps {
  event: ActiveCurtailmentEvent;
  className?: string;
  onDismissRestored?: () => void;
  onRequestEdit?: () => void;
  onRequestRestore?: () => void;
  onRequestStop?: () => void;
}

interface ActiveCurtailmentActionButtonsProps {
  displayState: ActiveCurtailmentDisplayState;
  onDismissRestored?: () => void;
  onRequestEdit?: () => void;
  onRequestRestore?: () => void;
  onRequestStop?: () => void;
}

interface ActiveCurtailmentProgressBarProps {
  primaryClassName: string;
  primaryProgressPercent: number;
  secondaryClassName: string;
  secondaryProgressPercent: number;
  showSecondaryProgress: boolean;
}

interface DotProps {
  className: string;
}

interface ProgressLegendItemProps {
  dotClassName: string;
  label: string;
}

interface ProgressSegmentProps {
  className: string;
  percent: number;
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

interface MinerCompliance {
  curtailedCount: number;
  restoreFailedCount: number;
  restoredCount: number;
  totalCount: number;
}

type ActiveCurtailmentDisplayState =
  | "cancelled"
  | "curtailing"
  | "curtailed"
  | "failed"
  | "pending"
  | "restoring"
  | "restored"
  | "restoreIncomplete";

interface FormatActivePowerValueArgs {
  isRestored: boolean;
  isRestoreFlow: boolean;
  observedReductionKw: number;
  restoredKw: number;
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
  isPending: boolean;
  isRestored: boolean;
  isRestoreIncomplete: boolean;
  isRestoring: boolean;
  isRestoreFlow: boolean;
  isTerminalFailure: boolean;
}

interface ActiveCurtailmentLegend {
  primaryDotClassName: string;
  primaryLabel: string;
  secondaryDotClassName: string;
  secondaryLabel: string;
}

const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});
const millisecondsPerSecond = 1000;
const unavailableTimeLabel = "Time unavailable";

const countedTargetStates: CurtailmentTargetState[] = [
  "pending",
  "dispatched",
  "confirmed",
  "drifted",
  "resolved",
  "released",
  "restoreFailed",
];
const curtailedTargetStates: CurtailmentTargetState[] = ["confirmed", "resolved"];
const restoreFailedTargetStates: CurtailmentTargetState[] = ["restoreFailed"];
const restoredTargetStates: CurtailmentTargetState[] = ["resolved", "released"];

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

const manageableDisplayStates = new Set<ActiveCurtailmentDisplayState>([
  "curtailed",
  "curtailing",
  "pending",
  "restoring",
]);

function Dot({ className }: DotProps): ReactElement {
  return <span className={clsx("inline-block h-2 w-2 shrink-0 rounded-full", className)} />;
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

function getTargetKw(event: Pick<ActiveCurtailmentEvent, "targetKw" | "estimatedReductionKw">): number {
  return event.targetKw ?? event.estimatedReductionKw;
}

function formatKw(value: number, fractionDigits = 1): string {
  const finiteValue = Number.isFinite(value) ? value : 0;

  return `${finiteValue.toLocaleString(undefined, {
    maximumFractionDigits: fractionDigits,
    minimumFractionDigits: fractionDigits,
  })} kW`;
}

function formatMinerCount(minerCount: number): string {
  return `${minerCount.toLocaleString()} ${minerCount === 1 ? "miner" : "miners"}`;
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

function getProgressPercent(value: number, total: number): number {
  if (!Number.isFinite(value) || !Number.isFinite(total) || total <= 0) {
    return 0;
  }

  return Math.min(Math.max((value / total) * 100, 0), 100);
}

function getRollupCount(event: ActiveCurtailmentEvent, states: CurtailmentTargetState[]): number {
  return event.rollups.reduce((total, rollup) => {
    if (!states.includes(rollup.state)) {
      return total;
    }

    return total + rollup.count;
  }, 0);
}

function getMinerCompliance(event: ActiveCurtailmentEvent): MinerCompliance {
  const curtailedCount = getRollupCount(event, curtailedTargetStates);
  const restoreFailedCount = getRollupCount(event, restoreFailedTargetStates);
  const restoredCount = getRollupCount(event, restoredTargetStates);
  const countedTargetCount = getRollupCount(event, countedTargetStates);
  const totalCount = Math.max(event.selectedMiners, countedTargetCount);

  return {
    curtailedCount,
    restoreFailedCount,
    restoredCount,
    totalCount,
  };
}

function isRestoredEventState(state: CurtailmentEventState): boolean {
  return state === "completed";
}

function isRestoreIncompleteEventState(state: CurtailmentEventState): boolean {
  return state === "completedWithFailures";
}

function getActiveCurtailmentDisplayState(
  event: ActiveCurtailmentEvent,
  powerShedPercent: number,
  curtailedPercent: number,
): ActiveCurtailmentDisplayState {
  if (event.state === "restoring") {
    return "restoring";
  }

  if (event.state === "pending") {
    return "pending";
  }

  if (isRestoreIncompleteEventState(event.state)) {
    return "restoreIncomplete";
  }

  if (isRestoredEventState(event.state)) {
    return "restored";
  }

  if (event.state === "cancelled") {
    return "cancelled";
  }

  if (event.state === "failed") {
    return "failed";
  }

  return powerShedPercent >= 100 || curtailedPercent >= 100 ? "curtailed" : "curtailing";
}

function getRestoredPercent(event: ActiveCurtailmentEvent, restoredCount: number, totalCount: number): number {
  if (isRestoredEventState(event.state)) {
    return 100;
  }

  return getProgressPercent(restoredCount, totalCount);
}

function formatActivePowerValue({
  isRestored,
  isRestoreFlow,
  observedReductionKw,
  restoredKw,
  targetKw,
}: FormatActivePowerValueArgs): string {
  if (isRestored) {
    return `${formatKw(targetKw)} restored`;
  }

  if (isRestoreFlow) {
    return `${formatKw(restoredKw)} of ${formatKw(targetKw)} restored`;
  }

  const cappedObservedReductionKw = Math.min(Math.max(observedReductionKw, 0), targetKw);
  return `${formatKw(cappedObservedReductionKw)} of ${formatKw(targetKw)}`;
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
  const isPending = displayState === "pending";
  const isRestored = displayState === "restored";
  const isRestoreIncomplete = displayState === "restoreIncomplete";
  const isRestoring = displayState === "restoring";
  const isTerminalFailure = displayState === "cancelled" || displayState === "failed";

  return {
    isCurtailmentComplete: displayState === "curtailed",
    isPending,
    isRestored,
    isRestoreIncomplete,
    isRestoring,
    isRestoreFlow: isRestoring || isRestored || isRestoreIncomplete,
    isTerminalFailure,
  };
}

function getProgressLegend(displayFlags: ActiveCurtailmentDisplayFlags): ActiveCurtailmentLegend {
  if (displayFlags.isRestoreFlow) {
    return {
      primaryDotClassName: "bg-intent-success-fill",
      primaryLabel: "Restored",
      secondaryDotClassName: displayFlags.isRestoreIncomplete ? "bg-intent-critical-fill" : "bg-core-primary-fill",
      secondaryLabel: displayFlags.isRestoreIncomplete ? "Not restored" : "Curtailed",
    };
  }

  return {
    primaryDotClassName: "bg-core-primary-fill",
    primaryLabel: "Curtailed",
    secondaryDotClassName: "bg-core-accent-fill",
    secondaryLabel: displayFlags.isPending ? "Pending" : "Curtailing",
  };
}

function shouldShowSecondaryProgress(displayFlags: ActiveCurtailmentDisplayFlags): boolean {
  if (displayFlags.isTerminalFailure) {
    return false;
  }

  if (displayFlags.isRestoreFlow) {
    return !displayFlags.isRestored;
  }

  return !displayFlags.isCurtailmentComplete;
}

function formatRestoreProfile(
  event: Pick<ActiveCurtailmentEvent, "restoreBatchSize" | "restoreBatchIntervalSec">,
): string {
  return `${formatMinerCount(event.restoreBatchSize)} every ${event.restoreBatchIntervalSec.toLocaleString()}s`;
}

function formatRemainingPower(remainingPowerKw?: number): string {
  if (remainingPowerKw === undefined) {
    return "Unavailable";
  }

  return `${formatKw(remainingPowerKw)} remaining`;
}

function formatActiveCurtailmentHeaderDetail(event: ActiveCurtailmentEvent): string {
  return `${event.reason} (Applies to ${event.scopeLabel})`;
}

function getActiveCurtailmentActionButton({
  displayState,
  onDismissRestored,
  onRequestRestore,
  onRequestStop,
}: ActiveCurtailmentActionButtonsProps): ReactElement | null {
  switch (displayState) {
    case "restored":
      return onDismissRestored ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Dismiss" onClick={onDismissRestored} />
      ) : null;
    case "cancelled":
    case "failed":
    case "restoreIncomplete":
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
      return null;
  }
}

function ActiveCurtailmentActionButtons({
  displayState,
  onDismissRestored,
  onRequestEdit,
  onRequestRestore,
  onRequestStop,
}: ActiveCurtailmentActionButtonsProps): ReactElement | null {
  const actionButton = getActiveCurtailmentActionButton({
    displayState,
    onDismissRestored,
    onRequestRestore,
    onRequestStop,
  });
  const showManageButton = Boolean(onRequestEdit && manageableDisplayStates.has(displayState));

  if (!actionButton && !showManageButton) {
    return null;
  }

  return (
    <div className="mb-8 flex shrink-0 justify-end gap-3 tablet:absolute tablet:top-10 tablet:right-10 tablet:mb-0">
      {showManageButton ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Manage" onClick={onRequestEdit} />
      ) : null}
      {actionButton}
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

function ProgressSegment({ className, percent }: ProgressSegmentProps): ReactElement {
  return (
    <div
      className={clsx("rounded-full transition-[flex-basis,width] duration-700 ease-out", className)}
      style={{ flexBasis: `${percent}%` }}
    />
  );
}

function ActiveCurtailmentProgressBar({
  primaryClassName,
  primaryProgressPercent,
  secondaryClassName,
  secondaryProgressPercent,
  showSecondaryProgress,
}: ActiveCurtailmentProgressBarProps): ReactElement {
  return (
    <div
      aria-label="Active curtailment progress"
      aria-valuemax={100}
      aria-valuemin={0}
      aria-valuenow={Math.round(primaryProgressPercent)}
      className="flex h-3 w-full gap-1 overflow-hidden rounded-full"
      data-testid="active-curtailment-progress"
      role="progressbar"
    >
      <ProgressSegment className={primaryClassName} percent={primaryProgressPercent} />
      {showSecondaryProgress ? (
        <ProgressSegment className={secondaryClassName} percent={secondaryProgressPercent} />
      ) : null}
    </div>
  );
}

function ProgressLegendItem({ dotClassName, label }: ProgressLegendItemProps): ReactElement {
  return (
    <span className="flex items-center gap-2">
      <Dot className={clsx("h-3 w-3", dotClassName)} />
      {label}
    </span>
  );
}

export default function ActiveCurtailmentStatus({
  event,
  className,
  onDismissRestored,
  onRequestEdit,
  onRequestRestore,
  onRequestStop,
}: ActiveCurtailmentStatusProps): ReactElement {
  const observedReductionKw = event.state === "pending" ? 0 : event.observedReductionKw;
  const targetKw = getTargetKw(event);
  const powerShedPercent = getProgressPercent(observedReductionKw, targetKw);
  const compliance = getMinerCompliance(event);
  const curtailedPercent = getProgressPercent(compliance.curtailedCount, compliance.totalCount);
  const restoredPercent = getRestoredPercent(event, compliance.restoredCount, compliance.totalCount);
  const displayState = getActiveCurtailmentDisplayState(event, powerShedPercent, curtailedPercent);
  const displayFlags = getDisplayFlags(displayState);
  const legend = getProgressLegend(displayFlags);
  const restoredKw = displayFlags.isRestored ? targetKw : (targetKw * restoredPercent) / 100;
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
  const powerLabel = displayFlags.isRestoreFlow ? "Power restore" : "Power shed";
  const powerValue = formatActivePowerValue({
    isRestored: displayFlags.isRestored,
    isRestoreFlow: displayFlags.isRestoreFlow,
    observedReductionKw,
    restoredKw,
    targetKw,
  });
  const dispatchStatus = displayStateLabels[displayState];
  const minerStatus =
    compliance.totalCount > 0 ? `${Math.round(curtailedPercent).toLocaleString()}% curtailed` : "No miners selected";
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
  const restoreFailureValue = formatMinerCount(compliance.restoreFailedCount);
  const restoreProgressPercent = displayFlags.isRestored ? 100 : restoredPercent;
  const curtailProgressPercent = displayFlags.isCurtailmentComplete ? 100 : curtailedPercent;
  const primaryProgressPercent = displayFlags.isRestoreFlow ? restoreProgressPercent : curtailProgressPercent;
  const activePhaseProgressPercent = displayFlags.isRestoreFlow ? restoredPercent : curtailedPercent;
  const secondaryProgressPercent = Math.max(100 - activePhaseProgressPercent, 0);
  const showSecondaryProgress = shouldShowSecondaryProgress(displayFlags);
  const statusIcon = getActiveCurtailmentStatusIcon({
    isTerminalFailure: displayFlags.isTerminalFailure,
    isRestored: displayFlags.isRestored,
    isRestoreIncomplete: displayFlags.isRestoreIncomplete,
    isCurtailmentComplete: displayFlags.isCurtailmentComplete,
  });

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
          onRequestRestore={onRequestRestore}
          onRequestStop={onRequestStop}
        />

        <div className="grid gap-3 tablet:pr-32">
          <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">{statusIcon}</div>
          <div>
            <div className="text-heading-50 text-text-primary-70">{powerLabel}</div>
            <div className="text-heading-300 text-text-primary">{powerValue}</div>
          </div>
        </div>

        <div className="mt-12 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
          <StatBlock label="Dispatch status" value={dispatchStatus} />
          {displayFlags.isRestoreFlow ? (
            <>
              <StatBlock label="Restore" value={formatRestoreProfile(event)} />
              <StatBlock label={restoreTimeLabel} value={restoreTimeValue} />
              {displayFlags.isRestoreIncomplete ? (
                <StatBlock label="Failed to restore" value={restoreFailureValue} />
              ) : (
                <StatBlock label={restoreCompletionLabel} value={restoreCompletionValue} />
              )}
            </>
          ) : (
            <>
              <StatBlock label="Applies to" value={formatMinerCount(event.selectedMiners)} />
              <StatBlock label="Miner status" value={minerStatus} />
              <StatBlock label="Current load" value={formatRemainingPower(event.remainingPowerKw)} />
            </>
          )}
        </div>

        <div className="mt-8 grid gap-3">
          <ActiveCurtailmentProgressBar
            primaryClassName={legend.primaryDotClassName}
            primaryProgressPercent={primaryProgressPercent}
            secondaryClassName={legend.secondaryDotClassName}
            secondaryProgressPercent={secondaryProgressPercent}
            showSecondaryProgress={showSecondaryProgress}
          />
          <div className="flex flex-wrap gap-x-6 gap-y-2 text-200 text-text-primary-70">
            <ProgressLegendItem dotClassName={legend.primaryDotClassName} label={legend.primaryLabel} />
            {showSecondaryProgress ? (
              <ProgressLegendItem dotClassName={legend.secondaryDotClassName} label={legend.secondaryLabel} />
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}
