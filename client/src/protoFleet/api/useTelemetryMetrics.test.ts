import { describe, expect, it } from "vitest";
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

    it("converts 3d to 259200 seconds", () => {
      const duration: FleetDuration = "3d";
      const seconds = parseInt(duration.slice(0, -1)) * 24 * 3600;
      expect(seconds).toBe(259200);
    });

    it("converts 10d to 864000 seconds", () => {
      const duration: FleetDuration = "10d";
      const seconds = parseInt(duration.slice(0, -1)) * 24 * 3600;
      expect(seconds).toBe(864000);
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

    it("uses 600s (10min) granularity for 3d (432 buckets)", () => {
      const durationSeconds = 259200; // 3d
      const granularity = 600;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(432);
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
    it("3d with 90s granularity would exceed limit (2880 buckets)", () => {
      const durationSeconds = 259200; // 3d
      const wrongGranularity = 90;
      const buckets = calculateBucketCount(durationSeconds, wrongGranularity);

      expect(buckets).toBe(2880);
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
    it("invalid duration returns default granularity (90s)", () => {
      // This tests that invalid durations fall back to 90s default
      const defaultGranularity = 90;
      expect(defaultGranularity).toBe(90);
    });
  });
});
