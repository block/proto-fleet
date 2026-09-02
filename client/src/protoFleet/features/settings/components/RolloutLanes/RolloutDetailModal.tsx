import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";
import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  batchLabel,
  currentBatchDevices,
  deviceStateLabels,
  deviceStateTone,
  formatDurationSeconds,
  formatHashRateHs,
  formatPercentChange,
  isAwaitingReview,
  isBatchStage,
  isPaused,
  isStaged,
  rolloutMethodLabels,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
  rolloutStatusHeadline,
  scopeCounts,
} from "./rolloutStatus";
import StatusChip from "./StatusChip";
import {
  type Rollout,
  RolloutCancelReason,
  type RolloutDevice,
  type RolloutEvidence,
  RolloutMethod,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import { variants } from "@/shared/components/Button";
import CompositionBar from "@/shared/components/CompositionBar";
import Modal, { sizes } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const millisecondsPerSecond = 1000;

const formatRolloutTimestamp = (timestamp?: Timestamp): string =>
  timestamp ? formatTimestamp(Math.floor(timestampMs(timestamp) / 1000)) : "—";

// Ticks once per second so the elapsed readout moves between polling
// snapshots. Lives in its own component so the per-second tick re-renders
// only this value (same pattern as the curtailment card).
function ElapsedProgressValue({ sinceMs }: { sinceMs: number }): ReactElement {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    const intervalId = setInterval(() => setNowMs(Date.now()), millisecondsPerSecond);
    return () => clearInterval(intervalId);
  }, []);

  const elapsedSeconds = Math.max((nowMs - sinceMs) / millisecondsPerSecond, 0);
  return <span>{`${formatElapsed(elapsedSeconds)} elapsed`}</span>;
}

const DetailRow = ({ label, value }: { label: string; value: ReactNode }) => (
  <div className="flex items-baseline justify-between gap-4 border-t border-border-5 py-3">
    <span className="text-200 text-text-primary-50">{label}</span>
    <span className="text-right text-200 text-text-primary">{value}</span>
  </div>
);

const minersNoun = (count: number): string => (count === 1 ? "1 miner" : `${count.toLocaleString()} miners`);

// One evidence figure: label, value, and an optional tone for the value.
const EvidenceStat = ({
  label,
  value,
  tone = "neutral",
  testId,
}: {
  label: string;
  value: string;
  tone?: "neutral" | "good" | "bad";
  testId?: string;
}) => (
  <div className="flex flex-col gap-0.5 rounded-lg bg-surface-base px-3 py-2" data-testid={testId}>
    <span className="text-100 text-text-primary-50">{label}</span>
    <span
      className={clsx(
        "text-heading-100",
        tone === "good" && "text-intent-healthy-text",
        tone === "bad" && "text-text-critical",
        tone === "neutral" && "text-text-primary",
      )}
    >
      {value}
    </span>
  </div>
);

// In the rest stage (and for immediate rollouts) the evidence covers every
// target; while batching or at the gate it covers the current batch.
const rolloutIsRest = (rollout: Rollout): boolean => !isBatchStage(rollout) && !isAwaitingReview(rollout);

// Post-update evidence for the miners under review, against their own
// baselines: the centerpiece of the review gate.
const EvidenceSection = ({ rollout, evidence }: { rollout: Rollout; evidence: RolloutEvidence }) => {
  const scopeLabel = rolloutIsRest(rollout) ? "All miners" : batchLabel(rollout);
  const hashrateValue = evidence.hasHashrateEvidence
    ? `${formatPercentChange(evidence.hashrateChangePercent)} (${formatHashRateHs(evidence.baselineHashRateHs)} → ${formatHashRateHs(evidence.currentHashRateHs)})`
    : "No recent samples";
  const hashrateTone = !evidence.hasHashrateEvidence
    ? "neutral"
    : rollout.maxHashrateDropPercent > 0 && evidence.hashrateChangePercent < -rollout.maxHashrateDropPercent
      ? "bad"
      : "good";

  return (
    <section className="flex flex-col gap-3" data-testid="rollout-evidence">
      <div className="flex items-baseline justify-between gap-4">
        <h3 className="text-heading-100 text-text-primary">{`${scopeLabel} — evidence versus baseline`}</h3>
        <span className="text-200 text-text-primary-50">{minersNoun(evidence.devicesTotal)}</span>
      </div>
      <div className="grid grid-cols-4 gap-2 phone:grid-cols-2">
        <EvidenceStat
          label="Back online"
          value={`${evidence.online} of ${evidence.devicesTotal}`}
          tone={evidence.online === evidence.devicesTotal ? "good" : "bad"}
          testId="evidence-online"
        />
        <EvidenceStat
          label="Hashing"
          value={`${evidence.hashing} of ${evidence.devicesTotal} (was ${evidence.baselineHashing})`}
          tone={evidence.hashing >= evidence.baselineHashing ? "good" : "bad"}
          testId="evidence-hashing"
        />
        <EvidenceStat label="Hashrate" value={hashrateValue} tone={hashrateTone} testId="evidence-hashrate" />
        <EvidenceStat
          label="New errors"
          value={evidence.newErrors.toLocaleString()}
          tone={evidence.newErrors === 0 ? "good" : "bad"}
          testId="evidence-errors"
        />
      </div>
      {rollout.autoAdvance ? (
        <div className="text-200 text-text-primary-70" data-testid="evidence-auto-advance">
          {evidence.readyToAdvance
            ? "Thresholds met — continuing automatically."
            : evidence.holdReason === "Stabilizing"
              ? `Stabilizing: continues automatically in ${formatDurationSeconds(evidence.stabilizationRemainingSeconds)} if the evidence holds.`
              : `Holding for review: ${evidence.holdReason}.`}
          {` Limits: hashrate drop ≤ ${rollout.maxHashrateDropPercent}%, stabilization ${formatDurationSeconds(rollout.stabilizationSeconds)}.`}
        </div>
      ) : null}
    </section>
  );
};

