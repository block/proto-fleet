import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart/types";

/**
 * Pad chart data with null values for missing timestamps in the requested duration
 *
 * @param data - The actual chart data from the API
 * @param duration - The requested time duration (e.g., "24h", "7d")
 * @returns Chart data padded with null values for the full time range
 *
 * @example
 * // If user selects 24h but only has 4h of data:
 * // Returns 24h worth of datapoints, with first 20h as null values
 * const paddedData = padChartDataWithNulls(chartData, "24h");
 */
export function padChartDataWithNulls<T extends ChartData>(data: T[], duration: FleetDuration): T[] {
  if (!data || data.length === 0) {
    return data;
  }

  const durationMs = getFleetDurationMs(duration);
  const granularitySeconds = getGranularityForDuration(duration);
  const now = Date.now();
  const startTime = now - durationMs;

  // Find the first bucket boundary at or before startTime
  const granularityMs = granularitySeconds * 1000;
  const firstBucket = Math.floor(startTime / granularityMs) * granularityMs;

  // Use the last actual data point as the end boundary, not current time
  // Filter out invalid datetime values and provide fallback to current time
  // Safe: data.length === 0 is handled by early return above, so Math.max never receives empty array
  const validTimestamps = data.map((d) => d.datetime).filter((dt) => typeof dt === "number" && !isNaN(dt));
  const lastDataTimestamp = validTimestamps.length > 0 ? Math.max(...validTimestamps) : now;
  const lastBucket = Math.floor(lastDataTimestamp / granularityMs) * granularityMs;

  // Generate all expected timestamps at the appropriate granularity interval
  const expectedTimestamps: number[] = [];
  for (let bucketTime = firstBucket; bucketTime <= lastBucket; bucketTime += granularityMs) {
    expectedTimestamps.push(bucketTime);
  }

  // Create a map of existing data by timestamp
  const existingDataMap = new Map<number, T>();
  data.forEach((point) => {
    // Round to nearest granularity bucket to match expected timestamps
    const bucketTime = Math.floor(point.datetime / granularityMs) * granularityMs;
    existingDataMap.set(bucketTime, point);
  });

  // Build the padded dataset
  const paddedData: T[] = expectedTimestamps.map((timestamp) => {
    const existingPoint = existingDataMap.get(timestamp);

    if (existingPoint) {
      // Use the bucketed timestamp to ensure consistent spacing
      return { ...existingPoint, datetime: timestamp };
    }

    // Create a null datapoint for this timestamp
    // TypeScript needs help inferring the shape, so we use type assertion
    const nullPoint: ChartData = {
      datetime: timestamp,
    };

    // Add null for all numeric keys from the first data point
    if (data.length > 0) {
      const samplePoint = data[0];
      Object.keys(samplePoint).forEach((key) => {
        if (key !== "datetime" && typeof samplePoint[key as keyof T] === "number") {
          (nullPoint as any)[key] = null;
        }
      });
    }

    return nullPoint as T;
  });

  return paddedData;
}
