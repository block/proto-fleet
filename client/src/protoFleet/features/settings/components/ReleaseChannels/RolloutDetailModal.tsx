import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";
import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import RolloutMinersModal, { type RolloutMinerFilter } from "./RolloutMinersModal";
import {
  type DeltaIntent,
  failedDevices,
  formatDurationSeconds,
  isActive as isActiveRollout,
  isAwaitingReview,
  isBatchStage,
  isPaused,
  isStaged,
  metricDisplay,
  type MetricKind,
  pacingSummary,
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutStageLabel,
  scopeCounts,
} from "./rolloutStatus";
import {
  type Rollout,
  type RolloutEvidence,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert, Dismiss, Info, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import type { ButtonProps } from "@/shared/components/ButtonGroup";
import Callout, { intents } from "@/shared/components/Callout";
import CompositionBar from "@/shared/components/CompositionBar";
import Header from "@/shared/components/Header";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const millisecondsPerSecond = 1000;

const formatRolloutTimestamp = (timestamp?: Timestamp): string =>
  timestamp ? formatTimestamp(Math.floor(timestampMs(timestamp) / 1000)) : "—";

const minersNoun = (count: number): string => (count === 1 ? "1 miner" : `${count.toLocaleString()} miners`);

// Ticks once per second so the elapsed readout moves between polling
// snapshots (same pattern as the curtailment card).
function ElapsedValue({ sinceMs }: { sinceMs: number }): ReactElement {
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    const intervalId = setInterval(() => setNowMs(Date.now()), millisecondsPerSecond);
    return () => clearInterval(intervalId);
  }, []);
  return <span>{`${formatElapsed(Math.max((nowMs - sinceMs) / millisecondsPerSecond, 0))} elapsed`}</span>;
}

// Same lockup as the curtailment detail's StatBlock.
function StatBlock({
  label,
  value,
  detail,
  testId,
}: {
  label: string;
  value: string;
  detail?: string;
  testId?: string;
}) {
  return (
    <div className="min-w-0" data-testid={testId}>
      <div className="text-200 text-text-primary-50">{label}</div>
      <div className="mt-1 text-emphasis-300 break-words text-text-primary" title={value}>
        {value}
      </div>
      {detail ? <div className="mt-1 text-200 break-words text-text-primary-70">{detail}</div> : null}
    </div>
  );
}

const deltaTextColor: Record<DeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
  neutral: "text-text-primary-50",
};

// Baseline-vs-current telemetry for the miners in scope, plus the error
// count: the evidence an operator weighs at a review gate.
function PerformanceStrip({ evidence }: { evidence: RolloutEvidence }): ReactElement {
  const temperatureUnit = useTemperatureUnit();
  const metrics: { kind: MetricKind; comparison: RolloutEvidence["hashRateHs"] }[] = [
    { kind: "hashrate", comparison: evidence.hashRateHs },
    { kind: "power", comparison: evidence.powerW },
    { kind: "efficiency", comparison: evidence.efficiencyJh },
    { kind: "temperature", comparison: evidence.tempC },
  ];
  return (
    <div data-testid="rollout-performance">
      <div className="grid gap-x-8 gap-y-5 text-text-primary tablet:grid-cols-2 laptop:grid-cols-5">
        {metrics.map(({ kind, comparison }) => {
          const display = metricDisplay(kind, comparison, temperatureUnit);
          return (
            <div key={kind} className="min-w-0" data-testid={`evidence-${kind}`}>
              <div className="text-200 text-text-primary-50">{display.label}</div>
              <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-emphasis-300 text-text-primary">
                <span className="min-w-0 whitespace-nowrap">{display.value}</span>
                {display.delta ? <span className={deltaTextColor[display.deltaIntent]}>{display.delta}</span> : null}
              </div>
            </div>
          );
        })}
        <div className="min-w-0" data-testid="evidence-errors">
          <div className="text-200 text-text-primary-50">New errors</div>
          <div
            className={clsx(
              "mt-1 text-emphasis-300",
              evidence.newErrors > 0 ? "text-text-critical" : "text-text-primary",
            )}
          >
            {evidence.newErrors.toLocaleString()}
          </div>
        </div>
      </div>
      <div className="mt-3 text-200 text-text-primary-50">
        Compares the baseline before the update with telemetry after miners come back.
      </div>
    </div>
  );
}

