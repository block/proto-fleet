import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

// Shared alert-row cells for the affected-miners modal's per-instance tables.
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

export const SummaryCell = (entry: AlertHistoryEntry) => (
  <span className="text-text-primary-50">{entry.summary || "—"}</span>
);
