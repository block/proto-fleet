import type { AuditEvent, ControllerLogEntry } from "@/shared/features/controllerLogs/types";

// Log messages, actors and targets come from the controller or its clients, so
// a leading formula character is neutralised before a spreadsheet can evaluate it.
const csvCell = (value: string | undefined) => {
  const text = value ?? "";
  const safe = /^[=+\-@\t\r]/.test(text) ? `'${text}` : text;
  return `"${safe.replace(/"/g, '""')}"`;
};

// Only Fleet relays bounded records, so the Truncated column appears only
// when a record in the export was shortened.
export const auditEventsToCsv = (events: AuditEvent[]) => {
  const truncation = events.some((event) => event.truncated);
  return [
    "Time,Actor,Source,Controller,Action,Target,Result,Error,Details" + (truncation ? ",Truncated" : ""),
    ...events.map((event) =>
      [
        event.time,
        event.actor,
        event.source,
        event.node,
        event.action,
        event.target,
        event.result,
        event.error,
        event.details,
        ...(truncation ? [event.truncated ? "yes" : ""] : []),
      ]
        .map(csvCell)
        .join(","),
    ),
  ];
};

/** Controller logs with source timestamps as recorded, so exports keep timezone and sub-second order. */
export const controllerLogsToCsv = (entries: ControllerLogEntry[]) => {
  const truncation = entries.some((entry) => entry.truncated);
  return [
    "Time,Severity,Component,Message" + (truncation ? ",Truncated" : ""),
    ...entries.map((entry) =>
      [
        entry.timestamp,
        entry.severity,
        entry.component,
        entry.message,
        ...(truncation ? [entry.truncated ? "yes" : ""] : []),
      ]
        .map(csvCell)
        .join(","),
    ),
  ];
};
