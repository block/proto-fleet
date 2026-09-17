import { create } from "@bufbuild/protobuf";

import { methodHelpText, methodLabels, orderLabels } from "./rolloutStatus";
import {
  type RolloutAutomationThresholds,
  RolloutAutomationThresholdsSchema,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// Behavior a new channel starts with: a single batch, least efficient first,
// no ceiling on miners offline. Batch sizing and thresholds carry sensible
// starting points for when the operator switches to a paced method. The API
// request builder removes inactive settings before sending this form draft.
export const defaultBehavior = (): RolloutBehavior =>
  create(RolloutBehaviorSchema, {
    method: RolloutMethod.ALL_AT_ONCE,
    order: RolloutOrder.LEAST_EFFICIENT_FIRST,
    batchSize: 10,
    pilotSize: 1,
    stabilizationSeconds: 600,
    thresholds: create(RolloutAutomationThresholdsSchema, { maxHashrateDropPercent: 10, maxNewErrors: 0 }),
  });

export const methodOptions = [RolloutMethod.ALL_AT_ONCE, RolloutMethod.BATCHED, RolloutMethod.PILOT_THEN_CONTINUE].map(
  (method) => ({ value: String(method), label: methodLabels[method], description: methodHelpText[method] }),
);

export const orderOptions = [RolloutOrder.LEAST_EFFICIENT_FIRST, RolloutOrder.RANDOM].map((order) => ({
  value: String(order),
  label: orderLabels[order],
}));

export const isPacedMethod = (method: RolloutMethod): boolean =>
  method === RolloutMethod.BATCHED || method === RolloutMethod.PILOT_THEN_CONTINUE;

// Whether a finished batch holds for review (and so whether auto-continue
// and its thresholds apply).
export const gatesAfterBatch = (behavior: RolloutBehavior): boolean =>
  behavior.method === RolloutMethod.PILOT_THEN_CONTINUE ||
  (behavior.method === RolloutMethod.BATCHED && behavior.reviewAfterEachBatch);

export const hasSampledLimit = (thresholds: RolloutAutomationThresholds | undefined): boolean =>
  thresholds?.maxHashrateDropPercent !== undefined ||
  thresholds?.maxEfficiencyIncreasePercent !== undefined ||
  thresholds?.maxTemperatureIncreaseCelsius !== undefined;

export type RolloutNumericField =
  | "batchSize"
  | "pilotSize"
  | "waitBetweenBatchesSeconds"
  | "stabilizationSeconds"
  | "maxConcurrentOffline"
  | "maxHashrateDropPercent"
  | "maxEfficiencyIncreasePercent"
  | "maxTemperatureIncreaseCelsius"
  | "minSampleCoveragePercent"
  | "maxNewErrors";

const MAX_INT32 = 2_147_483_647;
const integerError = (value: number, minimum = 0): string | undefined => {
  if (minimum === 1 && value < 1) return "Enter at least 1 miner.";
  return Number.isInteger(value) && value >= minimum && value <= MAX_INT32
    ? undefined
    : `Enter a whole number from ${minimum} to 2,147,483,647.`;
};

// Validate only fields the selected method will send. Hidden draft values stay
// available for correction if the operator switches back to that method.
export function rolloutBehaviorErrors(behavior: RolloutBehavior): Partial<Record<RolloutNumericField, string>> {
  const errors: Partial<Record<RolloutNumericField, string>> = {};
  const checkInteger = (field: RolloutNumericField, value: number, minimum = 0) => {
    const error = integerError(value, minimum);
    if (error) errors[field] = error;
  };
  const checkDuration = (field: "waitBetweenBatchesSeconds" | "stabilizationSeconds", value: number) => {
    if (integerError(value)) {
      errors[field] = "Enter a duration in whole seconds from 0 to 2,147,483,647 seconds.";
    }
  };
  checkInteger("maxConcurrentOffline", behavior.maxConcurrentOffline);
  if (behavior.method === RolloutMethod.BATCHED) {
    checkInteger("batchSize", behavior.batchSize, 1);
    if (!behavior.reviewAfterEachBatch) checkDuration("waitBetweenBatchesSeconds", behavior.waitBetweenBatchesSeconds);
  }
  if (behavior.method === RolloutMethod.PILOT_THEN_CONTINUE) checkInteger("pilotSize", behavior.pilotSize, 1);
  if (gatesAfterBatch(behavior) && behavior.autoContinueOnHealthyTelemetry) {
    checkDuration("stabilizationSeconds", behavior.stabilizationSeconds);
    const thresholds = behavior.thresholds;
    for (const field of [
      "maxHashrateDropPercent",
      "maxEfficiencyIncreasePercent",
      "maxTemperatureIncreaseCelsius",
    ] as const) {
      const value = thresholds?.[field];
      if (value === undefined) continue;
      if (!Number.isFinite(value) || value < 0 || (field === "maxHashrateDropPercent" && value > 100)) {
        errors[field] =
          field === "maxHashrateDropPercent" ? "Enter a number from 0 to 100." : "Enter a finite number of 0 or more.";
      }
    }
    if (thresholds?.maxNewErrors !== undefined) checkInteger("maxNewErrors", thresholds.maxNewErrors);
    if (hasSampledLimit(thresholds) && thresholds?.minSampleCoveragePercent !== undefined) {
      const coverage = thresholds.minSampleCoveragePercent;
      if (!Number.isFinite(coverage) || coverage <= 0 || coverage > 100) {
        errors.minSampleCoveragePercent = "Enter a number greater than 0 and at most 100.";
      }
    }
  }
  return errors;
}

// Live plan readout: "~3 batches of 10" for the miners currently in scope.
export function planReadout(behavior: RolloutBehavior, inScopeCount: number): string | null {
  if (inScopeCount <= 0) return null;
  switch (behavior.method) {
    case RolloutMethod.BATCHED: {
      if (behavior.batchSize <= 0) return null;
      const batches = Math.ceil(inScopeCount / behavior.batchSize);
      return `~${batches.toLocaleString()} ${batches === 1 ? "batch" : "batches"} of ${behavior.batchSize.toLocaleString()} across ${inScopeCount.toLocaleString()} miners`;
    }
    case RolloutMethod.PILOT_THEN_CONTINUE: {
      const pilot = Math.min(behavior.pilotSize, inScopeCount);
      return `Pilot batch of ${pilot.toLocaleString()}, then ${Math.max(inScopeCount - pilot, 0).toLocaleString()} remaining`;
    }
    case RolloutMethod.DELEGATED:
      return `${inScopeCount.toLocaleString()} ${inScopeCount === 1 ? "miner" : "miners"} in scope; an external controller decides which miners update and when`;
    default:
      return `${inScopeCount.toLocaleString()} miners in a single batch`;
  }
}
