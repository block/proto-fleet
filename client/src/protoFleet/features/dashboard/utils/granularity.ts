import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";

const DEFAULT_GRANULARITY_SECONDS = 90;
const GRANULARITY_48H_SECONDS = 180; // 3 minutes
const GRANULARITY_5D_SECONDS = 600; // 10 minutes
const GRANULARITY_14D_SECONDS = 1260; // 21 minutes (~960 buckets for 14d)
const GRANULARITY_30D_SECONDS = 2700; // 45 minutes (~960 buckets for 30d)
const GRANULARITY_90D_SECONDS = 8100; // 2.25 hours (~960 buckets for 90d)
const GRANULARITY_1Y_SECONDS = 32850; // ~9 hours (~960 buckets for 1y)

const HOURS_48_IN_SECONDS = 48 * 3600;
const DAYS_5_IN_SECONDS = 5 * 24 * 3600;
const DAYS_14_IN_SECONDS = 14 * 24 * 3600;
const DAYS_30_IN_SECONDS = 30 * 24 * 3600;
const DAYS_90_IN_SECONDS = 90 * 24 * 3600;
const DAYS_365_IN_SECONDS = 365 * 24 * 3600;

/**
 * Calculate optimal granularity based on duration to stay within backend LIMIT.
 * Backend has LIMIT of 1000 buckets, so we adjust granularity for longer durations.
 *
 * Note: These thresholds are intentionally different from backend data source selection
 * (raw ≤24h, hourly 24h-10d, daily >10d). The backend data source determines WHICH table
 * to query for performance, while this granularity controls HOW MANY buckets to return.
 * The backend aggregates its chosen data source to match this requested granularity.
 */
export const getGranularityForDuration = (duration: FleetDuration): number => {
  const totalSeconds = getFleetDurationMs(duration) / 1000;

  // Granularity thresholds ensure ~960 buckets max for chart rendering performance
  if (totalSeconds >= DAYS_365_IN_SECONDS) return GRANULARITY_1Y_SECONDS; // 1y -> ~9 hours
  if (totalSeconds >= DAYS_90_IN_SECONDS) return GRANULARITY_90D_SECONDS; // 90d -> 2.25 hours
  if (totalSeconds >= DAYS_30_IN_SECONDS) return GRANULARITY_30D_SECONDS; // 30d -> 45 min
  // Note: No "14d" duration option exists, but this threshold affects 10d queries (10d < 14d, so uses 10min)
  if (totalSeconds >= DAYS_14_IN_SECONDS) return GRANULARITY_14D_SECONDS; // 14d+ -> 21 min
  if (totalSeconds >= DAYS_5_IN_SECONDS) return GRANULARITY_5D_SECONDS; // 5d -> 10 min
  if (totalSeconds >= HOURS_48_IN_SECONDS) return GRANULARITY_48H_SECONDS; // 48h -> 3 min
  return DEFAULT_GRANULARITY_SECONDS; // Default for shorter durations
};
