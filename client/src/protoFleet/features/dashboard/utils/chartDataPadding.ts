import type { Duration } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart/types";

const GRANULARITY_SECONDS = 90; // 90 seconds per bucket

/**
 * Convert duration string to seconds
 */
function durationToSeconds(duration: Duration): number {
  const value = parseInt(duration.slice(0, -1));
  const unit = duration.slice(-1);

  switch (unit) {
    case "h":
      return value * 3600;
    case "d":
      return value * 24 * 3600;
    default:
      return 3600; // Default to 1 hour
  }
}

/**
 * Pad chart data with null values for missing timestamps in the requested duration
 *
 * @param data - The actual chart data from the API
 * @param duration - The requested time duration (e.g., "12h", "48h")
 * @returns Chart data padded with null values for the full time range
 *
 * @example
 * // If user selects 12h but only has 4h of data:
 * // Returns 12h worth of datapoints, with first 8h as null values
 * const paddedData = padChartDataWithNulls(chartData, "12h");
 */
export function padChartDataWithNulls<T extends ChartData>(data: T[], duration: Duration): T[] {
  if (!data || data.length === 0) {
    return data;
  }

  const durationSeconds = durationToSeconds(duration);
  const now = Date.now();
  const startTime = now - durationSeconds * 1000;

  // Find the first bucket boundary at or before startTime
  const granularityMs = GRANULARITY_SECONDS * 1000;
  const firstBucket = Math.floor(startTime / granularityMs) * granularityMs;

  // Use the last actual data point as the end boundary, not current time
  // Filter out invalid datetime values and provide fallback to current time
  // Safe: data.length === 0 is handled by early return above (line 36), so Math.max never receives empty array
  const validTimestamps = data.map((d) => d.datetime).filter((dt) => typeof dt === "number" && !isNaN(dt));
  const lastDataTimestamp = validTimestamps.length > 0 ? Math.max(...validTimestamps) : now;
  const lastBucket = Math.floor(lastDataTimestamp / granularityMs) * granularityMs;

  // Generate all expected timestamps at 90-second intervals
  const expectedTimestamps: number[] = [];
  for (let bucketTime = firstBucket; bucketTime <= lastBucket; bucketTime += granularityMs) {
    expectedTimestamps.push(bucketTime);
  }

  // Create a map of existing data by timestamp
  const existingDataMap = new Map<number, T>();
  data.forEach((point) => {
    // Round to nearest granularity bucket to match expected timestamps
    const bucketTime = Math.floor(point.datetime / (GRANULARITY_SECONDS * 1000)) * (GRANULARITY_SECONDS * 1000);
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
