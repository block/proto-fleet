import type { ControllerObservation } from "@/protoOS/api/adapters/controllerDiagnostics";

// Frigate samples roles and peer reachability every five seconds. Judge the
// original sample time as well as the reported freshness: receipt is not refresh.
export const OBSERVATION_MAX_AGE_MS = 15_000;
const CLOCK_SKEW_MS = 5_000;

export type ObservationState = "fresh" | "stale" | "unknown" | "missing";

export const observationState = (
  observation: ControllerObservation | undefined,
  nowMs: number,
  maxAgeMs = OBSERVATION_MAX_AGE_MS,
): ObservationState => {
  if (!observation) return "missing";
  const { freshness, observedAtMs } = observation;
  if (freshness === "unspecified" || observedAtMs === undefined || observedAtMs > nowMs + CLOCK_SKEW_MS) {
    return "unknown";
  }
  return freshness === "stale" || nowMs - observedAtMs > maxAgeMs ? "stale" : "fresh";
};

export const formatAge = (atMs: number, nowMs: number) => {
  const seconds = Math.max(0, Math.floor((nowMs - atMs) / 1_000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h ago` : `${Math.floor(hours / 24)}d ago`;
};

const stateLabels: Record<ObservationState, string> = {
  fresh: "Fresh",
  stale: "Stale",
  unknown: "Freshness unknown",
  missing: "Not observed",
};

export const observationSummary = (
  observation: ControllerObservation | undefined,
  state: ObservationState,
  nowMs: number,
) => {
  const parts = [stateLabels[state]];
  const observedAtMs = observation?.observedAtMs;
  if (observedAtMs !== undefined && observedAtMs <= nowMs + CLOCK_SKEW_MS) {
    parts.push(`observed ${formatAge(observedAtMs, nowMs)}`);
  }
  const lastSuccessfulAtMs = observation?.lastSuccessfulAtMs;
  if (state !== "fresh" && lastSuccessfulAtMs !== undefined && lastSuccessfulAtMs !== observedAtMs) {
    parts.push(`last success ${formatAge(lastSuccessfulAtMs, nowMs)}`);
  }
  return parts.join(" · ");
};
