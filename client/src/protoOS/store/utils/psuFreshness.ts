import type { MinerError, PsuTelemetry } from "../contract";

import { readingStatus, type ReadingStatus } from "./readingFreshness";

export function psuHealth(psu: PsuTelemetry | undefined, now = Date.now()) {
  if (psu?.observedAt && readingStatus(psu.observedAt.thermalState, now) !== "current") return "unknown";
  return psu?.health ?? "unknown";
}

export function psuFreshness(psu: PsuTelemetry | undefined, now = Date.now()) {
  return {
    electrical: readingStatus(psu?.observedAt?.electrical, now),
    temperatures: readingStatus(psu?.observedAt?.temperatures, now),
    thermalState: readingStatus(psu?.observedAt?.thermalState, now),
  };
}

export function psuReadingNotice(label: string, status: ReadingStatus) {
  return status === "current" ? undefined : `${label} ${status}`;
}

export function psuFaultReportNotice(errors: MinerError[], now: number) {
  const statuses = errors
    .flatMap((error) => error.reportedBy ?? [])
    .map((reporter) => readingStatus(reporter.observedAt, now));
  return [
    statuses.includes("stale")
      ? statuses.every((status) => status === "stale")
        ? "Board fault reports are stale"
        : "Some board fault reports are stale"
      : undefined,
    statuses.includes("unavailable") ? "Board fault report age unavailable" : undefined,
  ]
    .filter(Boolean)
    .join(". ");
}

export function psuFaultMessage(error: MinerError, now: number) {
  if (!error.reportedBy?.length) return error.message;
  const reporters = error.reportedBy.map((reporter) => {
    const status = readingStatus(reporter.observedAt, now);
    return status === "current"
      ? reporter.label
      : `${reporter.label} (${status === "stale" ? "stale" : "age unavailable"})`;
  });
  return `${error.message} · Reported by ${reporters.join(", ")}`;
}
