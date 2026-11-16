import type { AdapterRequestOptions } from "../types";
import { LOG_TAIL_DEADLINE_MS, type LogTailRead } from "./connectClient";
import type {
  SystemServiceGetLogsParams,
  SystemServiceListAuditEventsParams,
  V2AuditEvent,
  V2GetLogsResponse,
  V2GetSystemStatusResponse,
  V2ListAuditEventsResponse,
} from "./generated/generatedApi";
import {
  type AuditEventPage,
  type AuditEventQuery,
  type ControllerLogEntry,
  type ControllerLogQuery,
  type ControllerLogSeverity,
  type ControllerLogSnapshot,
} from "@/shared/features/controllerLogs/types";

const wireLevels: Record<ControllerLogSeverity, NonNullable<SystemServiceGetLogsParams["level"]>> = {
  debug: "LOG_LEVEL_DEBUG",
  info: "LOG_LEVEL_INFO",
  warn: "LOG_LEVEL_WARNING",
  error: "LOG_LEVEL_ERROR",
};

const toSeverity = (level: V2GetLogsResponse["level"]): ControllerLogSeverity => {
  switch (level) {
    case "LOG_LEVEL_DEBUG":
      return "debug";
    case "LOG_LEVEL_WARNING":
      return "warn";
    case "LOG_LEVEL_ERROR":
      return "error";
    default:
      return "info";
  }
};

const toLogEntry = (wire: V2GetLogsResponse): ControllerLogEntry => ({
  timestamp: wire.timestamp || undefined,
  severity: toSeverity(wire.level),
  component: wire.component ?? "",
  message: wire.message ?? "",
});

/** Audit reads are unary, so a controller that never answers would stall polling without this. */
export const AUDIT_READ_TIMEOUT_MS = 10_000;

/**
 * Runs `request` with the caller's signal plus a deadline. Expiry rejects with
 * an ordinary error, not an abort, so callers report it like any failed read.
 */
export async function withDeadline<T>(
  params: AdapterRequestOptions,
  ms: number,
  request: (params: AdapterRequestOptions) => Promise<T>,
): Promise<T> {
  const outer = params.signal ?? undefined;
  const giveUp = new AbortController();
  const timer = setTimeout(() => giveUp.abort(), ms);
  let failure: unknown;
  try {
    return await request({ ...params, signal: outer ? AbortSignal.any([outer, giveUp.signal]) : giveUp.signal });
  } catch (error) {
    failure = error;
  } finally {
    clearTimeout(timer);
  }
  if (outer?.aborted) throw outer.reason;
  if (giveUp.signal.aborted) throw new Error(`The controller did not respond within ${ms / 1000} seconds.`);
  throw failure;
}

export async function readControllerLogs(
  readLogTail: (query: SystemServiceGetLogsParams, params: AdapterRequestOptions) => Promise<LogTailRead>,
  getStatus: (params: AdapterRequestOptions) => Promise<{ data: V2GetSystemStatusResponse }>,
  query: ControllerLogQuery,
  params: AdapterRequestOptions,
): Promise<ControllerLogSnapshot> {
  // Identity is best effort: logs without a controller ID are still evidence,
  // so a stalled status call gives up rather than holding back the logs.
  const identity = withDeadline(params, LOG_TAIL_DEADLINE_MS, getStatus).then(
    ({ data }) => data.status?.serverId || undefined,
    () => {
      if (params.signal?.aborted) throw params.signal.reason;
      return undefined;
    },
  );
  identity.catch(() => undefined);
  const tail = await readLogTail(
    {
      tail: query.limit,
      level: query.minSeverity ? wireLevels[query.minSeverity] : undefined,
      component: query.component || undefined,
    },
    params,
  );
  const entries = tail.entries.map(toLogEntry);
  return {
    entries,
    limit: query.limit,
    coverage: tail.interruption
      ? { state: "interrupted", reason: tail.interruption }
      : tail.noResponse
        ? { state: "no-response" }
        : entries.length >= query.limit
          ? { state: "limit-reached" }
          : { state: "retained" },
    controllerId: await identity,
    fetchedAt: new Date().toISOString(),
  };
}

const toAuditEvent = (wire: V2AuditEvent, index: number) => ({
  id: wire.id || `index-${index}`,
  time: wire.time || undefined,
  actor: wire.actor ?? "",
  source: wire.source ?? "",
  node: wire.node ?? "",
  action: wire.action ?? "",
  target: wire.target ?? "",
  result: wire.result ?? "",
  error: wire.error || undefined,
  details: wire.detailsJson || undefined,
});

export async function listControllerAuditEvents(
  listAuditEvents: (
    query: SystemServiceListAuditEventsParams,
    params: AdapterRequestOptions,
  ) => Promise<{ data: V2ListAuditEventsResponse }>,
  query: AuditEventQuery,
  params: AdapterRequestOptions,
): Promise<AuditEventPage> {
  const { data } = await withDeadline(params, AUDIT_READ_TIMEOUT_MS, (request) =>
    listAuditEvents(
      {
        "range.start": query.start,
        "range.end": query.end,
        limit: query.limit,
        result: query.result,
      },
      request,
    ),
  );
  const events = (data.events ?? []).map(toAuditEvent);
  return {
    events,
    limit: query.limit,
    limitReached: events.length >= query.limit,
    fetchedAt: new Date().toISOString(),
  };
}
