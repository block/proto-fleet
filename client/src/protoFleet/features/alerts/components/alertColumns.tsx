import StatusDot from "@/protoFleet/features/alerts/components/StatusDot";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

// Shared alert-row cells, reused by the history table, the active-alert rollup, and the affected-miners modal.
export const StatusCell = (entry: Pick<AlertHistoryEntry, "status">) => (
  <StatusDot dotClass={entry.status === "resolved" ? "bg-intent-success-fill" : "bg-intent-critical-fill"}>
    {entry.status === "resolved" ? "Resolved" : "Firing"}
  </StatusDot>
);

// Named on the fields it reads, so the active-alert rollup can share it with the per-instance tables.
export const AlertNameCell = (entry: Pick<AlertHistoryEntry, "alert_name" | "severity">) => (
  <span className="flex items-center gap-2">
    <span className="text-emphasis-300 text-text-primary">{entry.alert_name}</span>
    {entry.severity ? (
      <span className="rounded bg-surface-5 px-2 py-0.5 text-200 text-text-primary-50">{entry.severity}</span>
    ) : null}
  </span>
);

export const TimestampText = ({ iso }: { iso: string }) => (
  <span className="text-text-primary-50">{formatTimestamp(isoToEpochSeconds(iso))}</span>
);

export const ReceivedCell = (entry: AlertHistoryEntry) => <TimestampText iso={entry.received_at} />;

// Device name and MAC are redacted to "" without org-wide miner:read, hence the shared em-dash fallback.
export const DeviceNameCell = (entry: AlertHistoryEntry) => (
  <span className="text-text-primary-50">{entry.device_name || "—"}</span>
);

export const DeviceMacCell = (entry: AlertHistoryEntry) => (
  <span className="text-text-primary-50">{entry.device_mac || "—"}</span>
);

export const SummaryCell = (entry: Pick<AlertHistoryEntry, "summary">) => (
  <span className="text-text-primary-50">{entry.summary || "—"}</span>
);
