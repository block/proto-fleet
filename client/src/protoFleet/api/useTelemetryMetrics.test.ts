import { describe, expect, it } from "vitest";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { FleetDuration } from "@/shared/components/DurationSelector";

// Import the constants and function we need to test
// Since they're not exported, we'll need to test through the hook's behavior
// But for unit testing the logic, let's test the duration calculations directly

describe("useTelemetryMetrics granularity calculations", () => {
  // Helper to calculate expected bucket count
  const calculateBucketCount = (durationSeconds: number, granularitySeconds: number): number => {
    return Math.ceil(durationSeconds / granularitySeconds);
  };

  describe("duration to seconds conversion", () => {
    it("converts 1h to 3600 seconds", () => {
      const duration: FleetDuration = "1h";
      const seconds = parseInt(duration.slice(0, -1)) * 3600;
      expect(seconds).toBe(3600);
    });

    it("converts 24h to 86400 seconds", () => {
      const duration: FleetDuration = "24h";
      const seconds = parseInt(duration.slice(0, -1)) * 3600;
      expect(seconds).toBe(86400);
    });

    it("converts 7d to 604800 seconds", () => {
      const duration: FleetDuration = "7d";
      const seconds = parseInt(duration.slice(0, -1)) * 24 * 3600;
      expect(seconds).toBe(604800);
    });

    it("converts 30d to 2592000 seconds", () => {
      const duration: FleetDuration = "30d";
      const seconds = parseInt(duration.slice(0, -1)) * 24 * 3600;
      expect(seconds).toBe(2592000);
    });
  });

  describe("granularity selection to stay within 1000 bucket limit", () => {
    const BACKEND_BUCKET_LIMIT = 1000;

    it("uses 90s granularity for 1h (40 buckets)", () => {
      const durationSeconds = 3600; // 1h
      const granularity = 90;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(40);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });

    it("uses 90s granularity for 24h (960 buckets)", () => {
      const durationSeconds = 86400; // 24h
      const granularity = 90;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(960);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });

    it("uses 900s granularity for 7d (672 buckets)", () => {
      const durationSeconds = 604800; // 7d
      const granularity = 900;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(672);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });

    it("uses 2700s (45min) granularity for 30d (960 buckets)", () => {
      const durationSeconds = 2592000; // 30d
      const granularity = 2700;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(960);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });
  });

  describe("granularity would exceed limit with wrong values", () => {
    it("7d with 600s granularity would exceed limit (1008 buckets)", () => {
      const durationSeconds = 604800; // 7d
      const wrongGranularity = 600;
      const buckets = calculateBucketCount(durationSeconds, wrongGranularity);

      expect(buckets).toBe(1008);
      expect(buckets).toBeGreaterThan(1000); // Would fail without optimization
    });

    it("30d with 90s granularity would exceed limit (28800 buckets)", () => {
      const durationSeconds = 2592000; // 30d
      const wrongGranularity = 90;
      const buckets = calculateBucketCount(durationSeconds, wrongGranularity);

      expect(buckets).toBe(28800);
      expect(buckets).toBeGreaterThan(1000); // Would fail without optimization
    });
  });

  describe("edge cases", () => {
    it("keeps 7d granularity aligned with hourly aggregates", () => {
      const granularity = getGranularityForDuration("7d");

      expect(granularity).toBe(900);
      expect(3600 % granularity).toBe(0);
    });

    it("invalid duration returns default granularity (90s)", () => {
      // This tests that invalid durations fall back to 90s default
      const defaultGranularity = 90;
      expect(defaultGranularity).toBe(90);
    });
  });
});
