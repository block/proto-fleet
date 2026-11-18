/**
 * The parts of a recovery evidence document a reader needs to judge it: which
 * sources were captured, what was left out, and the stack dropouts it saw. Logs,
 * CAN status and raw readings stay in the downloaded document.
 */
export interface EvidenceSource {
  name: string;
  status: "ok" | "unavailable" | "error" | "unknown";
  detail?: string;
  count?: number;
}

export interface EvidenceStackDropout {
  stack?: number;
  detectedAt?: string;
  /** Absent while the stack had not come back when the snapshot was taken. */
  clearedAt?: string;
}

export interface EvidenceDocument {
  sources: EvidenceSource[];
  omitted: string[];
  stackDropouts: EvidenceStackDropout[];
}

const record = (value: unknown): Record<string, unknown> | undefined =>
  value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
const text = (value: unknown) => (typeof value === "string" && value.trim() ? value.trim() : undefined);
const number = (value: unknown) => (typeof value === "number" && Number.isFinite(value) ? value : undefined);
const STATUSES = new Set(["ok", "unavailable", "error"]);

/** Tolerates missing or malformed parts; returns undefined only when the document is not a JSON object. */
export function parseEvidenceDocument(json: string): EvidenceDocument | undefined {
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return undefined;
  }
  const document = record(parsed);
  if (!document) return undefined;
  const sources = Object.entries(record(document.sources) ?? {})
    .map(([name, value]): EvidenceSource => {
      const source = record(value) ?? {};
      const status = text(source.status) ?? "";
      return {
        name,
        status: STATUSES.has(status) ? (status as EvidenceSource["status"]) : "unknown",
        detail: text(source.detail),
        count: number(source.count),
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
  const omitted = (Array.isArray(document.omitted) ? document.omitted : []).flatMap((value) => text(value) ?? []);
  const dropouts = record(document.events)?.stack_dropouts;
  const stackDropouts = (Array.isArray(dropouts) ? dropouts : []).flatMap((value) => {
    const dropout = record(value);
    return dropout
      ? [{ stack: number(dropout.stack), detectedAt: text(dropout.detected_at), clearedAt: text(dropout.cleared_at) }]
      : [];
  });
  return { sources, omitted, stackDropouts };
}
