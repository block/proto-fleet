// CT1 health observations expire after two minutes. Polling does not renew them.
export const HEALTH_MAX_AGE_MS = 120_000;
const MAX_FUTURE_SKEW_MS = 5_000;
export type ReadingStatus = "current" | "stale" | "unavailable";

export function readingStatus(at: number | undefined, now = Date.now()): ReadingStatus {
  if (at === undefined || !Number.isFinite(at) || at > now + MAX_FUTURE_SKEW_MS) return "unavailable";
  return now - at >= HEALTH_MAX_AGE_MS ? "stale" : "current";
}

/** Convert a health timestamp and optional source-sample age into observation time. */
export function observationTime(timestamp: string | undefined, age = 0, now = Date.now()): number | undefined {
  const at = Date.parse(timestamp ?? "");
  // Validate the enclosing frame before subtracting age; age cannot hide a future frame.
  if (readingStatus(at, now) === "unavailable" || !Number.isFinite(age) || age < 0) return undefined;
  return at - age;
}
