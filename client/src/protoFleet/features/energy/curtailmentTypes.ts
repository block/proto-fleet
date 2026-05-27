import type { CurtailmentEventState } from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export const curtailmentHistoryPageSize = 50;

export type CurtailmentPriority = "normal" | "high" | "emergency";

export type CurtailmentTargetState =
  | "pending"
  | "dispatched"
  | "confirmed"
  | "drifted"
  | "resolved"
  | "released"
  | "restoreFailed";

export interface CurtailmentTargetRollup {
  state: CurtailmentTargetState;
  count: number;
}

export interface ActiveCurtailmentEvent {
  reason: string;
  state: CurtailmentEventState;
  scopeLabel: string;
  endedAt?: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  observedReductionKw: number;
  remainingPowerKw?: number;
  restoreBatchSize: number;
  restoreBatchIntervalSec: number;
  rollups: CurtailmentTargetRollup[];
}

export interface CurtailmentHistoryEvent {
  id: string;
  reason: string;
  state: CurtailmentEventState;
  priority: CurtailmentPriority;
  scopeLabel: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  sourceLabel: string;
  startedAt?: string;
  endedAt?: string;
  scheduledAt?: string;
  createdAt?: string;
}
