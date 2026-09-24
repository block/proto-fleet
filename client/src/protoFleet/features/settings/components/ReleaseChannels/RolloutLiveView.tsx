import { type ReactElement, type ReactNode, useEffect, useId, useLayoutEffect, useState } from "react";
import clsx from "clsx";
import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import RolloutMinersModal, { type RolloutMinerFilter } from "./RolloutMinersModal";
import {
  batchLabel,
  type DeltaIntent,
  evidenceScopeLabel,
  failedCount,
  formatDurationSeconds,
  isActive as isActiveRollout,
  isAwaitingReview,
  canRollBack as isCurrentGeneration,
  isPaused,
  isStaged,
  metricDisplay,
  type MetricKind,
  orderLabels,
  pacingSummary,
  pairLabel,
  rollbackLabel,
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutStageLabel,
  scopeCounts,
  scopedToBatch,
} from "./rolloutStatus";
import {
  type Rollout,
  type RolloutDevice,
  type RolloutEvidence,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { gatesAfterBatch } from "@/protoFleet/api/rolloutBehavior";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert, ArrowRight, Info, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import type { ButtonProps } from "@/shared/components/ButtonGroup";
import Callout, { intents } from "@/shared/components/Callout";
import CompositionBar from "@/shared/components/CompositionBar";
import Dialog from "@/shared/components/Dialog";
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
function PerformanceStrip({
  evidence,
  testIdPrefix,
}: {
  evidence: RolloutEvidence;
  testIdPrefix: string;
}): ReactElement {
  const temperatureUnit = useTemperatureUnit();
  const metrics: { kind: MetricKind; comparison: RolloutEvidence["hashRateHs"] }[] = [
    { kind: "hashrate", comparison: evidence.hashRateHs },
    { kind: "power", comparison: evidence.powerW },
    { kind: "efficiency", comparison: evidence.efficiencyJh },
    { kind: "temperature", comparison: evidence.tempC },
  ];
  return (
    <div data-testid={`${testIdPrefix}rollout-performance`}>
      <div className="grid gap-x-8 gap-y-5 text-text-primary tablet:grid-cols-2 laptop:grid-cols-5">
        {metrics.map(({ kind, comparison }) => {
          const display = metricDisplay(kind, comparison, temperatureUnit);
          return (
            <div key={kind} className="min-w-0" data-testid={`${testIdPrefix}evidence-${kind}`}>
              <div className="text-200 text-text-primary-50">{display.label}</div>
              <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-emphasis-300 text-text-primary">
                <span className="min-w-0 whitespace-nowrap">{display.value}</span>
                {display.delta ? <span className={deltaTextColor[display.deltaIntent]}>{display.delta}</span> : null}
              </div>
            </div>
          );
        })}
        <div className="min-w-0" data-testid={`${testIdPrefix}evidence-errors`}>
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

export interface RolloutLiveViewProps extends RolloutDetailActions {
  // Live rollout from the poll, so progress and status track the server.
  rollout: Rollout;
  // The pair's current assignment generation; rollback is offered while the
  // rollout is still at it. Undefined hides rollback.
  currentGeneration?: bigint;
  // Terminal retries require the current assignment and no active rollout
  // for the pair. Without that snapshot, only active retries are offered.
  canRetryRemaining?: boolean;
  // Keep page polling failures visible alongside the modal's action controls.
  refreshWarning?: ReactNode;
  minerNames: Record<string, string>;
  // Per-miner progress is paged by the server; the miners drill-down
  // fetches it on demand.
  listRolloutDevices: (rolloutId: bigint, signal?: AbortSignal) => Promise<RolloutDevice[]>;
  // The monitor keeps lifecycle writes locked across presentation changes.
  actionsDisabled?: boolean;
  // Keep owned dialogs mounted when the monitor replaces an inline card with
  // banners or a fullscreen view. Only the card markup is hidden.
  hideCard?: boolean;
  onDialogChange?: (rollout: Rollout, isOpen: boolean) => void;
  onViewUpdate?: (rollout: Rollout) => void;
  presentation?: "inline" | "fullscreen";
  onClose?: () => void;
}

// Shared production update view. Both presentations use the same lifecycle
// controls, confirmations, current snapshot and miner drill-downs.
const RolloutLiveView = ({
  rollout,
  presentation = "fullscreen",
  actionsDisabled = false,
  hideCard = false,
  onDialogChange,
  onViewUpdate,
  currentGeneration,
  canRetryRemaining,
  refreshWarning,
  minerNames,
  listRolloutDevices,
  onClose,
  onContinue,
  onPause,
  onResume,
  onCancel,
  onRollback,
  onRetryFailed,
  onManage,
}: RolloutLiveViewProps) => {
  const inline = presentation === "inline";
  const testIdPrefix = inline ? "inline-" : "";
  const testId = (value: string) => `${testIdPrefix}${value}`;
  const detailsId = useId();
  const [expandedRolloutId, setExpandedRolloutId] = useState<bigint | null>(null);
  const detailsExpanded = !inline || expandedRolloutId === rollout.id;
  const [isContinuing, setIsContinuing] = useState(false);
  const [isTogglingPause, setIsTogglingPause] = useState(false);
  const [isRetrying, setIsRetrying] = useState(false);
  const [retryTarget, setRetryTarget] = useState<Rollout | null>(null);
  const [minersFilter, setMinersFilter] = useState<RolloutMinerFilter | null>(null);

  const active = isActiveRollout(rollout);
  const awaitingReview = isAwaitingReview(rollout);
  const paused = isPaused(rollout);
  const canContinue = awaitingReview && !paused;
  const batchCounts = scopeCounts(rollout);
  const counts = inline ? rolloutDeviceCounts(rollout) : batchCounts;
  const scopedTargetCount = batchCounts.total + batchCounts.excluded + batchCounts.skipped;
  const failed = failedCount(rollout);
  const segments = rolloutProgressSegments(counts);
  const evidence = rollout.evidence;
  const startedAtMs = rollout.createdAt ? timestampMs(rollout.createdAt) : undefined;
  const finishedAtMs = rollout.finishedAt ? timestampMs(rollout.finishedAt) : undefined;
  const title = `${rollout.channelName}, ${pairLabel(rollout)} firmware update`;
  const canRollBack = isCurrentGeneration(rollout, currentGeneration);
  const retryAllowed = canRetryRemaining ?? active;
  const hasOpenDialog = minersFilter !== null || Boolean(retryTarget && retryAllowed && retryTarget.id === rollout.id);
  useLayoutEffect(() => {
    onDialogChange?.(rollout, hasOpenDialog);
  }, [onDialogChange, rollout, hasOpenDialog]);

  // A poll can invalidate this assignment while its confirmation is open.
  // Clear that confirmation so it cannot reappear if eligibility returns.
  if (retryTarget && (!retryAllowed || retryTarget.id !== rollout.id)) setRetryTarget(null);

  const handleContinue = () => {
    if (busy) return;
    setIsContinuing(true);
    onContinue(rollout).finally(() => setIsContinuing(false));
  };
  const handleTogglePause = () => {
    if (busy) return;
    setIsTogglingPause(true);
    (paused ? onResume(rollout) : onPause(rollout)).finally(() => setIsTogglingPause(false));
  };
  const handleRetry = () => {
    if (!retryTarget || !retryAllowed || busy) return;
    setRetryTarget(null);
    setIsRetrying(true);
    onRetryFailed(retryTarget).finally(() => setIsRetrying(false));
  };
  const busy = actionsDisabled || isContinuing || isTogglingPause || isRetrying;

  // Keep lifecycle controls beside status and recovery actions in overflow.
  const lifecycleButtons: ButtonProps[] = [];
  if (canContinue) {
    lifecycleButtons.push({
      text: "Continue",
      variant: variants.primary,
      onClick: handleContinue,
      loading: isContinuing,
      disabled: actionsDisabled || isTogglingPause || isRetrying,
      testId: testId("view-rollout-continue-action"),
    });
  }
  if (active) {
    lifecycleButtons.push({
      text: paused ? "Resume" : "Pause",
      variant: paused ? variants.primary : variants.secondary,
      onClick: handleTogglePause,
      loading: isTogglingPause,
      disabled: actionsDisabled || isContinuing || isRetrying,
      testId: testId(paused ? "view-rollout-resume-action" : "view-rollout-pause-action"),
    });
  }
  const overflowActions: RowAction[] = [];
  if (inline) {
    overflowActions.push({
      label: "View miners",
      onClick: () => setMinersFilter("all"),
      testId: testId("view-rollout-view-miners-action"),
    });
  }
  if (inline && onViewUpdate) {
    overflowActions.push({
      label: "View update",
      onClick: () => onViewUpdate(rollout),
      testId: testId("view-rollout-open-action"),
    });
  }
  if (onManage && !inline) {
    overflowActions.push({
      label: "Manage channel",
      onClick: () => onManage(rollout),
      disabled: busy,
      testId: testId("view-rollout-manage-action"),
    });
  }
  if (retryAllowed) {
    overflowActions.push({
      label: "Retry remaining",
      onClick: () => setRetryTarget(rollout),
      disabled: busy,
      testId: testId("view-rollout-retry-action"),
    });
  }
  if (canRollBack) {
    overflowActions.push({
      label: rollbackLabel(rollout),
      onClick: () => onRollback(rollout),
      disabled: busy,
      testId: testId("view-rollout-rollback-action"),
    });
  }
  if (active) {
    const previousAction = overflowActions[overflowActions.length - 1];
    if (previousAction) previousAction.showGroupDivider = true;
    overflowActions.push({
      label: "Cancel remaining",
      onClick: () => onCancel(rollout),
      disabled: busy,
      danger: true,
      testId: testId("view-rollout-cancel-action"),
    });
  }

  const overflowMenu = (
    <RowActionsMenu
      actions={overflowActions}
      ariaLabel={`More actions for ${title}`}
      popoverTestId={testId("view-rollout-more-actions-menu")}
      testIdPrefix={testId("view-rollout-more-actions")}
      triggerClassName="!h-8 !w-8 !px-0 !py-0"
      triggerVariant={variants.secondary}
    />
  );

  return (
    <>
      {!hideCard ? (
        <section className="w-full" aria-label={inline ? "Active firmware update" : undefined}>
          {inline ? (
            <div className="mb-4">
              <h2 className="text-heading-200 text-text-primary" data-testid={testId("rollout-detail-title")}>
                Active firmware update
              </h2>
              <div className="mt-1 text-emphasis-300 text-text-primary" data-testid={testId("rollout-identifier")}>
                <span>{title}</span>
                <span>{` · ${rollout.firmwareVersion}`}</span>
              </div>
            </div>
          ) : null}
          <div
            className="w-full rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10"
            data-testid={testId("rollout-live-view")}
          >
            {!inline ? (
              <div className="mb-8 flex items-center gap-4">
                <Button
                  ariaLabel="Back"
                  prefixIcon={<ArrowRight className="rotate-180" />}
                  variant={variants.secondary}
                  size={sizes.compact}
                  className="shrink-0"
                  onClick={onClose}
                  testId={testId("view-rollout-back-action")}
                />
                <h2
                  className="min-w-0 text-heading-200 wrap-anywhere text-text-primary"
                  data-testid={testId("rollout-detail-title")}
                >
                  {title}
                </h2>
              </div>
            ) : null}
            {refreshWarning ? <div className="mb-6">{refreshWarning}</div> : null}
            <div className="flex flex-wrap items-start justify-between gap-6">
              <div className="grid min-w-0 gap-3">
                <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">
                  {statusIcon(rollout)}
                </div>
                <div>
                  <div className="text-heading-50 text-text-primary-70">Update status</div>
                  <div className="text-heading-300 text-text-primary" data-testid={testId("rollout-status-headline")}>
                    {inline && rolloutStageLabel(rollout) === "In progress" ? "Updating" : rolloutStageLabel(rollout)}
                  </div>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-3" data-testid={testId("rollout-detail-actions")}>
                {inline ? overflowMenu : null}
                {inline && onManage ? (
                  <Button
                    text="Manage"
                    variant={variants.secondary}
                    size={sizes.compact}
                    onClick={() => onManage(rollout)}
                    disabled={busy}
                    testId={testId("view-rollout-manage-action")}
                  />
                ) : null}
                {lifecycleButtons.map((button) => (
                  <Button key={button.testId} {...button} size={sizes.compact} />
                ))}
                {!inline ? overflowMenu : null}
              </div>
            </div>
            {isRetrying ? (
              <p role="status" className="mt-3 text-200 text-text-primary-70">
                Requesting retry…
              </p>
            ) : null}

            <div className="mt-6 grid gap-3" data-testid={testId("rollout-detail-progress")}>
              <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
                <div className="text-200 text-text-primary-50">
                  {`${!inline && scopedToBatch(rollout) ? batchLabel(rollout) : "Overall progress"}: ${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} miners updated (${counts.percent}%)`}
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
                {!inline && scopedToBatch(rollout) && scopedTargetCount !== rollout.deviceCount ? (
                  <span className="ml-auto text-right text-text-primary-50">
                    {`${(rollout.deviceCount - scopedTargetCount).toLocaleString()} outside this batch`}
                  </span>
                ) : null}
                {counts.excluded > 0 ? (
                  <span className="text-right text-text-primary-50">{`${counts.excluded.toLocaleString()} excluded`}</span>
                ) : null}
                {counts.skipped > 0 ? (
                  <span className="text-right text-text-primary-50">{`${counts.skipped.toLocaleString()} skipped`}</span>
                ) : null}
              </div>
              {!inline ? (
                <Button
                  text="View miners"
                  variant={variants.textOnly}
                  className="justify-self-start"
                  onClick={() => setMinersFilter("all")}
                  testId={testId("view-rollout-view-miners-action")}
                />
              ) : null}
            </div>

            {failed > 0 ? (
              <Callout
                className="mt-6"
                intent={intents.danger}
                prefixIcon={<Alert />}
                testId={testId("rollout-failed-banner")}
                title={`${minersNoun(failed)} failed to update`}
                subtitle="Review miner details for the cause of each failed update."
                buttonText="Review miners"
                buttonOnClick={() => setMinersFilter("failed")}
              />
            ) : null}

            {canContinue ? (
              <Callout
                className="mt-6"
                intent={intents.information}
                prefixIcon={<Info />}
                testId={testId("review-banner")}
                title={`${batchCounts.updated} of ${scopedTargetCount} miners in this batch updated to ${rollout.firmwareVersion}.`}
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
                className="mt-6"
                intent={intents.information}
                prefixIcon={<Info />}
                testId={testId("paused-banner")}
                title="Update paused"
                subtitle="No new update commands are sent and the update does not advance until resumed. Miners already updating finish on their own."
              />
            ) : null}

            {inline ? (
              <button
                type="button"
                className="mt-3 cursor-pointer rounded text-200 text-text-primary underline underline-offset-2 hover:opacity-70 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-core-primary-fill"
                aria-expanded={detailsExpanded}
                aria-controls={detailsId}
                onClick={() => setExpandedRolloutId(detailsExpanded ? null : rollout.id)}
                data-testid={testId("rollout-details-toggle")}
              >
                {detailsExpanded ? "Hide details" : "View details"}
              </button>
            ) : null}
            {detailsExpanded ? (
              <div
                id={detailsId}
                className={clsx(inline && "-mx-6 mt-6 border-t border-border-5 px-6 tablet:-mx-10 tablet:px-10")}
                data-testid={testId("rollout-details")}
              >
                <div className={inline ? "mt-6" : "mt-10"} data-testid={testId("rollout-detail-stats")}>
                  <div className="grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
                    <StatBlock
                      label="Scope"
                      value={`${rollout.channelName} channel, ${minersNoun(rollout.deviceCount)}`}
                    />
                    <StatBlock
                      label="Target version"
                      value={rollout.firmwareVersion}
                      detail={rollout.previousFirmwareVersion ? `from ${rollout.previousFirmwareVersion}` : undefined}
                    />
                    <StatBlock label="Method" value={pacingSummary(rollout.behavior)} />
                    {inline && isStaged(rollout) && rollout.behavior ? (
                      <StatBlock label="Order" value={orderLabels[rollout.behavior.order]} />
                    ) : null}
                    {rollout.behavior && gatesAfterBatch(rollout.behavior) ? (
                      <StatBlock label="Review gates" value={thresholdSummary(rollout)} />
                    ) : null}
                    <StatBlock label="Started" value={formatRolloutTimestamp(rollout.createdAt)} />
                    {rollout.finishedAt ? (
                      <StatBlock label="Finished" value={formatRolloutTimestamp(rollout.finishedAt)} />
                    ) : null}
                  </div>
                </div>

                {evidence ? (
                  <div className="mt-10" data-testid={testId("rollout-evidence")}>
                    <div className="mb-4 text-200 text-text-primary-50">
                      {`Telemetry evidence: ${evidenceScopeLabel(rollout).toLowerCase()} (${minersNoun(evidence.devicesTotal)})`}
                    </div>
                    <div className="mb-6 grid grid-cols-2 gap-x-8 gap-y-5">
                      <StatBlock
                        label="Back online"
                        value={`${evidence.online} of ${evidence.devicesTotal}`}
                        detail={`Evidence: ${evidenceScopeLabel(rollout).toLowerCase()}`}
                        testId={testId("evidence-online")}
                      />
                      <StatBlock
                        label="Hashing"
                        value={`${evidence.hashing} of ${evidence.devicesTotal}`}
                        detail={`${evidenceScopeLabel(rollout)}; was ${evidence.baselineHashing} before the update`}
                        testId={testId("evidence-hashing")}
                      />
                    </div>
                    {active ? <PerformanceStrip evidence={evidence} testIdPrefix={testIdPrefix} /> : null}
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        </section>
      ) : null}

      {retryTarget && retryAllowed ? (
        <Dialog
          open
          testId={testId("retry-rollout-dialog")}
          title="Retry remaining updates?"
          subtitle={`Retry failed, skipped, or canceled work for ${pairLabel(retryTarget)} in ${retryTarget.channelName}, including earlier updates for this firmware assignment. This does not advance review gates.`}
          onDismiss={() => setRetryTarget(null)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setRetryTarget(null) },
            {
              text: "Retry remaining",
              testId: testId("confirm-rollout-retry"),
              variant: variants.primary,
              onClick: handleRetry,
              disabled: busy,
            },
          ]}
        />
      ) : null}

      {minersFilter !== null ? (
        <RolloutMinersModal
          key={minersFilter}
          rollout={rollout}
          minerNames={minerNames}
          listRolloutDevices={listRolloutDevices}
          initialFilter={minersFilter}
          onClose={() => setMinersFilter(null)}
        />
      ) : null}
    </>
  );
};

export default RolloutLiveView;