function statusIcon(rollout: Rollout): ReactNode {
  switch (rollout.state) {
    case RolloutState.COMPLETED:
      return <Success className="text-intent-success-fill" />;
    case RolloutState.COMPLETED_WITH_FAILURES:
      return <Alert className="text-intent-critical-fill" />;
    case RolloutState.CANCELED:
      return <Info className="text-text-primary-50" />;
    case RolloutState.PAUSED_AT_PILOT_GATE:
    case RolloutState.PAUSED_AT_BATCH_REVIEW:
      return <Info className="text-text-primary" />;
    case RolloutState.PAUSED:
      return <Alert className="text-core-accent-fill" />;
    default:
      return <ProgressCircular indeterminate className="text-core-primary-fill" />;
  }
}

// Human summary of the auto-continue thresholds a rollout was started with.
function thresholdSummary(rollout: Rollout): string {
  const b = rollout.behavior;
  if (!b?.autoContinueOnHealthyTelemetry) return "Manual";
  const t = b.thresholds;
  const parts: string[] = [];
  if (t?.maxHashrateDropPercent !== undefined) parts.push(`hashrate drop ≤ ${t.maxHashrateDropPercent}%`);
  if (t?.maxEfficiencyIncreasePercent !== undefined) parts.push(`efficiency +≤ ${t.maxEfficiencyIncreasePercent}%`);
  if (t?.maxTemperatureIncreaseCelsius !== undefined) parts.push(`temp +≤ ${t.maxTemperatureIncreaseCelsius}°C`);
  if (t?.maxNewErrors !== undefined) parts.push(`≤ ${t.maxNewErrors} new errors`);
  if (b.stabilizationSeconds > 0) parts.push(`${formatDurationSeconds(b.stabilizationSeconds)} to settle`);
  return parts.length > 0 ? `Automatic (${parts.join(", ")})` : "Automatic";
}

export interface RolloutDetailActions {
  // Releases the review gate.
  onContinue: (rollout: Rollout) => Promise<void>;
  onPause: (rollout: Rollout) => Promise<void>;
  onResume: (rollout: Rollout) => Promise<void>;
  // Open confirmations (the caller owns the dialogs).
  onCancel: (rollout: Rollout) => void;
  onRollback: (rollout: Rollout) => void;
  onRetryFailed: (rollout: Rollout) => Promise<void>;
  // Drills into the rollout's release channel.
  onManage?: (rollout: Rollout) => void;
}

interface RolloutDetailModalProps extends RolloutDetailActions {
  // Live rollout from the poll, so progress and status track the server.
  rollout: Rollout;
  minerNames: Record<string, string>;
  onClose: () => void;
}

