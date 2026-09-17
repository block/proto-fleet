import { describe, expect, it } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";

import { RolloutBehaviorSchema, RolloutMethod, RolloutOrder } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import { gatesAfterBatch } from "@/protoFleet/features/settings/components/ReleaseChannels/behaviorUtils";

describe("rolloutBehaviorForRequest", () => {
  it("keeps pilot automation but drops hidden batch, wait and controller settings", () => {
    const draft = create(RolloutBehaviorSchema, {
      method: RolloutMethod.PILOT_THEN_CONTINUE,
      order: RolloutOrder.RANDOM,
      batchSize: 10,
      pilotSize: 2,
      reviewAfterEachBatch: true,
      waitBetweenBatchesSeconds: 60,
      autoContinueOnHealthyTelemetry: true,
      stabilizationSeconds: 600,
      thresholds: { maxHashrateDropPercent: 0, maxNewErrors: 0, minSampleCoveragePercent: 75 },
      maxConcurrentOffline: 3,
      controllerTimeoutSeconds: 30,
    });

    expect(toJson(RolloutBehaviorSchema, rolloutBehaviorForRequest(draft))).toEqual({
      method: "ROLLOUT_METHOD_PILOT_THEN_CONTINUE",
      order: "ROLLOUT_ORDER_RANDOM",
      pilotSize: 2,
      autoContinueOnHealthyTelemetry: true,
      stabilizationSeconds: 600,
      thresholds: { maxHashrateDropPercent: 0, maxNewErrors: 0, minSampleCoveragePercent: 75 },
      maxConcurrentOffline: 3,
    });
  });

  it("removes sample coverage when the last sampled limit is cleared without losing an error limit of zero", () => {
    const draft = create(RolloutBehaviorSchema, {
      method: RolloutMethod.BATCHED,
      batchSize: 5,
      reviewAfterEachBatch: true,
      autoContinueOnHealthyTelemetry: true,
      thresholds: { maxNewErrors: 0, minSampleCoveragePercent: 100 },
    });

    expect(toJson(RolloutBehaviorSchema, rolloutBehaviorForRequest(draft))).toEqual({
      method: "ROLLOUT_METHOD_BATCHED",
      batchSize: 5,
      reviewAfterEachBatch: true,
      autoContinueOnHealthyTelemetry: true,
      thresholds: { maxNewErrors: 0 },
    });
    expect(draft.thresholds?.minSampleCoveragePercent).toBe(100);
  });

  it.each(["maxHashrateDropPercent", "maxEfficiencyIncreasePercent", "maxTemperatureIncreaseCelsius"] as const)(
    "preserves coverage when %s is explicitly zero",
    (metric) => {
      const draft = create(RolloutBehaviorSchema, {
        method: RolloutMethod.PILOT_THEN_CONTINUE,
        pilotSize: 1,
        autoContinueOnHealthyTelemetry: true,
        thresholds: { [metric]: 0, minSampleCoveragePercent: 50 },
      });

      expect(toJson(RolloutBehaviorSchema, rolloutBehaviorForRequest(draft))).toMatchObject({
        thresholds: { [metric]: 0, minSampleCoveragePercent: 50 },
      });
    },
  );

  it("keeps only delegated settings when leaving a paced method", () => {
    const draft = create(RolloutBehaviorSchema, {
      method: RolloutMethod.DELEGATED,
      order: RolloutOrder.RANDOM,
      maxConcurrentOffline: 2,
      controllerTimeoutSeconds: 90,
      batchSize: 10,
      pilotSize: 1,
      reviewAfterEachBatch: true,
      waitBetweenBatchesSeconds: 60,
      autoContinueOnHealthyTelemetry: true,
      stabilizationSeconds: 600,
      thresholds: { maxNewErrors: 0 },
    });

    expect(toJson(RolloutBehaviorSchema, rolloutBehaviorForRequest(draft))).toEqual({
      method: "ROLLOUT_METHOD_DELEGATED",
      order: "ROLLOUT_ORDER_RANDOM",
      maxConcurrentOffline: 2,
      controllerTimeoutSeconds: 90,
    });
  });

  it("leaves invalid active settings for the server to reject", () => {
    const draft = create(RolloutBehaviorSchema, {
      method: RolloutMethod.BATCHED,
      batchSize: 0,
      waitBetweenBatchesSeconds: -1,
      maxConcurrentOffline: -1,
    });

    expect(rolloutBehaviorForRequest(draft)).toMatchObject({
      batchSize: 0,
      waitBetweenBatchesSeconds: -1,
      maxConcurrentOffline: -1,
    });
  });
});

describe("review gate controls", () => {
  it.each([RolloutMethod.ALL_AT_ONCE, RolloutMethod.UNSPECIFIED, RolloutMethod.DELEGATED])(
    "hides automation for method %s even when a prior method retained review settings",
    (method) => {
      expect(gatesAfterBatch(create(RolloutBehaviorSchema, { method, reviewAfterEachBatch: true }))).toBe(false);
    },
  );
});
