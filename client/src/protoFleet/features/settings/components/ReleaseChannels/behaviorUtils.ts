import { create } from "@bufbuild/protobuf";

import { methodHelpText, methodLabels, orderLabels } from "./rolloutStatus";
import {
  RolloutAutomationThresholdsSchema,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// Behavior a new channel starts with: a single batch, least efficient first,
// no ceiling on miners offline. Batch sizing and thresholds carry sensible
// starting points for when the operator switches to a paced method.
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
  behavior.method === RolloutMethod.PILOT_THEN_CONTINUE || behavior.reviewAfterEachBatch;

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
    default:
      return `${inScopeCount.toLocaleString()} miners in a single batch`;
  }
}
