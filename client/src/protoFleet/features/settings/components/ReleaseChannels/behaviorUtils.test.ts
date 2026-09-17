import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { planReadout, rolloutBehaviorErrors } from "./behaviorUtils";
import { RolloutBehaviorSchema, RolloutMethod } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

describe("delegated rollout plan readout", () => {
  const delegated = create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED });

  it.each([
    { count: 1, scope: "1 miner in scope" },
    { count: 48, scope: "48 miners in scope" },
  ])("describes external selection with $scope", ({ count, scope }) => {
    expect(planReadout(delegated, count)).toBe(`${scope}; an external controller decides which miners update and when`);
  });

  it("does not present a plan for an empty channel", () => {
    expect(planReadout(delegated, 0)).toBeNull();
  });
});

describe("sample coverage validation", () => {
  it.each(["maxHashrateDropPercent", "maxEfficiencyIncreasePercent", "maxTemperatureIncreaseCelsius"] as const)(
    "accepts the default and fractional/full coverage with an explicit zero %s limit",
    (metric) => {
      for (const coverage of [undefined, 0.5, 100]) {
        const behavior = create(RolloutBehaviorSchema, {
          method: RolloutMethod.PILOT_THEN_CONTINUE,
          pilotSize: 1,
          autoContinueOnHealthyTelemetry: true,
          thresholds: { [metric]: 0, minSampleCoveragePercent: coverage },
        });
        expect(rolloutBehaviorErrors(behavior)).toEqual({});
      }
    },
  );

  it.each([
    { method: RolloutMethod.ALL_AT_ONCE },
    { method: RolloutMethod.DELEGATED },
    { method: RolloutMethod.BATCHED, batchSize: 1 },
    { method: RolloutMethod.PILOT_THEN_CONTINUE, pilotSize: 1, autoContinueOnHealthyTelemetry: false },
  ])("does not block inactive coverage in $method behavior", (patch) => {
    const behavior = create(RolloutBehaviorSchema, {
      autoContinueOnHealthyTelemetry: true,
      thresholds: { maxHashrateDropPercent: 10, minSampleCoveragePercent: Number.NaN },
      ...patch,
    });
    expect(rolloutBehaviorErrors(behavior)).toEqual({});
  });
});
