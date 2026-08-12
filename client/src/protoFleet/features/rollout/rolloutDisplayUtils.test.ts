import { describe, expect, it } from "vitest";

import { rolloutErrorImpactCount, rolloutMetricDeltaIntent } from "./rolloutDisplayUtils";

describe("rolloutMetricDeltaIntent", () => {
  it("treats lower hashrate as a positive curtailment dispatch outcome", () => {
    expect(rolloutMetricDeltaIntent("hashrate", -1, "curtailment", "dispatch")).toBe("positive");
  });

  it("treats lower hashrate as a negative restoration outcome", () => {
    expect(rolloutMetricDeltaIntent("hashrate", -1, "curtailment", "restore")).toBe("negative");
  });

  it("treats higher temperature as negative across rollout processes", () => {
    expect(rolloutMetricDeltaIntent("temperature", 1, "firmware")).toBe("negative");
    expect(rolloutMetricDeltaIntent("temperature", 1, "curtailment", "restore")).toBe("negative");
  });
});

describe("rolloutErrorImpactCount", () => {
  it("counts each impacted miner once across multiple error strings", () => {
    expect(
      rolloutErrorImpactCount([
        { id: "one", message: "First error", impactedMiners: ["miner-1", "miner-2"] },
        { id: "two", message: "Second error", impactedMiners: ["miner-1"] },
      ]),
    ).toBe(2);
  });
});
