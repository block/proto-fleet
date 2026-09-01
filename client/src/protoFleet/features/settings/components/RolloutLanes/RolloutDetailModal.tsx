import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";
import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  isAwaitingReview,
  isPilotStage,
  pilotCohortCounts,
  rolloutDeviceCounts,
  rolloutMethodLabels,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
  rolloutStatusHeadline,
} from "./rolloutStatus";
import { type Rollout, RolloutMethod, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
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

interface RolloutDetailModalProps {
  // Live rollout from the poll, so progress and status track the server.
  rollout: Rollout;
  onClose: () => void;
  // Advances a pilot rollout past its review gate.
  onContinue: (rollout: Rollout) => Promise<void>;
}

// Full update detail surface behind "View update": status lockup, live
// progress, the pilot review gate for gated rollouts, and a detail list
// that later grows richer rollout mechanics (batching, ordering, telemetry
// deltas) as the backend supports them.
const RolloutDetailModal = ({ rollout, onClose, onContinue }: RolloutDetailModalProps) => {
  const [isContinuing, setIsContinuing] = useState(false);
  const awaitingReview = isAwaitingReview(rollout);
  // In the pilot stage the progress lockup tracks the cohort; from the
  // review gate on, the whole rollout.
  const counts = isPilotStage(rollout) ? pilotCohortCounts(rollout) : rolloutDeviceCounts(rollout);
  const pilotCounts = pilotCohortCounts(rollout);
  const segments = rolloutProgressSegments(counts);
  const isActive = rollout.status === RolloutStatus.ACTIVE;
  const startedAtMs = rollout.createdAt ? timestampMs(rollout.createdAt) : undefined;
  const finishedAtMs = rollout.finishedAt ? timestampMs(rollout.finishedAt) : undefined;

  const handleContinue = () => {
    setIsContinuing(true);
    onContinue(rollout).finally(() => setIsContinuing(false));
  };

  return (
    <Modal
      open
      size={sizes.large}
      title={`${rollout.laneName}, ${rollout.model} firmware update`}
      onDismiss={onClose}
      buttons={
        awaitingReview
          ? [
              { text: "Close", variant: variants.secondary, onClick: onClose, disabled: isContinuing },
              { text: "Continue rollout", variant: variants.primary, onClick: handleContinue, loading: isContinuing },
            ]
          : [{ text: "Done", variant: variants.primary, onClick: onClose }]
      }
    >
      <div className="flex flex-col gap-5" data-testid={`rollout-detail-${rollout.id.toString()}`}>
        <div className="flex items-center gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-core-primary-5">
            {awaitingReview ? (
              <span className="size-2.5 rounded-full bg-intent-warning-fill" />
            ) : isActive ? (
              <ProgressCircular indeterminate />
            ) : (
              <span
                className={clsx(
                  "size-2.5 rounded-full",
                  rollout.status === RolloutStatus.COMPLETED ? "bg-intent-healthy-fill" : "bg-core-primary-10",
                )}
              />
            )}
          </div>
          <div className="min-w-0">
            <div className="text-heading-50 text-text-primary-70">Update status</div>
            <div className="text-heading-300 text-text-primary">{rolloutStatusHeadline(rollout)}</div>
          </div>
        </div>

        {awaitingReview ? (
          <div
            className="flex flex-col gap-1 rounded-lg bg-intent-warning-10 px-4 py-3"
            data-testid="pilot-review-banner"
          >
            <span className="text-300 text-text-primary">
              {`The pilot group (${pilotCounts.total === 1 ? "1 miner" : `${pilotCounts.total.toLocaleString()} miners`}) is on ${rollout.firmwareVersion}.`}
            </span>
            <span className="text-200 text-text-primary-70">
              Check the pilot miners before continuing. Continuing updates the remaining miners; rolling back from
              history reverts the whole model group.
            </span>
          </div>
        ) : null}

        <div className="grid gap-3">
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1 text-200">
            <span className="text-text-primary-50">
              {isPilotStage(rollout) ? `Pilot: ${rolloutProgressSummary(counts)}` : rolloutProgressSummary(counts)}
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

        <div>
          <DetailRow
            label="Scope"
            value={`${rollout.laneName} channel, ${rollout.devices.length === 1 ? "1 miner" : `${rollout.devices.length.toLocaleString()} miners`}`}
          />
          <DetailRow label="Target version" value={rollout.firmwareVersion} />
          <DetailRow label="Method" value={rolloutMethodLabels[rollout.method]} />
          {rollout.method === RolloutMethod.PILOT ? (
            <DetailRow
              label="Pilot size"
              value={rollout.pilotCount === 1 ? "1 miner" : `${rollout.pilotCount.toLocaleString()} miners`}
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
