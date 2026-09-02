import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";
import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import RolloutMinersModal, { type RolloutMinerFilter } from "./RolloutMinersModal";
import {
  attentionDevices,
  type DeltaIntent,
  formatDurationSeconds,
  isAwaitingReview,
  isBatchStage,
  isPaused,
  isStaged,
  metricDisplay,
  type MetricKind,
  type MetricPair,
  rolloutDeviceCounts,
  rolloutMethodLabels,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutStageLabel,
  scopeCounts,
} from "./rolloutStatus";
import {
  type Rollout,
  RolloutCancelReason,
  type RolloutEvidence,
  RolloutMethod,
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
  const metrics: { kind: MetricKind; comparison: MetricPair | undefined }[] = [
    {
      kind: "hashrate",
      comparison: evidence.hasHashrateEvidence
        ? { baseline: evidence.baselineHashRateHs, current: evidence.currentHashRateHs }
        : undefined,
    },
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
  if (rollout.status === RolloutStatus.COMPLETED) return <Success className="text-intent-success-fill" />;
  if (rollout.status === RolloutStatus.CANCELED) {
    return rollout.cancelReason === RolloutCancelReason.ABORTED ? (
      <Alert className="text-intent-critical-fill" />
    ) : (
      <Info className="text-text-primary-50" />
    );
  }
  if (isAwaitingReview(rollout)) return <Info className="text-text-primary" />;
  if (isPaused(rollout)) return <Alert className="text-core-accent-fill" />;
  return <ProgressCircular indeterminate className="text-core-primary-fill" />;
}

interface RolloutDetailModalProps {
  // Live rollout from the poll, so progress and status track the server.
  rollout: Rollout;
  minerNames: Record<string, string>;
  onClose: () => void;
  // Releases the review gate.
  onContinue: (rollout: Rollout) => Promise<void>;
  onPause: (rollout: Rollout) => Promise<void>;
  onResume: (rollout: Rollout) => Promise<void>;
  // Opens the abort confirmation (the caller owns the dialog).
  onAbort: (rollout: Rollout) => void;
  // Drills into the rollout's release channel.
  onManage?: (rollout: Rollout) => void;
}

// Full-screen update detail: a sticky header carrying the lifecycle actions,
// then (errors first) the status lockup, plan stat lockups, progress against
// plan, and the telemetry evidence strip. Miner drill-downs open as a
// standalone list modal.
const RolloutDetailModal = ({
  rollout,
  minerNames,
  onClose,
  onContinue,
  onPause,
  onResume,
  onAbort,
  onManage,
}: RolloutDetailModalProps) => {
  const [isContinuing, setIsContinuing] = useState(false);
  const [isTogglingPause, setIsTogglingPause] = useState(false);
  const [minersFilter, setMinersFilter] = useState<RolloutMinerFilter | null>(null);

  const isActive = rollout.status === RolloutStatus.ACTIVE;
  const awaitingReview = isAwaitingReview(rollout);
  const paused = isPaused(rollout);
  const staged = isStaged(rollout);
  const counts = scopeCounts(rollout);
  const totals = rolloutDeviceCounts(rollout);
  const attention = attentionDevices(rollout).length;
  const segments = rolloutProgressSegments(counts);
  const evidence = rollout.evidence;
  const startedAtMs = rollout.createdAt ? timestampMs(rollout.createdAt) : undefined;
  const finishedAtMs = rollout.finishedAt ? timestampMs(rollout.finishedAt) : undefined;
  const title = `${rollout.laneName}, ${rollout.model} firmware update`;

  const handleContinue = () => {
    setIsContinuing(true);
    onContinue(rollout).finally(() => setIsContinuing(false));
  };
  const handleTogglePause = () => {
    setIsTogglingPause(true);
    (paused ? onResume(rollout) : onPause(rollout)).finally(() => setIsTogglingPause(false));
  };
  const busy = isContinuing || isTogglingPause;

  // Header action bar: Manage / Continue / Resume / Pause inline, the rest in
  // an overflow menu, mirroring the reference ViewRolloutModal.
  const headerButtons: ButtonProps[] = [];
  if (isActive && onManage) {
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
      disabled: isTogglingPause,
      testId: "view-rollout-continue-action",
    });
  }
  if (isActive) {
    headerButtons.push({
      text: paused ? "Resume" : "Pause",
      variant: paused ? variants.primary : variants.secondary,
      onClick: handleTogglePause,
      loading: isTogglingPause,
      disabled: isContinuing,
      testId: paused ? "view-rollout-resume-action" : "view-rollout-pause-action",
    });
  }
  const overflowActions: RowAction[] = [
    { label: "View miners", onClick: () => setMinersFilter("all"), testId: "view-rollout-view-miners-action" },
  ];
  if (isActive) {
    overflowActions.push({
      label: "Cancel remaining update",
      onClick: () => onAbort(rollout),
      showGroupDivider: false,
      testId: "view-rollout-cancel-action",
    });
  }

  // Plan lockups: what the update is doing and how its gates behave.
  const methodStat =
    rollout.method === RolloutMethod.PILOT
      ? {
          value: `${minersNoun(rollout.batchSize)} in pilot batch`,
          detail: `${minersNoun(Math.max(totals.total - rollout.batchSize, 0))} after review`,
        }
      : rollout.method === RolloutMethod.BATCHES
        ? {
            value: `${minersNoun(rollout.batchSize)} per batch`,
            detail: `${rollout.batchCount} batches, review after each`,
          }
        : { value: `${minersNoun(totals.total)} in one batch` };
  const gateStat = !staged
    ? null
    : rollout.autoAdvance
      ? `Automatic (hashrate drop ≤ ${rollout.maxHashrateDropPercent}%, ${formatDurationSeconds(rollout.stabilizationSeconds)} stabilization)`
      : "Manual";

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
            {isActive && attention > 0 ? (
              <Callout
                intent={intents.danger}
                prefixIcon={<Alert />}
                testId="rollout-attention-banner"
                title={`${minersNoun(attention)} ${attention === 1 ? "needs" : "need"} attention`}
                subtitle="Worse off than before the update: offline, no longer hashing, or reporting new errors. Review them before continuing."
                buttonText="Review miners"
                buttonOnClick={() => setMinersFilter("attention")}
              />
            ) : null}

            <div className={clsx("grid gap-3", isActive && attention > 0 && "mt-10")}>
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
                testId="pilot-review-banner"
                title={`${counts.updated === counts.total ? "Every miner" : `${counts.updated} of ${counts.total} miners`} in this batch ${counts.updated === 1 && counts.total === 1 ? "is" : "are"} on ${rollout.firmwareVersion}.`}
                subtitle={
                  evidence && rollout.autoAdvance
                    ? evidence.readyToAdvance
                      ? "Thresholds met — continuing automatically."
                      : evidence.holdReason === "Stabilizing"
                        ? `Continues automatically in ${formatDurationSeconds(evidence.stabilizationRemainingSeconds)} if the evidence holds.`
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
                <StatBlock label="Scope" value={`${rollout.laneName} channel, ${minersNoun(totals.total)}`} />
                <StatBlock
                  label={rolloutMethodLabels[rollout.method]}
                  value={methodStat.value}
                  detail={methodStat.detail}
                />
                {gateStat ? <StatBlock label="Review gates" value={gateStat} /> : null}
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
                  {isActive && startedAtMs !== undefined ? (
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
              </div>
            </div>

            {isActive && evidence ? (
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
