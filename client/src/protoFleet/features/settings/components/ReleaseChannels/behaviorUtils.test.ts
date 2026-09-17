import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { planReadout, rebaseBehavior, rolloutBehaviorErrors } from "./behaviorUtils";
import {
  RolloutAutomationThresholdsSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

describe("behavior draft rebasing", () => {
  it("updates untouched fields without mutating the draft or losing inactive invalid edits", () => {
    const previous = create(RolloutBehaviorSchema, {
      method: RolloutMethod.BATCHED,
      batchSize: 10,
      pilotSize: 1,
      thresholds: { maxHashrateDropPercent: 10, maxNewErrors: 0 },
    });
    const draft = create(RolloutBehaviorSchema, {
      ...previous,
      method: RolloutMethod.ALL_AT_ONCE,
      batchSize: Number.NaN,
      thresholds: { ...previous.thresholds!, maxHashrateDropPercent: Number.NaN },
    });
    const incoming = create(RolloutBehaviorSchema, {
      ...previous,
      batchSize: 20,
      pilotSize: 3,
      maxConcurrentOffline: 7,
      thresholds: create(RolloutAutomationThresholdsSchema, {
        maxHashrateDropPercent: 5,
        maxNewErrors: 2,
        minSampleCoveragePercent: 75,
      }),
    });
    Object.freeze(draft);
    Object.freeze(draft.thresholds);

    const merged = rebaseBehavior(draft, previous, incoming);

    expect(merged).not.toBe(draft);
    expect(merged.thresholds).not.toBe(draft.thresholds);
    expect(merged).toMatchObject({
      method: RolloutMethod.ALL_AT_ONCE,
      batchSize: Number.NaN,
      pilotSize: 3,
      maxConcurrentOffline: 7,
      thresholds: { maxHashrateDropPercent: Number.NaN, maxNewErrors: 2, minSampleCoveragePercent: 75 },
    });
    expect(draft).toMatchObject({ pilotSize: 1, maxConcurrentOffline: 0, thresholds: { maxNewErrors: 0 } });
  });
});

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