// Full-screen update detail: a sticky header carrying the lifecycle actions,
// then (failures first) the status lockup, plan stat lockups, progress
// against plan, and the telemetry evidence strip. Miner drill-downs open as
// a standalone list modal.
const RolloutDetailModal = ({
  rollout,
  minerNames,
  onClose,
  onContinue,
  onPause,
  onResume,
  onCancel,
  onRollback,
  onRetryFailed,
  onManage,
}: RolloutDetailModalProps) => {
  const [isContinuing, setIsContinuing] = useState(false);
  const [isTogglingPause, setIsTogglingPause] = useState(false);
  const [isRetrying, setIsRetrying] = useState(false);
  const [minersFilter, setMinersFilter] = useState<RolloutMinerFilter | null>(null);

  const active = isActiveRollout(rollout);
  const awaitingReview = isAwaitingReview(rollout);
  const paused = isPaused(rollout);
  const staged = isStaged(rollout);
  const counts = scopeCounts(rollout);
  const totals = rolloutDeviceCounts(rollout);
  const failed = failedDevices(rollout).length;
  const segments = rolloutProgressSegments(counts);
  const evidence = rollout.evidence;
  const startedAtMs = rollout.createdAt ? timestampMs(rollout.createdAt) : undefined;
  const finishedAtMs = rollout.finishedAt ? timestampMs(rollout.finishedAt) : undefined;
  const title = `${rollout.channelName}, ${rollout.model} firmware update`;
  const canRollBack =
    rollout.previousFirmwareFileId !== "" && rollout.previousFirmwareFileId !== rollout.firmwareFileId;

  const handleContinue = () => {
    setIsContinuing(true);
    onContinue(rollout).finally(() => setIsContinuing(false));
  };
  const handleTogglePause = () => {
    setIsTogglingPause(true);
    (paused ? onResume(rollout) : onPause(rollout)).finally(() => setIsTogglingPause(false));
  };
  const handleRetry = () => {
    setIsRetrying(true);
    onRetryFailed(rollout).finally(() => setIsRetrying(false));
  };
  const busy = isContinuing || isTogglingPause || isRetrying;

  // Header action bar: Manage / Continue / Resume / Pause / Retry failed
  // inline, the rest in an overflow menu, mirroring the design's
  // ViewRolloutModal.
  const headerButtons: ButtonProps[] = [];
  if (onManage) {
    headerButtons.push({
      text: "Manage",
      variant: variants.secondary,
      onClick: () => onManage(rollout),
      disabled: busy,
      testId: "view-rollout-manage-action",
    });
  }
  if (awaitingReview) {
    headerButtons.push({
      text: "Continue",
      variant: variants.primary,
      onClick: handleContinue,
      loading: isContinuing,
      disabled: isTogglingPause || isRetrying,
      testId: "view-rollout-continue-action",
    });
  }
  if (active) {
    headerButtons.push({
      text: paused ? "Resume" : "Pause",
      variant: paused ? variants.primary : variants.secondary,
      onClick: handleTogglePause,
      loading: isTogglingPause,
      disabled: isContinuing || isRetrying,
      testId: paused ? "view-rollout-resume-action" : "view-rollout-pause-action",
    });
  }
  if (failed > 0 && rollout.status !== RolloutStatus.CANCELED) {
    headerButtons.push({
      text: "Retry failed",
      variant: variants.secondary,
      onClick: handleRetry,
      loading: isRetrying,
      disabled: isContinuing || isTogglingPause,
      testId: "view-rollout-retry-action",
    });
  }
  const overflowActions: RowAction[] = [
    { label: "View miners", onClick: () => setMinersFilter("all"), testId: "view-rollout-view-miners-action" },
  ];
  if (canRollBack) {
    overflowActions.push({
      label: `Roll back to ${rollout.previousFirmwareVersion}`,
      onClick: () => onRollback(rollout),
      testId: "view-rollout-rollback-action",
    });
  }
  if (active) {
    overflowActions.push({
      label: "Cancel remaining",
      onClick: () => onCancel(rollout),
      showGroupDivider: false,
      testId: "view-rollout-cancel-action",
    });
  }

  return (
    <>
      <Modal
        open
        onDismiss={onClose}
        size={modalSizes.fullscreen}
        showHeader={false}
        className="!p-0"
        bodyClassName="flex h-full min-h-0 w-full flex-col overflow-auto bg-surface-base pb-6"
      >
        <div className="sticky top-0 z-10 bg-surface-base px-6 pt-6 pb-4" data-testid="rollout-detail-header">
          <Header
            title={title}
            titleSize="text-heading-200"
            icon={<Dismiss />}
            iconAriaLabel="Close update details"
            iconOnClick={onClose}
            inline
            centerButton
            stackButtonsOnPhone={false}
            buttons={headerButtons}
          >
            <RowActionsMenu
              actions={overflowActions}
              ariaLabel={`More actions for ${title}`}
              popoverTestId="view-rollout-more-actions-menu"
              testIdPrefix="view-rollout-more-actions"
              triggerClassName="!h-10 !w-10 !px-0 !py-0"
              triggerVariant={variants.secondary}
            />
          </Header>
        </div>

        <div className="mx-auto w-full max-w-[800px] px-6 pb-6" data-testid={`rollout-detail-${rollout.id.toString()}`}>
          <div className="pt-6">
            {failed > 0 ? (
              <Callout
                intent={intents.danger}
                prefixIcon={<Alert />}
                testId="rollout-failed-banner"
                title={`${minersNoun(failed)} failed to update`}
                subtitle="They did not report the new version after three update attempts and are left alone until retried. Review them, then retry or continue without them."
                buttonText="Review miners"
                buttonOnClick={() => setMinersFilter("failed")}
              />
            ) : null}

            <div className={clsx("grid gap-3", failed > 0 && "mt-10")}>
              <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">
                {statusIcon(rollout)}
              </div>
              <div>
                <div className="text-heading-50 text-text-primary-70">Update status</div>
                <div className="text-heading-300 text-text-primary" data-testid="rollout-status-headline">
                  {rolloutStageLabel(rollout)}
                </div>
              </div>
            </div>

            {awaitingReview ? (
              <Callout
                className="mt-10"
                intent={intents.information}
                prefixIcon={<Info />}
                testId="review-banner"
                title={`${counts.updated === counts.total ? "Every miner" : `${counts.updated} of ${counts.total} miners`} in this batch ${counts.updated === 1 && counts.total === 1 ? "is" : "are"} on ${rollout.firmwareVersion}.`}
                subtitle={
                  evidence && rollout.behavior?.autoContinueOnHealthyTelemetry
                    ? evidence.readyToAdvance
                      ? "Conditions met — continuing automatically."
                      : rollout.state === RolloutState.STABILIZING_TELEMETRY
                        ? `Continues automatically in ${formatDurationSeconds(evidence.stabilizationRemainingSeconds)} if telemetry holds.`
                        : `Holding for review: ${evidence.holdReason}.`
                    : rollout.currentBatch + 1 < rollout.batchCount
                      ? "Check the evidence below, then continue to start the next batch."
                      : "Check the evidence below, then continue to update the remaining miners."
                }
              />
            ) : null}

            {paused ? (
              <Callout
                className="mt-10"
                intent={intents.information}
                prefixIcon={<Info />}
                testId="paused-banner"
                title="Update paused"
                subtitle="No new update commands are sent and the update does not advance until resumed. Miners already updating finish on their own."
              />
            ) : null}

            <div className="mt-10" data-testid="rollout-detail-stats">
              <div className="grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
                <StatBlock label="Scope" value={`${rollout.channelName} channel, ${minersNoun(totals.total)}`} />
                <StatBlock label="Method" value={pacingSummary(rollout.behavior)} />
                {staged ? <StatBlock label="Review gates" value={thresholdSummary(rollout)} /> : null}
                <StatBlock
                  label="Target version"
                  value={rollout.firmwareVersion}
                  detail={rollout.previousFirmwareVersion ? `from ${rollout.previousFirmwareVersion}` : undefined}
                />
                {evidence ? (
                  <>
                    <StatBlock
                      label="Back online"
                      value={`${evidence.online} of ${evidence.devicesTotal}`}
                      testId="evidence-online"
                    />
                    <StatBlock
                      label="Hashing"
                      value={`${evidence.hashing} of ${evidence.devicesTotal}`}
                      detail={`was ${evidence.baselineHashing} before the update`}
                      testId="evidence-hashing"
                    />
                  </>
                ) : null}
                <StatBlock label="Started" value={formatRolloutTimestamp(rollout.createdAt)} />
                {rollout.finishedAt ? (
                  <StatBlock label="Finished" value={formatRolloutTimestamp(rollout.finishedAt)} />
                ) : null}
              </div>
            </div>

            <div className="mt-10 grid gap-3" data-testid="rollout-detail-progress">
              <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
                <div className="text-200 text-text-primary-50">
                  {`${isBatchStage(rollout) || awaitingReview ? `${rolloutStageLabel(rollout)}: ` : ""}${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} miners updated (${counts.percent}%)`}
                </div>
                <div className="text-right text-200 text-text-primary">
                  {active && startedAtMs !== undefined ? (
                    <ElapsedValue sinceMs={startedAtMs} />
                  ) : startedAtMs !== undefined && finishedAtMs !== undefined ? (
                    `${formatElapsed((finishedAtMs - startedAtMs) / millisecondsPerSecond)} elapsed`
                  ) : null}
                </div>
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
                {staged && counts.total !== totals.total ? (
                  <span className="ml-auto text-right text-text-primary-50">
                    {`${(totals.total - counts.total).toLocaleString()} outside this batch`}
                  </span>
                ) : null}
                {totals.excluded > 0 ? (
                  <span className="text-right text-text-primary-50">{`${totals.excluded.toLocaleString()} excluded`}</span>
                ) : null}
              </div>
            </div>

            {active && evidence ? (
              <div className="mt-10" data-testid="rollout-evidence">
                <PerformanceStrip evidence={evidence} />
              </div>
            ) : null}
          </div>
        </div>
      </Modal>

      {minersFilter !== null ? (
        <RolloutMinersModal
          key={minersFilter}
          rollout={rollout}
          minerNames={minerNames}
          initialFilter={minersFilter}
          onClose={() => setMinersFilter(null)}
        />
      ) : null}
    </>
  );
};

export default RolloutDetailModal;
