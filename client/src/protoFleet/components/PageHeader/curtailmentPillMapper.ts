import { type CurtailmentPillEvent, isCurtailmentPillState } from "./curtailmentPillTypes";
import { getFixedKwTarget, hasCurtailmentTargetMetrics } from "@/protoFleet/api/curtailmentMappers";
import type { CurtailmentEvent as ProtoCurtailmentEvent } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  getActiveCurtailmentDisplayState,
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventObservedReductionKw,
  getCurtailmentEventScopeLabel,
  getCurtailmentEventSelectedMinerCount,
  getCurtailmentTargetRollups,
  isActiveCurtailmentEventState,
  mapCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export function mapCurtailmentPillEvent(event?: ProtoCurtailmentEvent): CurtailmentPillEvent | null {
  if (!event) {
    return null;
  }

  const state = mapCurtailmentEventState(event.state);
  if (!isActiveCurtailmentEventState(state)) {
    return null;
  }

  const selectedMiners = getCurtailmentEventSelectedMinerCount(event);
  const estimatedReductionKw = getCurtailmentEventEstimatedReductionKw(event);
  const displayState = getActiveCurtailmentDisplayState(
    {
      state,
      selectedMiners,
      estimatedReductionKw,
      targetKw: getFixedKwTarget(event),
      observedReductionKw: getCurtailmentEventObservedReductionKw(event, estimatedReductionKw),
      rollups: getCurtailmentTargetRollups(event),
    },
    { dispatchStartedAsCurtailing: true },
  );

  if (!isCurtailmentPillState(displayState)) {
    return null;
  }

  return {
    reason: event.reason || "Curtailment",
    state: displayState,
    scopeLabel: getCurtailmentEventScopeLabel(event),
    selectedMiners,
    estimatedReductionKw,
    targetMetricsAvailable: hasCurtailmentTargetMetrics(event),
  };
}
