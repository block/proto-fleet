/** Controller log and audit-trail contracts for CT1 (Frigate) controllers, read by ProtoOS and Fleet. */

export type ControllerLogSeverity = "debug" | "info" | "warn" | "error";

/** Frigate caps a log tail and an audit page at 1000 entries. */
export const CONTROLLER_LOG_LIMIT = 1000;
export const AUDIT_EVENT_LIMIT = 1000;
/** Frigate rejects audit windows longer than seven days. */
export const AUDIT_WINDOW_MAX_MS = 7 * 24 * 60 * 60 * 1000;

export interface ControllerLogQuery {
  limit: number;
  /** Server ID of one HA controller; omitted means the read-preferred controller. */
  controllerId?: string;
  /** Lowest severity to include. The controller defaults to info. */
  minSeverity?: ControllerLogSeverity;
  /** Exact component name; omitted means every component. */
  component?: string;
}

export interface ControllerLogEntry {
  /** Controller-recorded time, ISO 8601. */
  timestamp?: string;
  severity: ControllerLogSeverity;
  component: string;
  message: string;
  /** True when the message was shortened to fit the read's size bound. */
  truncated?: boolean;
}

/**
 * How much of the controller's log the snapshot covers. A controller keeps
 * only a bounded in-memory tail, so no snapshot is ever a complete history.
 * - `retained`: fewer than `limit` entries came back, so this is everything the
 *   controller still holds for the filters. Older entries were evicted or lost
 *   at the last restart.
 * - `limit-reached`: the controller returned `limit` entries; older retained
 *   entries for the filters were not returned.
 * - `interrupted`: the read stopped before the tail was known to be finished,
 *   because the stream failed, the controller was still sending at the
 *   deadline, or the stream paused before the newest matching entry arrived.
 * - `no-response`: the controller sent nothing, so no matching entries and a
 *   slow controller cannot be told apart.
 */
export type ControllerLogCoverage =
  | { state: "retained" }
  | { state: "limit-reached" }
  | { state: "interrupted"; reason: string }
  | { state: "no-response" };

export interface ControllerLogSnapshot {
  /** Oldest first, as the controller sent them. */
  entries: ControllerLogEntry[];
  limit: number;
  coverage: ControllerLogCoverage;
  /** Server ID of the controller that produced the logs, when it reports one. */
  controllerId?: string;
  /** Controllers that can be addressed by `controllerId`, when the source reports them. */
  controllerIds?: string[];
  /** When the snapshot was read, ISO 8601. */
  fetchedAt: string;
}

export type AuditResult = "ok" | "error";

export interface AuditEventQuery {
  /** ISO 8601 window bounds; the controller caps the window at seven days. */
  start: string;
  end: string;
  limit: number;
  result?: AuditResult;
  /** Server ID of one HA controller; omitted means the read-preferred controller. */
  controllerId?: string;
}

export interface AuditEvent {
  id: string;
  /** Controller-recorded time, ISO 8601. */
  time?: string;
  /** Client-supplied identity ("user@host") or subsystem name. */
  actor: string;
  /** "grpc:<addr>", "rest:<addr>", or "system". */
  source: string;
  /** Server ID of the controller that performed the action. */
  node: string;
  action: string;
  target: string;
  result: string;
  error?: string;
  /** Structured JSON payload as recorded; omitted when empty. */
  details?: string;
  /** True when a field was shortened to fit the read's size bound. */
  truncated?: boolean;
}

export interface AuditEventPage {
  /** Newest first, as the controller sent them. */
  events: AuditEvent[];
  limit: number;
  /** True when the page is full, so older events in the window were not returned. */
  limitReached: boolean;
  /** Server ID of the controller that answered, when the source reports it. */
  controllerId?: string;
  /** Controllers that can be addressed by `controllerId`, when the source reports them. */
  controllerIds?: string[];
  fetchedAt: string;
}

/** Reads one controller record view; the signal cancels it when the view closes or the filters change. */
export type ControllerRecordsRead<Query, Data> = (query: Query, signal: AbortSignal) => Promise<Data>;

/** How an app reads a controller's records. Transport stays with the app. */
export interface ControllerRecordsSource {
  readLogs?: ControllerRecordsRead<ControllerLogQuery, ControllerLogSnapshot>;
  listAuditEvents?: ControllerRecordsRead<AuditEventQuery, AuditEventPage>;
  /** Re-read on this interval while open. Omitted means read on open, on filter change and on Refresh. */
  pollIntervalMs?: number;
}
