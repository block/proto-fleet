import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";

export const curtailingCurtailmentEvent: ActiveCurtailmentEvent = {
  reason: "ERCOT ERS obligation",
  state: "active",
  scopeLabel: "Rockdale, TX",
  sourceLabel: "Manual",
  isAutomationOwned: false,
  selectedMiners: 18,
  estimatedReductionKw: 60.2,
  targetKw: 60,
  observedReductionKw: 59.4,
  remainingPowerKw: 132.5,
  restoreBatchSize: 10,
  restoreBatchIntervalSec: 120,
  rollups: [
    { state: "confirmed", count: 16 },
    { state: "dispatched", count: 1 },
    { state: "drifted", count: 1 },
    { state: "resolved", count: 0 },
    { state: "restoreFailed", count: 0 },
  ],
};

export const curtailedCurtailmentEvent: ActiveCurtailmentEvent = {
  ...curtailingCurtailmentEvent,
  observedReductionKw: curtailingCurtailmentEvent.targetKw ?? curtailingCurtailmentEvent.estimatedReductionKw,
  remainingPowerKw: 131.9,
  rollups: [{ state: "confirmed", count: curtailingCurtailmentEvent.selectedMiners }],
};

export const restoringCurtailmentEvent: ActiveCurtailmentEvent = {
  ...curtailingCurtailmentEvent,
  state: "restoring",
  rollups: [
    { state: "resolved", count: 8 },
    { state: "confirmed", count: 9 },
    { state: "restoreFailed", count: 1 },
  ],
};

export const restoredCurtailmentEvent: ActiveCurtailmentEvent = {
  ...curtailingCurtailmentEvent,
  state: "completed",
  observedReductionKw: 0,
  remainingPowerKw: 191.9,
  endedAt: "2026-04-30T14:08:00-04:00",
  rollups: [{ state: "resolved", count: curtailingCurtailmentEvent.selectedMiners }],
};

export const restoreIncompleteCurtailmentEvent: ActiveCurtailmentEvent = {
  ...restoredCurtailmentEvent,
  state: "completedWithFailures",
  rollups: [
    { state: "resolved", count: 17 },
    { state: "restoreFailed", count: 1 },
  ],
};
