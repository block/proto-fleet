import type { AuditEvent, ControllerLogEntry } from "@/protoOS/api/adapters/controllerLogs";

// Log messages, actors and targets come from the controller or its clients, so
// a leading formula character is neutralised before a spreadsheet can evaluate it.
const csvCell = (value: string | undefined) => {
  const text = value ?? "";
  const safe = /^[=+\-@\t\r]/.test(text) ? `'${text}` : text;
  return `"${safe.replace(/"/g, '""')}"`;
};

export const auditEventsToCsv = (events: AuditEvent[]) => [
  "Time,Actor,Source,Controller,Action,Target,Result,Error,Details",
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
    ]
      .map(csvCell)
      .join(","),
  ),
];

/** Controller logs with source timestamps as recorded, so exports keep timezone and sub-second order. */
export const controllerLogsToCsv = (entries: ControllerLogEntry[]) => [
  "Time,Severity,Component,Message",
  ...entries.map((entry) => [entry.timestamp, entry.severity, entry.component, entry.message].map(csvCell).join(",")),
];
