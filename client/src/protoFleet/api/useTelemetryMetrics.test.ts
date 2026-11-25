// @vitest-environment node
import { describe, expect, it } from "vitest";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { type FleetDuration } from "@/shared/components/DurationSelector";

const BACKEND_BUCKET_LIMIT = 1000;

describe("useTelemetryMetrics granularity calculations", () => {
  it.each<[FleetDuration, number, number]>([
    ["1h", 3600, 90],
    ["24h", 86_400, 90],
    ["7d", 604_800, 900],
    ["30d", 2_592_000, 2700],
    ["90d", 7_776_000, 8100],
    ["1y", 31_536_000, 32_850],
  ])("uses a granularity for %s that stays within the backend bucket limit", (duration, durationSeconds, expected) => {
    const granularity = getGranularityForDuration(duration);

    expect(granularity).toBe(expected);
    expect(durationSeconds / granularity).toBeLessThanOrEqual(BACKEND_BUCKET_LIMIT);
  });

  it("keeps 7d granularity aligned with hourly aggregates", () => {
    const granularity = getGranularityForDuration("7d");

    expect(granularity).toBe(900);
    expect(3600 % granularity).toBe(0);
  });
});
