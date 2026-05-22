export const curtailmentPillStates = ["pending", "active", "restoring"] as const;

export type CurtailmentPillState = (typeof curtailmentPillStates)[number];

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
