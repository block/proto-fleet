import type { CurtailmentEvent as ProtoCurtailmentEvent } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { CurtailmentPillEvent } from "@/protoFleet/components/PageHeader/curtailmentPillTypes";
import {
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventScopeLabel,
  getCurtailmentEventSelectedMinerCount,
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

  return {
    reason: event.reason,
    state,
    scopeLabel: getCurtailmentEventScopeLabel(event),
    selectedMiners: getCurtailmentEventSelectedMinerCount(event),
    estimatedReductionKw: getCurtailmentEventEstimatedReductionKw(event),
  };
}
