import type { AuditEvent } from "@/protoOS/api/adapters/controllerLogs";

export const auditResultLabel = (result: AuditEvent["result"]) =>
  result === "ok" ? "Succeeded" : result === "error" ? "Failed" : result || "Unknown";

export const auditResultStatus = (result: AuditEvent["result"]) => (result === "ok" ? "normal" : "error");

/** Pretty-prints a JSON payload, falling back to the text as recorded. */
export const formatAuditDetails = (details: string) => {
  try {
    return JSON.stringify(JSON.parse(details), null, 2);
  } catch {
    return details;
  }
};
