import { type Timestamp, timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { containersClient } from "@/protoFleet/api/clients";
import {
  type ControllerAuditEvent,
  ControllerLogCoverage,
  ControllerLogSeverity,
  type GetControllerLogsResponse,
  type ListControllerAuditEventsResponse,
  type ControllerLogEntry as WireLogEntry,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type {
  AuditEvent,
  AuditEventPage,
  ControllerLogEntry,
  ControllerLogSnapshot,
  ControllerRecordsSource,
  ControllerLogCoverage as Coverage,
  ControllerLogSeverity as Severity,
} from "@/shared/features/controllerLogs/types";

const toWireSeverity: Record<Severity, ControllerLogSeverity> = {
  debug: ControllerLogSeverity.DEBUG,
  info: ControllerLogSeverity.INFO,
  warn: ControllerLogSeverity.WARN,
  error: ControllerLogSeverity.ERROR,
};

const fromWireSeverity = (severity: ControllerLogSeverity): Severity =>
  severity === ControllerLogSeverity.DEBUG
    ? "debug"
    : severity === ControllerLogSeverity.WARN
      ? "warn"
      : severity === ControllerLogSeverity.ERROR
        ? "error"
        : "info";

const iso = (timestamp: Timestamp | undefined) => (timestamp ? timestampDate(timestamp).toISOString() : undefined);

const toCoverage = (response: GetControllerLogsResponse): Coverage => {
  switch (response.coverage) {
    case ControllerLogCoverage.RETAINED:
      return { state: "retained" };
    case ControllerLogCoverage.LIMIT_REACHED:
      return { state: "limit-reached" };
    case ControllerLogCoverage.NO_RESPONSE:
      return { state: "no-response" };
    default:
      return { state: "interrupted", reason: response.interruption || "the read ended early" };
  }
};

const toLogEntry = (entry: WireLogEntry): ControllerLogEntry => ({
  timestamp: iso(entry.time),
  severity: fromWireSeverity(entry.severity),
  component: entry.component,
  message: entry.message,
  truncated: entry.messageTruncated || undefined,
});

export const toControllerLogSnapshot = (response: GetControllerLogsResponse): ControllerLogSnapshot => ({
  entries: response.entries.map(toLogEntry),
  limit: response.limit,
  coverage: toCoverage(response),
  controllerId: response.controllerId || undefined,
  controllerIds: response.controllerIds,
  fetchedAt: iso(response.readAt) ?? new Date().toISOString(),
});

const toAuditEvent = (event: ControllerAuditEvent, index: number): AuditEvent => ({
  // Rows need a stable key; an event without an ID keeps its page position.
  id: event.id || `position-${index}`,
  time: iso(event.time),
  actor: event.actor,
  source: event.source,
  node: event.node,
  action: event.action,
  target: event.target,
  result: event.result,
  error: event.error || undefined,
  details: event.detailsJson || undefined,
  truncated: event.truncated || undefined,
});

export const toAuditEventPage = (response: ListControllerAuditEventsResponse): AuditEventPage => ({
  events: response.events.map(toAuditEvent),
  limit: response.limit,
  limitReached: response.limitReached,
  controllerId: response.controllerId || undefined,
  controllerIds: response.controllerIds,
  fetchedAt: iso(response.readAt) ?? new Date().toISOString(),
});

type AuthErrorHandler = (props: { error: unknown }) => void;

/** Reads a container's controller records through Fleet, and its Fleet Node when assigned. */
export const fleetControllerRecords = (
  deviceIdentifier: string,
  handleAuthErrors: AuthErrorHandler,
): ControllerRecordsSource => {
  const withAuth = async <T>(read: () => Promise<T>) => {
    try {
      return await read();
    } catch (error) {
      handleAuthErrors({ error });
      throw error;
    }
  };
  return {
    readLogs: (query, signal) =>
      withAuth(async () =>
        toControllerLogSnapshot(
          await containersClient.getControllerLogs(
            {
              deviceIdentifier,
              controllerId: query.controllerId ?? "",
              minSeverity: query.minSeverity ? toWireSeverity[query.minSeverity] : ControllerLogSeverity.UNSPECIFIED,
              component: query.component ?? "",
              limit: query.limit,
            },
            { signal },
          ),
        ),
      ),
    listAuditEvents: (query, signal) =>
      withAuth(async () =>
        toAuditEventPage(
          await containersClient.listControllerAuditEvents(
            {
              deviceIdentifier,
              controllerId: query.controllerId ?? "",
              start: timestampFromDate(new Date(query.start)),
              end: timestampFromDate(new Date(query.end)),
              limit: query.limit,
              result: query.result ?? "",
            },
            { signal },
          ),
        ),
      ),
  };
};
