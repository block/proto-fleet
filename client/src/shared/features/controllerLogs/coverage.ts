import type { AuditEventPage, ControllerLogEntry, ControllerLogSnapshot } from "@/shared/features/controllerLogs/types";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

export interface CoverageNotice {
  intent: "information" | "warning";
  title: string;
  subtitle: string;
}

export const formatControllerTime = (iso: string | undefined) => {
  const ms = iso ? Date.parse(iso) : NaN;
  return Number.isFinite(ms) ? formatActivityTimestamp(ms / 1000) : "";
};

/** Entries at or after `windowStartMs`; entries without a time stay visible rather than vanish. */
export const entriesInWindow = (entries: ControllerLogEntry[], windowStartMs: number | undefined) =>
  windowStartMs === undefined
    ? entries
    : entries.filter((entry) => {
        const ms = entry.timestamp ? Date.parse(entry.timestamp) : NaN;
        return !Number.isFinite(ms) || ms >= windowStartMs;
      });

/**
 * Explains what part of the requested range the snapshot covers. The entries
 * are the newest contiguous matches, so a range that starts at or after the
 * oldest entry is fully covered and needs no notice.
 */
export function logCoverageNotice(
  snapshot: ControllerLogSnapshot,
  windowStartMs: number | undefined,
): CoverageNotice | undefined {
  const { coverage, entries, limit } = snapshot;
  if (coverage.state === "interrupted") {
    return {
      intent: "warning",
      title: "Log stream interrupted",
      subtitle: `Received ${entries.length} entries before the read stopped: ${coverage.reason}. Newer entries may be missing.`,
    };
  }
  if (coverage.state === "no-response") {
    return {
      intent: "information",
      title: "No log entries received",
      subtitle: "Either no entries match these filters, or the controller is slow to respond.",
    };
  }
  const oldest = entries.find((entry) => entry.timestamp)?.timestamp;
  const oldestMs = oldest ? Date.parse(oldest) : NaN;
  if (windowStartMs !== undefined && Number.isFinite(oldestMs) && windowStartMs >= oldestMs) return undefined;
  return {
    intent: "information",
    title: oldest ? `Logs available from ${formatControllerTime(oldest)}` : "Logs are kept in controller memory only",
    subtitle:
      coverage.state === "limit-reached"
        ? // Fewer than the limit means long entries filled the read's size limit.
          entries.length < limit
          ? `Only the latest ${entries.length.toLocaleString()} matching entries fit in one read.`
          : `Only the latest ${limit.toLocaleString()} matching entries are returned.`
        : oldest
          ? "Held in controller memory. Earlier entries are lost on restart or when the buffer fills."
          : "Earlier entries are lost on restart or when the buffer fills.",
  };
}

export function auditCoverageNotice(page: AuditEventPage): CoverageNotice | undefined {
  if (!page.limitReached) return undefined;
  const oldest = page.events[page.events.length - 1]?.time;
  return {
    intent: "information",
    // A long page can be cut below the limit to fit one read.
    title:
      page.events.length === 1
        ? "Showing the newest event"
        : `Showing the newest ${page.events.length.toLocaleString()} events`,
    subtitle: oldest
      ? `Events before ${formatControllerTime(oldest)} in this time range aren't shown.`
      : "Older events in this time range aren't shown.",
  };
}
