import { describe, expect, it } from "vitest";
import { padChartDataWithNulls } from "./chartDataPadding";
import type { ChartData } from "@/shared/components/LineChart/types";

describe("padChartDataWithNulls", () => {
  it("should return empty array when input is empty", () => {
    const result = padChartDataWithNulls([], "1h");
    expect(result).toEqual([]);
  });

  it("should pad data with null values for missing timestamps", () => {
    const now = Date.now();
    const thirtyMinutesAgo = now - 1800 * 1000;

    // Simulate having only 2 data points in the last hour
    const data: ChartData[] = [
      { datetime: thirtyMinutesAgo, hashrate: 100 },
      { datetime: now, hashrate: 150 },
    ];

    const result = padChartDataWithNulls(data, "1h");

    // Should have many more points (1 hour / 90 seconds = 40 buckets)
    expect(result.length).toBeGreaterThan(2);
    expect(result.length).toBeLessThanOrEqual(41); // 3600 / 90 = 40, plus potential rounding

    // First points should be null (no data in the first part of the hour)
    const firstNullPoint = result.find((point) => point.datetime < thirtyMinutesAgo);
    expect(firstNullPoint).toBeDefined();
    expect(firstNullPoint?.hashrate).toBeNull();

    // Original data points should be preserved
    const preservedPoints = result.filter((point) => point.hashrate !== null);
    expect(preservedPoints.length).toBeGreaterThanOrEqual(2);
  });

  it("should preserve all numeric fields from original data", () => {
    const now = Date.now();

    interface MultiMetricData extends ChartData {
      hashrate: number | null;
      power: number | null;
      efficiency: number | null;
    }

    const data: MultiMetricData[] = [{ datetime: now, hashrate: 100, power: 2000, efficiency: 50 }];

    const result = padChartDataWithNulls(data, "1h");

    // Check that a null point has all the same fields
    const nullPoint = result.find((point) => point.hashrate === null);
    expect(nullPoint).toBeDefined();
    expect(nullPoint).toHaveProperty("hashrate");
    expect(nullPoint).toHaveProperty("power");
    expect(nullPoint).toHaveProperty("efficiency");
    expect(nullPoint?.hashrate).toBeNull();
    expect(nullPoint?.power).toBeNull();
    expect(nullPoint?.efficiency).toBeNull();
  });

  it("should handle different duration strings correctly with dynamic granularity", () => {
    const now = Date.now();

    const data: ChartData[] = [{ datetime: now, hashrate: 100 }];

    // 1 hour should have ~40 buckets (3600s / 90s granularity)
    const result1h = padChartDataWithNulls(data, "1h");
    expect(result1h.length).toBeGreaterThan(30);
    expect(result1h.length).toBeLessThanOrEqual(50);

    // 7 days use 900s granularity: 672 buckets (604800s / 900s)
    const result7d = padChartDataWithNulls(data, "7d");
    expect(result7d.length).toBeGreaterThan(650);
    expect(result7d.length).toBeLessThanOrEqual(1000);

    // 24 hours uses 90s granularity: ~960 buckets
    const result24h = padChartDataWithNulls(data, "24h");
    expect(result24h.length).toBeGreaterThan(900);
    expect(result24h.length).toBeLessThanOrEqual(1000);
  });

  it("should use 90-second granularity for short durations (1h)", () => {
    const now = Date.now();
    const granularity = 90 * 1000; // 90 seconds in milliseconds for 1h duration

    const data: ChartData[] = [{ datetime: now, hashrate: 100 }];

    const result = padChartDataWithNulls(data, "1h");

    // Check that timestamps are at 90-second intervals for 1h duration
    for (let i = 1; i < result.length; i++) {
      const timeDiff = result[i].datetime - result[i - 1].datetime;
      expect(timeDiff).toBe(granularity);
    }
  });

  it("should match existing data to correct buckets", () => {
    const now = Date.now();
    const granularity = 90 * 1000;

    // Create a timestamp that's already on a 90-second boundary
    const bucketTime = Math.floor(now / granularity) * granularity;

    const data: ChartData[] = [{ datetime: bucketTime, hashrate: 100 }];

    const result = padChartDataWithNulls(data, "1h");

    // The data point should be preserved (not replaced with null)
    const matchingPoint = result.find((point) => {
      const pointBucket = Math.floor(point.datetime / granularity) * granularity;
      return pointBucket === bucketTime;
    });

    expect(matchingPoint).toBeDefined();
    expect(matchingPoint?.hashrate).toBe(100);
  });

  it("should not pad beyond the last actual data point timestamp", () => {
    const now = Date.now();
    const fiveMinutesAgo = now - 5 * 60 * 1000;
    const tenMinutesAgo = now - 10 * 60 * 1000;

    // Create data that stops 5 minutes ago (not at current time)
    const data: ChartData[] = [
      { datetime: tenMinutesAgo, hashrate: 100 },
      { datetime: fiveMinutesAgo, hashrate: 150 },
    ];

    const result = padChartDataWithNulls(data, "1h");

    // Get the last timestamp in the result
    const lastTimestamp = result[result.length - 1].datetime;

    // The last timestamp should be close to fiveMinutesAgo (within one bucket)
    // and should NOT extend to current time
    const granularity = 90 * 1000;
    const expectedLastBucket = Math.floor(fiveMinutesAgo / granularity) * granularity;
    expect(lastTimestamp).toBe(expectedLastBucket);

    // Verify no timestamps are close to current time
    const timeSinceLastPoint = now - lastTimestamp;
    expect(timeSinceLastPoint).toBeGreaterThan(4 * 60 * 1000); // At least 4 minutes ago

    // Verify we don't have null datapoints at the end (after the last actual data)
    const lastActualDataBucket = Math.floor(fiveMinutesAgo / granularity) * granularity;
    const pointsAfterLastData = result.filter((point) => point.datetime > lastActualDataBucket);
    expect(pointsAfterLastData.length).toBe(0);
  });
});
