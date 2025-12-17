import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

/**
 * Helper to merge items avoiding timestamp duplicates
 *
 * Works with both timestamp and openTime fields commonly found in telemetry data
 */
export function mergeByTimestamp<T extends { timestamp?: { seconds?: bigint } } | { openTime?: { seconds?: bigint } }>(
  existing: T[],
  incoming: T[],
): T[] {
  const getTimestamp = (item: T): string | undefined => {
    if ("timestamp" in item) {
      return item.timestamp?.seconds?.toString();
    }
    if ("openTime" in item) {
      return item.openTime?.seconds?.toString();
    }
    return undefined;
  };

  // Only include defined timestamps in the Set
  const existingTimestamps = new Set(
    existing.map(getTimestamp).filter((timestamp): timestamp is string => timestamp !== undefined),
  );
  const newItems = incoming.filter((item) => {
    const timestamp = getTimestamp(item);
    return timestamp && !existingTimestamps.has(timestamp);
  });

  return [...existing, ...newItems];
}

/**
 * Helper to merge status counts avoiding duplicates
 *
 * Handles optional arrays by returning the non-empty one, or merging both if both exist
 */
export function mergeStatusCounts<T extends { timestamp?: { seconds?: bigint } }>(
  historical: T[] | undefined,
  streaming: T[] | undefined,
): T[] {
  if (!historical || historical.length === 0) return streaming || [];
  if (!streaming || streaming.length === 0) return historical;

  return mergeByTimestamp(historical, streaming);
}

/**
 * Helper to merge metrics avoiding duplicates
 *
 * Type-safe wrapper around mergeByTimestamp specifically for Metric arrays
 */
export function mergeMetrics(historical: Metric[], streaming: Metric[] | undefined): Metric[] {
  if (!streaming || streaming.length === 0) return historical;

  return mergeByTimestamp(historical, streaming);
}
