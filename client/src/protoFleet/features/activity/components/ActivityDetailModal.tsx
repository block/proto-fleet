import { type ReactNode, useEffect } from "react";
import clsx from "clsx";
import type { JsonObject, JsonValue } from "@bufbuild/protobuf";

import type { ActivityEntry } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import type {
  CommandBatchDeviceResult,
  GetCommandBatchDeviceResultsResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useCommandBatchDeviceResults } from "@/protoFleet/api/useCommandBatchDeviceResults";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { baseEventType, isCompletedEvent } from "@/protoFleet/features/activity/utils/eventType";
import {
  formatActivityDescription,
  formatActivityErrorMessage,
  formatActivityErrorSummary,
} from "@/protoFleet/features/activity/utils/formatActivityDescription";
import { formatLabel } from "@/protoFleet/features/activity/utils/formatLabel";
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

type MetadataRow = {
  label: string;
  value: ReactNode;
};

const hiddenMetadataKeys = new Set([
  "cohort_id",
  "device_identifier",
  "device_identifiers",
  "idempotency_key",
  "label",
  "rig_uuid",
  "rig_uuids",
]);

const cohortMetadataLabels: Record<string, string> = {
  update_kind: "Update kind",
  old_expires_at: "Expiry before",
  new_expires_at: "Expiry after",
  old_firmware_file_id: "Firmware before",
  new_firmware_file_id: "Firmware after",
  affected_member_count: "Affected miners",
  desired_config_changed: "Config changed",
  desired_config_cleared: "Config cleared",
};

function metadataRows(entry: ActivityEntry, hasBatch: boolean): MetadataRow[] {
  if (hasBatch || !entry.metadata) return [];
  if (baseEventType(entry.eventType) === "cohort_updated") {
    return cohortUpdateMetadataRows(entry.metadata);
  }
  return genericMetadataRows(entry.metadata);
}

function cohortUpdateMetadataRows(metadata: JsonObject): MetadataRow[] {
  const rows: MetadataRow[] = [];
  addMetadataRow(rows, "Update kind", formatLabel(stringMetadataValue(metadata.update_kind)));
  addMetadataRow(rows, "Expiry before", formatTimestampMetadataValue(metadata.old_expires_at));
  addMetadataRow(rows, "Expiry after", formatTimestampMetadataValue(metadata.new_expires_at));
  addMetadataRow(
    rows,
    "Target",
    [stringMetadataValue(metadata.manufacturer), stringMetadataValue(metadata.model)].filter(Boolean).join(" "),
  );
  addMetadataRow(rows, "Firmware before", formatNullableMetadataValue(metadata.old_firmware_file_id));
  addMetadataRow(rows, "Firmware after", formatNullableMetadataValue(metadata.new_firmware_file_id));
  addMetadataRow(rows, "Affected miners", formatMinerCount(metadata.affected_member_count));
  addMetadataRow(rows, "Config changed", formatBooleanMetadataValue(metadata.desired_config_changed));
  addMetadataRow(rows, "Config cleared", formatBooleanMetadataValue(metadata.desired_config_cleared));
  return rows;
}

function genericMetadataRows(metadata: JsonObject): MetadataRow[] {
  return Object.entries(metadata).flatMap(([key, value]) => {
    if (hiddenMetadataKeys.has(key) || Array.isArray(value) || isJsonObject(value)) return [];
    return [{ label: cohortMetadataLabels[key] ?? formatLabel(key), value: formatGenericMetadataValue(value) }];
  });
}

function addMetadataRow(rows: MetadataRow[], label: string, value: ReactNode) {
  if (value == null || value === "") return;
  rows.push({ label, value });
}

function stringMetadataValue(value: JsonValue | undefined): string {
  return typeof value === "string" ? value : "";
}

function formatTimestampMetadataValue(value: JsonValue | undefined): string {
  if (typeof value !== "string") return "";
  const seconds = Date.parse(value) / 1000;
  if (!Number.isFinite(seconds)) return value;
  return formatActivityTimestamp(seconds);
}

function formatNullableMetadataValue(value: JsonValue | undefined): string {
  if (value == null) return "None";
  if (typeof value === "string" && value.trim() !== "") return value;
  return "";
}

function formatMinerCount(value: JsonValue | undefined): string {
  if (typeof value !== "number") return "";
  return `${value} ${value === 1 ? "miner" : "miners"}`;
}

function formatBooleanMetadataValue(value: JsonValue | undefined): string {
  return typeof value === "boolean" ? (value ? "Yes" : "No") : "";
}

function formatGenericMetadataValue(value: JsonValue | undefined): ReactNode {
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "number") return value.toLocaleString();
  if (typeof value === "string") return value;
  if (value == null) return "None";
  return "";
}

function isJsonObject(value: JsonValue | undefined): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

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

  const detailMetadataRows = metadataRows(entry, batchId != null);
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
          <SummaryRow label="Result" divider={hasErrorMessage || detailMetadataRows.length > 0}>
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
            <SummaryRow
              label="Issue"
              className="text-intent-critical break-words"
              divider={detailMetadataRows.length > 0}
            >
              {formatActivityErrorSummary(entry.errorMessage)}
            </SummaryRow>
          ) : null}
          {detailMetadataRows.map((row, index) => (
            <SummaryRow key={row.label} label={row.label} divider={index < detailMetadataRows.length - 1}>
              {row.value}
            </SummaryRow>
          ))}
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
