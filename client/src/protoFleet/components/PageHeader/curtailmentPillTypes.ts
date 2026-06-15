import type { ActiveCurtailmentDisplayState } from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export const curtailmentPillStates = [
  "pending",
  "curtailing",
  "curtailed",
  "restoring",
] as const satisfies readonly ActiveCurtailmentDisplayState[];

export type CurtailmentPillState = (typeof curtailmentPillStates)[number];

const curtailmentPillStateSet = new Set<ActiveCurtailmentDisplayState>(curtailmentPillStates);

export function isCurtailmentPillState(state: ActiveCurtailmentDisplayState): state is CurtailmentPillState {
  return curtailmentPillStateSet.has(state);
}

export interface CurtailmentPillEvent {
  reason: string;
  state: CurtailmentPillState;
  scopeLabel: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetMetricsAvailable: boolean;
}

export interface CurtailmentPillProps {
  event: CurtailmentPillEvent;
  detailsPath?: string;
}
