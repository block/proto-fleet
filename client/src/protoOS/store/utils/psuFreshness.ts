import type { MinerError, PsuTelemetry } from "../contract";

// Matches Frigate and Fleet's CT1 health window; a successful poll cannot renew a source sample.
export const PSU_MAX_AGE_MS = 120_000;
export type PsuReadingStatus = "current" | "stale" | "unavailable";

export function psuReadingStatus(at: number | undefined, now = Date.now()): PsuReadingStatus {
  if (at === undefined || !Number.isFinite(at) || at > now + 5_000) return "unavailable";
  return now - at >= PSU_MAX_AGE_MS ? "stale" : "current";
}

export function psuHealth(psu: PsuTelemetry | undefined, now = Date.now()) {
  if (psu?.observedAt && psuReadingStatus(psu.observedAt.thermalState, now) !== "current") return "unknown";
  return psu?.health ?? "unknown";
}

export function psuFreshness(psu: PsuTelemetry | undefined, now = Date.now()) {
  return {
    electrical: psuReadingStatus(psu?.observedAt?.electrical, now),
    temperatures: psuReadingStatus(psu?.observedAt?.temperatures, now),
    thermalState: psuReadingStatus(psu?.observedAt?.thermalState, now),
  };
}

export function psuReadingNotice(label: string, status: PsuReadingStatus) {
  return status === "current" ? undefined : `${label} ${status}`;
}

export function psuFaultReportNotice(errors: MinerError[], now: number) {
  const statuses = errors
    .flatMap((error) => error.reportedBy ?? [])
    .map((reporter) => psuReadingStatus(reporter.observedAt, now));
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
    const status = psuReadingStatus(reporter.observedAt, now);
    return status === "current"
      ? reporter.label
      : `${reporter.label} (${status === "stale" ? "stale" : "age unavailable"})`;
  });
  return `${error.message} · Reported by ${reporters.join(", ")}`;
}
