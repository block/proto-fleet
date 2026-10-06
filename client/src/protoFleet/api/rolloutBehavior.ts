import { create } from "@bufbuild/protobuf";

import {
  type RolloutAutomationThresholds,
  RolloutAutomationThresholdsSchema,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

export const isPacedMethod = (method: RolloutMethod): boolean =>
  method === RolloutMethod.BATCHED || method === RolloutMethod.PILOT_THEN_CONTINUE;

export const gatesAfterBatch = (behavior: RolloutBehavior): boolean =>
  behavior.method === RolloutMethod.PILOT_THEN_CONTINUE ||
  (behavior.method === RolloutMethod.BATCHED && behavior.reviewAfterEachBatch);

export const hasSampledLimit = (thresholds: RolloutAutomationThresholds | undefined): boolean =>
  thresholds?.maxHashrateDropPercent !== undefined ||
  thresholds?.maxEfficiencyIncreasePercent !== undefined ||
  thresholds?.maxTemperatureIncreaseCelsius !== undefined;

// Forms retain inactive values so switching methods does not discard edits.
// Requests must include only the settings consumed by the selected behavior.
export function rolloutBehaviorForRequest(draft: RolloutBehavior): RolloutBehavior {
  const batched = draft.method === RolloutMethod.BATCHED;
  const pilot = draft.method === RolloutMethod.PILOT_THEN_CONTINUE;
  const review = batched && draft.reviewAfterEachBatch;
  const autoContinue = gatesAfterBatch(draft) && draft.autoContinueOnHealthyTelemetry;
  const thresholds = autoContinue ? draft.thresholds : undefined;

  return create(RolloutBehaviorSchema, {
    method: draft.method,
    order: draft.order,
    maxConcurrentOffline: draft.maxConcurrentOffline,
    batchSize: batched ? draft.batchSize : 0,
    pilotSize: pilot ? draft.pilotSize : 0,
    reviewAfterEachBatch: review,
    waitBetweenBatchesSeconds: batched && !review ? draft.waitBetweenBatchesSeconds : 0,
    autoContinueOnHealthyTelemetry: autoContinue,
    stabilizationSeconds: autoContinue ? draft.stabilizationSeconds : 0,
    thresholds: thresholds
      ? create(RolloutAutomationThresholdsSchema, {
          ...thresholds,
          minSampleCoveragePercent: hasSampledLimit(thresholds) ? thresholds.minSampleCoveragePercent : undefined,
        })
      : undefined,
    controllerTimeoutSeconds: draft.method === RolloutMethod.DELEGATED ? draft.controllerTimeoutSeconds : 0,
  });
}
