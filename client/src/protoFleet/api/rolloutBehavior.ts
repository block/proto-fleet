import { create } from "@bufbuild/protobuf";

import {
  RolloutAutomationThresholdsSchema,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// Forms retain inactive values so switching methods does not discard edits.
// Requests must include only the settings consumed by the selected behavior.
export function rolloutBehaviorForRequest(draft: RolloutBehavior): RolloutBehavior {
  const batched = draft.method === RolloutMethod.BATCHED;
  const pilot = draft.method === RolloutMethod.PILOT_THEN_CONTINUE;
  const review = batched && draft.reviewAfterEachBatch;
  const autoContinue = (pilot || review) && draft.autoContinueOnHealthyTelemetry;
  const thresholds = autoContinue ? draft.thresholds : undefined;
  const hasSampledLimit =
    thresholds?.maxHashrateDropPercent !== undefined ||
    thresholds?.maxEfficiencyIncreasePercent !== undefined ||
    thresholds?.maxTemperatureIncreaseCelsius !== undefined;

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
          minSampleCoveragePercent: hasSampledLimit ? thresholds.minSampleCoveragePercent : undefined,
        })
      : undefined,
    controllerTimeoutSeconds: draft.method === RolloutMethod.DELEGATED ? draft.controllerTimeoutSeconds : 0,
  });
}
