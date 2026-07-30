import { type ReactNode, useEffect } from "react";
import clsx from "clsx";

import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import type {
  CommandBatchDeviceResult,
  GetCommandBatchDeviceResultsResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useCommandBatchDeviceResults } from "@/protoFleet/api/useCommandBatchDeviceResults";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { isCompletedEvent } from "@/protoFleet/features/activity/utils/eventType";
import {
  formatActivityDescription,
  formatActivityErrorMessage,
  formatActivityErrorSummary,
} from "@/protoFleet/features/activity/utils/formatActivityDescription";
import { formatScope } from "@/protoFleet/features/activity/utils/formatScope";
import { Alert, Info } from "@/shared/assets/icons";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import StatusCircle from "@/shared/components/StatusCircle";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

interface ActivityDetailModalProps {
  entry: ActivityEntry | null;
  onDismiss: () => void;
}

function SummaryRow({
  label,
  children,
  className,
  divider = true,
}: {
  label: string;
  children: ReactNode;
  className?: string;
  divider?: boolean;
}) {
  return (
    <Row compact divider={divider}>
      <div className="flex w-full items-center justify-between gap-4">
        <span className="shrink-0 text-300 text-text-primary-70">{label}</span>
        <span className={clsx("min-w-0 text-right text-300 text-text-primary", className)}>{children}</span>
      </div>
    </Row>
  );
}

const formatBatchResult = (data: GetCommandBatchDeviceResultsResponse): string => {
  const completedCount = data.successCount;
  const totalCount = data.totalCount || data.successCount + data.failureCount;

  if (totalCount === 0) return "Completed";

  return `${completedCount}/${totalCount} ${totalCount === 1 ? "miner" : "miners"} completed`;
};

const ActivityDetailModal = ({ entry, onDismiss }: ActivityDetailModalProps) => {
  const batchId = entry?.batchId;
  const { fetch, getResult } = useCommandBatchDeviceResults({
    activeBatchId: batchId,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  useEffect(() => {
    if (batchId) {
      void fetch(batchId);
    }
  }, [batchId, fetch]);

  if (!entry) return null;

  const batchState = batchId ? getResult(batchId) : null;
  const batchData = batchState?.data;
  const batchInProgress =
    (batchId != null && batchData == null && !isCompletedEvent(entry.eventType)) ||
    (batchData != null && batchData.status !== "finished" && !batchData.detailsPruned);
  const isFailed =
    batchData != null && !batchInProgress && !batchData.detailsPruned
      ? batchData.failureCount > 0
      : entry.result === "failure";

  const showBatchCounts = batchData != null && !batchData.detailsPruned;
  const hasErrorMessage = Boolean(entry.errorMessage);
  const resultLabel = batchInProgress
    ? "In progress"
    : showBatchCounts && batchData
      ? formatBatchResult(batchData)
      : isFailed
        ? "Couldn't complete"
        : "Completed";

  return (
    <Modal title={formatActivityDescription(entry)} onDismiss={onDismiss}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col">
          <SummaryRow label="Time">{formatActivityTimestamp(Number(entry.createdAt?.seconds))}</SummaryRow>
          <SummaryRow label="Scope">
            {formatScope(entry.scopeType, entry.scopeLabel, entry.scopeCount || undefined)}
          </SummaryRow>
          <SummaryRow label="User">{entry.username ?? "—"}</SummaryRow>
          <SummaryRow label="Result" divider={hasErrorMessage}>
            <span className="inline-flex items-center gap-1.5">
              <StatusCircle
                status={batchInProgress ? "pending" : isFailed ? "error" : "normal"}
                variant="simple"
                width="w-1.5"
                removeMargin
              />
              {resultLabel}
            </span>
          </SummaryRow>
          {entry.errorMessage ? (
            <SummaryRow label="Issue" className="text-intent-critical break-words" divider={false}>
              {formatActivityErrorSummary(entry.errorMessage)}
            </SummaryRow>
          ) : null}
        </div>

        {batchId ? (
          <BatchDeviceResults
            isLoading={batchState?.isLoading ?? false}
            error={batchState?.error ?? null}
            data={batchData ?? null}
          />
        ) : null}
      </div>
    </Modal>
  );
};

function BatchDeviceResults({
  isLoading,
  error,
  data,
}: {
  isLoading: boolean;
  error: string | null;
  data: GetCommandBatchDeviceResultsResponse | null;
}) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-6">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (!data) {
    return error ? (
      <div className="text-intent-critical flex items-center gap-2 text-200">
        <Alert width="w-3.5" />
        <span>{error}</span>
      </div>
    ) : null;
  }

  if (data.detailsPruned) {
    return (
      <div className="flex items-center gap-2 text-200 text-text-primary-50">
        <Info width="w-3.5" />
        <span>Per-miner details are no longer available.</span>
      </div>
    );
  }

  const isPending = data.status === "pending" || data.status === "processing";
  if (isPending && data.deviceResults.length === 0) {
    return <div className="text-200 text-text-primary-50">Results will appear as devices complete.</div>;
  }

  const issueResults = data.deviceResults.filter((result) => result.status !== "success" || result.errorMessage);
  const hasHiddenIssues = data.truncated && data.failureCount > 0 && issueResults.length === 0;
  const showIssues = issueResults.length > 0 || hasHiddenIssues;

  if (!showIssues && !data.truncated) return null;

  return (
    <>
      {showIssues ? (
        <div className="flex flex-col gap-2">
          <div className="text-heading-200 text-text-primary">Issues</div>
          <div className="flex flex-col divide-y divide-border-5 border-y border-border-5">
            {issueResults.length > 0 ? (
              issueResults.map((result) => <BatchDeviceResultRow key={result.deviceIdentifier} result={result} />)
            ) : (
              <div className="py-3 text-200 text-text-primary-70">Issue details may be outside the results shown.</div>
            )}
          </div>
        </div>
      ) : null}

      {data.truncated ? (
        <div className="text-200 text-text-primary-50">Some miner details may not be shown.</div>
      ) : null}
    </>
  );
}

function getDeviceResultDescription(result: CommandBatchDeviceResult) {
  return result.errorMessage ? formatActivityErrorMessage(result.errorMessage) : null;
}

function BatchDeviceResultRow({ result }: { result: CommandBatchDeviceResult }) {
  const identifiers = [result.macAddress, result.ipAddress].filter(Boolean);
  const description = getDeviceResultDescription(result) ?? "Couldn't complete";
  const timestamp = result.updatedAt ? formatActivityTimestamp(Number(result.updatedAt.seconds)) : "—";

  return (
    <section className="py-3">
      <div className="flex w-full items-start justify-between gap-4 text-left">
        <div className="flex min-w-0 flex-col gap-2">
          <div className="min-w-0">
            <div className="text-300 break-words text-text-primary">{result.deviceName || result.deviceIdentifier}</div>
            {identifiers.length > 0 ? (
              <div className="font-mono text-200 break-words text-text-primary-50">{identifiers.join(", ")}</div>
            ) : null}
          </div>
        </div>
        <span className="flex shrink-0 items-center gap-2">
          <span className="text-right text-200 whitespace-nowrap text-text-primary-50">{timestamp}</span>
        </span>
      </div>

      <div className="mt-2 text-200 break-words text-text-primary-70">{description}</div>
    </section>
  );
}
export default ActivityDetailModal;
