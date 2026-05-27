import {
  type ActiveCurtailmentEventState,
  activeCurtailmentEventStates,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export const curtailmentPillStates = activeCurtailmentEventStates;

export type CurtailmentPillState = ActiveCurtailmentEventState;

export interface CurtailmentPillEvent {
  reason: string;
  state: CurtailmentPillState;
  scopeLabel: string;
  selectedMiners: number;
  estimatedReductionKw: number;
}

export interface CurtailmentPillProps {
  event: CurtailmentPillEvent;
  detailsPath?: string;
}
