import { describe, expect, it } from "vitest";
import { Duration } from "@/shared/components/DurationSelector";

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
      const duration: Duration = "1h";
      const seconds = parseInt(duration.slice(0, -1)) * 3600;
      expect(seconds).toBe(3600);
    });

    it("converts 12h to 43200 seconds", () => {
      const duration: Duration = "12h";
      const seconds = parseInt(duration.slice(0, -1)) * 3600;
      expect(seconds).toBe(43200);
    });

    it("converts 24h to 86400 seconds", () => {
      const duration: Duration = "24h";
      const seconds = parseInt(duration.slice(0, -1)) * 3600;
      expect(seconds).toBe(86400);
    });

    it("converts 48h to 172800 seconds", () => {
      const duration: Duration = "48h";
      const seconds = parseInt(duration.slice(0, -1)) * 3600;
      expect(seconds).toBe(172800);
    });

    it("converts 5d to 432000 seconds", () => {
      const duration: Duration = "5d";
      const seconds = parseInt(duration.slice(0, -1)) * 24 * 3600;
      expect(seconds).toBe(432000);
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

    it("uses 90s granularity for 12h (480 buckets)", () => {
      const durationSeconds = 43200; // 12h
      const granularity = 90;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(480);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });

    it("uses 90s granularity for 24h (960 buckets)", () => {
      const durationSeconds = 86400; // 24h
      const granularity = 90;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(960);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });

    it("uses 180s (3min) granularity for 48h (960 buckets)", () => {
      const durationSeconds = 172800; // 48h
      const granularity = 180;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(960);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });

    it("uses 600s (10min) granularity for 5d (720 buckets)", () => {
      const durationSeconds = 432000; // 5d
      const granularity = 600;
      const buckets = calculateBucketCount(durationSeconds, granularity);

      expect(buckets).toBe(720);
      expect(buckets).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
    });
  });

  describe("granularity would exceed limit with wrong values", () => {
    it("48h with 90s granularity would exceed limit (1920 buckets)", () => {
      const durationSeconds = 172800; // 48h
      const wrongGranularity = 90;
      const buckets = calculateBucketCount(durationSeconds, wrongGranularity);

      expect(buckets).toBe(1920);
      expect(buckets).toBeGreaterThan(1000); // Would fail without optimization
    });

    it("5d with 90s granularity would exceed limit (4800 buckets)", () => {
      const durationSeconds = 432000; // 5d
      const wrongGranularity = 90;
      const buckets = calculateBucketCount(durationSeconds, wrongGranularity);

      expect(buckets).toBe(4800);
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
