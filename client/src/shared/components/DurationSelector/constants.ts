// ProtoOS durations (used by single miner dashboard)
export const durations = ["1h", "12h", "24h", "48h", "5d"] as const;

export type Duration = (typeof durations)[number];

// ProtoFleet durations
export const fleetDurations = ["1h", "24h", "7d", "30d", "90d", "1y"] as const;

export type FleetDuration = (typeof fleetDurations)[number];

export const durationToMs: Record<Duration, number> = {
  "1h": 60 * 60 * 1000,
  "12h": 12 * 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "48h": 48 * 60 * 60 * 1000,
  "5d": 5 * 24 * 60 * 60 * 1000,
};

export const fleetDurationToMs: Record<FleetDuration, number> = {
  "1h": 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
  "90d": 90 * 24 * 60 * 60 * 1000,
  "1y": 365 * 24 * 60 * 60 * 1000,
};

/**
 * Runtime-safe duration guard for persisted/untyped values.
 */
export const isDuration = (value: unknown): value is Duration => {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(durationToMs, value);
};

/**
 * Runtime-safe Fleet duration guard for persisted/untyped values.
 */
export const isFleetDuration = (value: unknown): value is FleetDuration => {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(fleetDurationToMs, value);
};

/**
 * Converts a ProtoOS duration value to milliseconds.
 * This helper is ProtoOS-only and does not accept Fleet-only durations.
 */
export const getDurationMs = (duration: Duration): number => {
  return durationToMs[duration];
};

/**
 * Converts a ProtoFleet duration value to milliseconds.
 */
export const getFleetDurationMs = (duration: FleetDuration): number => {
  return fleetDurationToMs[duration];
};
