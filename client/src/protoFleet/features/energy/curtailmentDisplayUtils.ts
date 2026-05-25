export const curtailmentEventStateConfigs = {
  pending: {
    label: "Pending",
    dotClassName: "bg-core-accent-fill",
    order: 0,
  },
  active: {
    label: "Active",
    dotClassName: "bg-intent-warning-fill",
    order: 1,
  },
  restoring: {
    label: "Restoring",
    dotClassName: "bg-core-accent-fill",
    order: 2,
  },
  completed: {
    label: "Completed",
    dotClassName: "bg-text-primary-30",
    order: 3,
  },
  completedWithFailures: {
    label: "Completed with failures",
    dotClassName: "bg-text-primary-30",
    order: 4,
  },
  cancelled: {
    label: "Cancelled",
    dotClassName: "bg-intent-critical-fill",
    order: 5,
  },
  failed: {
    label: "Failed",
    dotClassName: "bg-intent-critical-fill",
    order: 6,
  },
} as const;

export type CurtailmentEventState = keyof typeof curtailmentEventStateConfigs;

export const curtailmentEventStates = Object.keys(curtailmentEventStateConfigs) as CurtailmentEventState[];

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