// Per-miner rows for the batch under review (or every target otherwise).
const DeviceEvidenceTable = ({ devices }: { devices: RolloutDevice[] }) => (
  <div className="max-h-64 overflow-y-auto rounded-lg border border-border-5">
    <table className="w-full text-left text-200">
      <thead className="text-100 sticky top-0 bg-surface-elevated-base text-text-primary-50">
        <tr>
          <th className="px-3 py-2 font-normal">Miner</th>
          <th className="px-3 py-2 font-normal">State</th>
          <th className="px-3 py-2 font-normal">Device status</th>
          <th className="px-3 py-2 text-right font-normal">Hashrate (before → now)</th>
          <th className="px-3 py-2 text-right font-normal">Open errors</th>
        </tr>
      </thead>
      <tbody>
        {devices.map((device) => (
          <tr
            key={device.deviceId.toString()}
            className="border-t border-border-5 text-text-primary"
            data-testid={`evidence-device-${device.deviceIdentifier}`}
          >
            <td className="truncate px-3 py-2">{device.deviceIdentifier}</td>
            <td className="px-3 py-2">
              <StatusChip label={deviceStateLabels[device.state]} tone={deviceStateTone(device.state)} />
            </td>
            <td className="px-3 py-2 text-text-primary-70">{device.status || "Unknown"}</td>
            <td className="px-3 py-2 text-right text-text-primary-70">
              {`${device.hasBaselineHashRate ? formatHashRateHs(device.baselineHashRateHs) : "—"} → ${device.hasHashRate ? formatHashRateHs(device.hashRateHs) : "—"}`}
            </td>
            <td
              className={clsx(
                "px-3 py-2 text-right",
                device.openErrors > device.baselineOpenErrors ? "text-text-critical" : "text-text-primary-70",
              )}
            >
              {device.hasBaseline ? `${device.openErrors} (was ${device.baselineOpenErrors})` : device.openErrors}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

interface RolloutDetailModalProps {
  // Live rollout from the poll, so progress and status track the server.
  rollout: Rollout;
  onClose: () => void;
  // Releases the review gate.
  onContinue: (rollout: Rollout) => Promise<void>;
  onPause: (rollout: Rollout) => Promise<void>;
  onResume: (rollout: Rollout) => Promise<void>;
  // Opens the abort confirmation (the caller owns the dialog).
  onAbort: (rollout: Rollout) => void;
}

// Full update detail surface behind "View update": status lockup, live
// progress, the review gate with its evidence, the operator controls
// (pause / resume / abort / continue), and the detail rows.
const RolloutDetailModal = ({ rollout, onClose, onContinue, onPause, onResume, onAbort }: RolloutDetailModalProps) => {
  const [isContinuing, setIsContinuing] = useState(false);
  const [isTogglingPause, setIsTogglingPause] = useState(false);
  const isActive = rollout.status === RolloutStatus.ACTIVE;
  const awaitingReview = isAwaitingReview(rollout);
  const paused = isPaused(rollout);
  const counts = scopeCounts(rollout);
  const segments = rolloutProgressSegments(counts);
  const startedAtMs = rollout.createdAt ? timestampMs(rollout.createdAt) : undefined;
  const finishedAtMs = rollout.finishedAt ? timestampMs(rollout.finishedAt) : undefined;
  const evidenceDevices = rolloutIsRest(rollout) ? rollout.devices : currentBatchDevices(rollout);

  const handleContinue = () => {
    setIsContinuing(true);
    onContinue(rollout).finally(() => setIsContinuing(false));
  };
  const handleTogglePause = () => {
    setIsTogglingPause(true);
    (paused ? onResume(rollout) : onPause(rollout)).finally(() => setIsTogglingPause(false));
  };

  const busy = isContinuing || isTogglingPause;
  const buttons = isActive
    ? [
        { text: "Abort", variant: variants.secondaryDanger, onClick: () => onAbort(rollout), disabled: busy },
        {
          text: paused ? "Resume" : "Pause",
          variant: variants.secondary,
          onClick: handleTogglePause,
          loading: isTogglingPause,
          disabled: isContinuing,
        },
        awaitingReview
          ? { text: "Continue rollout", variant: variants.primary, onClick: handleContinue, loading: isContinuing }
          : { text: "Done", variant: variants.primary, onClick: onClose, disabled: busy },
      ]
    : [{ text: "Done", variant: variants.primary, onClick: onClose }];

  return (
    <Modal
      open
      size={sizes.large}
      title={`${rollout.laneName}, ${rollout.model} firmware update`}
      onDismiss={onClose}
      buttons={buttons}
    >
      <div className="flex flex-col gap-5" data-testid={`rollout-detail-${rollout.id.toString()}`}>
        <div className="flex items-center gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-core-primary-5">
            {awaitingReview ? (
              <span className="size-2.5 rounded-full bg-intent-warning-fill" />
            ) : paused ? (
              <span className="bg-core-primary-30 size-2.5 rounded-full" />
            ) : isActive ? (
              <ProgressCircular indeterminate />
            ) : (
              <span
                className={clsx(
                  "size-2.5 rounded-full",
                  rollout.status === RolloutStatus.COMPLETED
                    ? "bg-intent-healthy-fill"
                    : rollout.cancelReason === RolloutCancelReason.ABORTED
                      ? "bg-intent-critical-fill"
                      : "bg-core-primary-10",
                )}
              />
            )}
          </div>
          <div className="min-w-0">
            <div className="text-heading-50 text-text-primary-70">Update status</div>
            <div className="text-heading-300 text-text-primary" data-testid="rollout-status-headline">
              {rolloutStatusHeadline(rollout)}
            </div>
          </div>
        </div>

        {awaitingReview ? (
          <div
            className="flex flex-col gap-1 rounded-lg bg-intent-warning-10 px-4 py-3"
            data-testid="pilot-review-banner"
          >
            <span className="text-300 text-text-primary">
              {`${batchLabel(rollout)} (${minersNoun(counts.total)}) is on ${rollout.firmwareVersion}.`}
            </span>
            <span className="text-200 text-text-primary-70">
              {rollout.currentBatch + 1 < rollout.batchCount
                ? "Check the evidence below before continuing. Continuing starts the next batch; aborting restores the previous firmware assignment."
                : "Check the evidence below before continuing. Continuing updates the remaining miners; aborting restores the previous firmware assignment."}
            </span>
          </div>
        ) : null}

        {paused ? (
          <div
            className="rounded-lg bg-core-primary-5 px-4 py-3 text-200 text-text-primary-70"
            data-testid="paused-banner"
          >
            Paused: no new update commands are sent and the rollout does not advance until resumed. Miners already
            updating finish on their own.
          </div>
        ) : null}

        <div className="grid gap-3">
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1 text-200">
            <span className="text-text-primary-50">
              {isBatchStage(rollout) || awaitingReview
                ? `${batchLabel(rollout)}: ${rolloutProgressSummary(counts)}`
                : rolloutProgressSummary(counts)}
            </span>
            <span className="text-right text-text-primary">
              {isActive && startedAtMs !== undefined ? (
                <ElapsedProgressValue sinceMs={startedAtMs} />
              ) : startedAtMs !== undefined && finishedAtMs !== undefined ? (
                `${formatElapsed((finishedAtMs - startedAtMs) / millisecondsPerSecond)} elapsed`
              ) : null}
            </span>
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
          </div>
        </div>

        {isActive && rollout.evidence ? (
          <>
            <EvidenceSection rollout={rollout} evidence={rollout.evidence} />
            {evidenceDevices.length > 0 ? <DeviceEvidenceTable devices={evidenceDevices} /> : null}
          </>
        ) : null}

        <div>
          <DetailRow label="Scope" value={`${rollout.laneName} channel, ${minersNoun(rollout.devices.length)}`} />
          <DetailRow label="Target version" value={rollout.firmwareVersion} />
          {rollout.previousFirmwareVersion ? (
            <DetailRow label="Previous version" value={rollout.previousFirmwareVersion} />
          ) : null}
          <DetailRow label="Method" value={rolloutMethodLabels[rollout.method]} />
          {isStaged(rollout) ? (
            <DetailRow
              label={rollout.method === RolloutMethod.PILOT ? "Pilot size" : "Batch size"}
              value={
                rollout.batchCount > 1
                  ? `${minersNoun(rollout.batchSize)} per batch, ${rollout.batchCount} batches`
                  : minersNoun(rollout.batchSize)
              }
            />
          ) : null}
          {isStaged(rollout) ? (
            <DetailRow
              label="Review gates"
              value={
                rollout.autoAdvance
                  ? `Automatic (hashrate drop ≤ ${rollout.maxHashrateDropPercent}%, ${formatDurationSeconds(rollout.stabilizationSeconds)} stabilization)`
                  : "Manual"
              }
            />
          ) : null}
          <DetailRow label="Started" value={formatRolloutTimestamp(rollout.createdAt)} />
          {rollout.finishedAt ? (
            <DetailRow label="Finished" value={formatRolloutTimestamp(rollout.finishedAt)} />
          ) : null}
        </div>
      </div>
    </Modal>
  );
};

export default RolloutDetailModal;
