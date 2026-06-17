import { type ReactElement, type ReactNode } from "react";
import clsx from "clsx";

import {
  type ActiveCurtailmentDisplayState,
  type CurtailmentEventState,
  formatCurtailmentKw as formatKw,
  formatCurtailmentMinerCount as formatMinerCount,
  getActiveCurtailmentDisplayState,
  getCurtailmentTargetKw as getTargetKw,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import { Alert, Fan, Fleet, Success } from "@/shared/assets/icons";
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

export type InfraSequence = "before_miners" | "with_miners" | "after_miners";

export interface InfraCurtailmentState {
  sequence: InfraSequence;
  offsetMinutes: number;
  totalDevices: number;
  curtailedDevices: number;
  restoredDevices: number;
  progressPercent: number;
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
  infraState?: InfraCurtailmentState;
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

function formatRestoreProfile(
  event: Pick<ActiveCurtailmentEvent, "restoreBatchSize" | "restoreBatchIntervalSec">,
): string {
  return `${formatMinerCount(event.restoreBatchSize)} every ${event.restoreBatchIntervalSec.toLocaleString()}s`;
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

function getMinerProgressPercent(
  displayFlags: ActiveCurtailmentDisplayFlags,
  compliance: MinerCompliance,
): number {
  if (displayFlags.isRestored) return 100;
  if (compliance.totalCount === 0) return 0;
  if (displayFlags.isRestoreFlow) {
    return Math.round((compliance.restoredCount / compliance.totalCount) * 100);
  }
  return Math.round((compliance.curtailedCount / compliance.totalCount) * 100);
}

function computeOverallProgress(event: ActiveCurtailmentEvent, compliance: MinerCompliance): number {
  const minerPct = compliance.totalCount > 0
    ? Math.round((compliance.curtailedCount / compliance.totalCount) * 100)
    : 0;
  if (!event.infraState) return minerPct;
  return Math.round((minerPct + event.infraState.progressPercent) / 2);
}

function getSequenceLabel(target: "miners" | "infra", infraSequence: InfraSequence): string | undefined {
  if (infraSequence === "with_miners") return undefined;
  if (target === "miners") {
    return infraSequence === "before_miners" ? "After infrastructure" : "Before infrastructure";
  }
  return infraSequence === "before_miners" ? "Before miners" : "After miners";
}

function ProgressRow({
  icon,
  label,
  progressPercent,
  detail,
  sequenceLabel,
}: {
  icon: ReactNode;
  label: string;
  progressPercent: number;
  detail: string;
  sequenceLabel?: string;
}): ReactElement {
  return (
    <div className="flex items-center gap-3">
      <div className="shrink-0 text-text-primary-70">{icon}</div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-baseline justify-between">
          <div className="flex items-baseline gap-2">
            <span className="text-emphasis-300 font-medium">{label}</span>
            {sequenceLabel && (
              <span className="text-200 text-text-primary-70">{sequenceLabel}</span>
            )}
          </div>
          <span className="text-emphasis-300 font-medium">{progressPercent}%</span>
        </div>
        <div className="h-1.5 overflow-hidden rounded-full bg-border-5">
          <div
            className="h-full rounded-full bg-core-primary-fill transition-all duration-500"
            style={{ width: `${progressPercent}%` }}
          />
        </div>
        <span className="text-200 text-text-primary-70">{detail}</span>
      </div>
    </div>
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
  const targetKw = getTargetKw(event);
  const compliance = getMinerCompliance(event);
  const displayState = getActiveCurtailmentDisplayState(event, { dispatchStartedAsCurtailing: true });
  const displayFlags = getDisplayFlags(displayState);
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
            </>
          )}
        </div>

        {/* Staggered miner + infrastructure progress */}
        <div className="mt-8 flex flex-col gap-3">
          <div className="text-heading-200 text-text-primary">
            {computeOverallProgress(event, compliance)}% complete
          </div>

          <div className="flex flex-col gap-4">
            {/* Miner progress */}
            <ProgressRow
              icon={<Fleet />}
              label="Miners"
              progressPercent={getMinerProgressPercent(displayFlags, compliance)}
              detail={`${compliance.curtailedCount} of ${compliance.totalCount}`}
              sequenceLabel={event.infraState ? getSequenceLabel("miners", event.infraState.sequence) : undefined}
            />

            {/* Infrastructure progress */}
            {event.infraState && (
              <ProgressRow
                icon={<Fan />}
                label="Infrastructure"
                progressPercent={event.infraState.progressPercent}
                detail={`${event.infraState.curtailedDevices} of ${event.infraState.totalDevices}`}
                sequenceLabel={getSequenceLabel("infra", event.infraState.sequence)}
              />
            )}
          </div>
        </div>
      </div>
    </section>
  );
}
