import {
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentEventState as ProtoCurtailmentEventState,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

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
export const activeCurtailmentEventStates = [
  "pending",
  "active",
  "restoring",
] as const satisfies readonly CurtailmentEventState[];

export type ActiveCurtailmentEventState = (typeof activeCurtailmentEventStates)[number];

const activeCurtailmentEventStateSet = new Set<CurtailmentEventState>(activeCurtailmentEventStates);
const estimatedReductionKwSnapshotKeys = ["estimated_reduction_kw", "estimatedReductionKw"] as const;
const selectedCountSnapshotKeys = ["selected_count", "selectedCount"] as const;
const wattsPerKilowatt = 1000;

interface CurtailmentTargetKwEvent {
  estimatedReductionKw: number;
  targetKw?: number;
}

function getSnapshotNumber(event: ProtoCurtailmentEvent, keys: readonly string[]): number | undefined {
  for (const key of keys) {
    const value = event.decisionSnapshot?.[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
  }
  return undefined;
}

function getMinerCountLabel(minerCount: number): string {
  return minerCount === 1 ? "miner" : "miners";
}

export function isActiveCurtailmentEventState(state: CurtailmentEventState): state is ActiveCurtailmentEventState {
  return activeCurtailmentEventStateSet.has(state);
}

export function mapCurtailmentEventState(state: ProtoCurtailmentEventState): CurtailmentEventState {
  switch (state) {
    case ProtoCurtailmentEventState.ACTIVE:
      return "active";
    case ProtoCurtailmentEventState.RESTORING:
      return "restoring";
    case ProtoCurtailmentEventState.COMPLETED:
      return "completed";
    case ProtoCurtailmentEventState.COMPLETED_WITH_FAILURES:
      return "completedWithFailures";
    case ProtoCurtailmentEventState.CANCELLED:
      return "cancelled";
    case ProtoCurtailmentEventState.FAILED:
      return "failed";
    case ProtoCurtailmentEventState.PENDING:
    case ProtoCurtailmentEventState.UNSPECIFIED:
    default:
      return "pending";
  }
}

export function getCurtailmentEventSelectedMinerCount(event: ProtoCurtailmentEvent): number {
  const snapshotSelectedCount = getSnapshotNumber(event, selectedCountSnapshotKeys);
  return snapshotSelectedCount ?? event.targetRollup?.total ?? event.targets.length;
}

export function getCurtailmentEventEstimatedReductionKw(event: ProtoCurtailmentEvent): number {
  const snapshotEstimatedReductionKw = getSnapshotNumber(event, estimatedReductionKwSnapshotKeys);
  if (snapshotEstimatedReductionKw !== undefined) {
    return snapshotEstimatedReductionKw;
  }

  const baselinePowerW = event.targets.reduce((total, target) => total + (target.baselinePowerW ?? 0), 0);
  return baselinePowerW / wattsPerKilowatt;
}

export function getCurtailmentEventScopeLabel(event: ProtoCurtailmentEvent): string {
  switch (event.scope.case) {
    case "wholeOrg":
      return "Whole fleet";
    case "deviceSetIds":
      return `${event.scope.value.deviceSetIds.length.toLocaleString()} device sets`;
    case "deviceIdentifiers": {
      const count = event.scope.value.deviceIdentifiers.length;
      return `${count.toLocaleString()} ${getMinerCountLabel(count)}`;
    }
    default:
      return "Unknown scope";
  }
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
