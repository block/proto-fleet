import StatusDot from "@/protoFleet/features/alerts/components/StatusDot";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

// Shared alert-row cells, reused by the history table and the per-miner active-alerts modal.
export const StatusBadge = ({ status }: { status: AlertHistoryEntry["status"] }) => (
  <StatusDot dotClass={status === "resolved" ? "bg-intent-success-fill" : "bg-intent-critical-fill"}>
    {status === "resolved" ? "Resolved" : "Firing"}
  </StatusDot>
);

export const AlertNameCell = (entry: AlertHistoryEntry) => (
  <span className="flex items-center gap-2">
    <span className="text-emphasis-300 text-text-primary">{entry.alert_name}</span>
    {entry.severity ? (
      <span className="rounded bg-surface-5 px-2 py-0.5 text-200 text-text-primary-50">{entry.severity}</span>
    ) : null}
  </span>
);

export const ReceivedCell = (entry: AlertHistoryEntry) => (
  <span className="text-text-primary-50">{formatTimestamp(isoToEpochSeconds(entry.received_at))}</span>
);

export const SummaryCell = (entry: AlertHistoryEntry) => (
  <span className="text-text-primary-50">{entry.summary || "—"}</span>
);
