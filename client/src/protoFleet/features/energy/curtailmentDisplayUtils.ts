export const curtailmentEventStates = [
  "pending",
  "active",
  "restoring",
  "completed",
  "completedWithFailures",
  "cancelled",
  "failed",
] as const;

export type CurtailmentEventState = (typeof curtailmentEventStates)[number];

export const curtailmentEventStateLabels: Record<CurtailmentEventState, string> = {
  pending: "Pending",
  active: "Active",
  restoring: "Restoring",
  completed: "Completed",
  completedWithFailures: "Completed with failures",
  cancelled: "Cancelled",
  failed: "Failed",
};

export const curtailmentEventStateDotClassNames: Record<CurtailmentEventState, string> = {
  pending: "bg-core-accent-fill",
  active: "bg-intent-warning-fill",
  restoring: "bg-core-accent-fill",
  completed: "bg-text-primary-30",
  completedWithFailures: "bg-text-primary-30",
  cancelled: "bg-intent-critical-fill",
  failed: "bg-intent-critical-fill",
};

export const curtailmentEventStateOrder: Record<CurtailmentEventState, number> = {
  pending: 0,
  active: 1,
  restoring: 2,
  completed: 3,
  completedWithFailures: 4,
  cancelled: 5,
  failed: 6,
};

interface CurtailmentTargetKwEvent {
  estimatedReductionKw: number;
  targetKw?: number;
}

function getMinerCountLabel(minerCount: number): string {
  return minerCount === 1 ? "miner" : "miners";
}

export function getCurtailmentTargetKw(event: CurtailmentTargetKwEvent): number {
  return event.targetKw ?? event.estimatedReductionKw;
}

export function formatCurtailmentKw(value: number, fractionDigits = 1): string {
  const finiteValue = Number.isFinite(value) ? value : 0;

  return `${finiteValue.toLocaleString(undefined, {
    maximumFractionDigits: fractionDigits,
    minimumFractionDigits: fractionDigits,
  })} kW`;
}

export function formatCurtailmentMinerCount(minerCount: number): string {
  return `${minerCount.toLocaleString()} ${getMinerCountLabel(minerCount)}`;
}

export function formatCurtailmentSelectedMinerCount(minerCount: number): string {
  return `${minerCount.toLocaleString()} selected ${getMinerCountLabel(minerCount)}`;
}

export function formatCurtailmentTargetVsActual(event: CurtailmentTargetKwEvent): string {
  return `${formatCurtailmentKw(getCurtailmentTargetKw(event))} / ${formatCurtailmentKw(event.estimatedReductionKw)}`;
}
