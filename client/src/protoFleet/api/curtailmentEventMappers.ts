import {
  type CurtailmentEvent,
  CurtailmentEventState as ProtoEventState,
  CurtailmentTargetState as ProtoTargetState,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  type CurtailmentEventState,
  formatCurtailmentSelectedMinerCount,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export const activeCurtailmentEventStates = [
  "pending",
  "active",
  "restoring",
] as const satisfies readonly CurtailmentEventState[];

export const curtailmentTargetRollupStates = [
  "pending",
  "dispatched",
  "confirmed",
  "drifted",
  "resolved",
  "released",
  "restoreFailed",
] as const;

export type CurtailmentMappedEventState = CurtailmentEventState;
export type ActiveCurtailmentMappedEventState = (typeof activeCurtailmentEventStates)[number];
export type CurtailmentMappedTargetState = (typeof curtailmentTargetRollupStates)[number];

const activeCurtailmentEventStateSet = new Set<CurtailmentMappedEventState>(activeCurtailmentEventStates);

function formatCountLabel(count: number, singular: string): string {
  const noun = count === 1 ? singular : `${singular}s`;
  return `${count} ${noun}`;
}

export function isActiveCurtailmentEventState(
  state: CurtailmentMappedEventState,
): state is ActiveCurtailmentMappedEventState {
  return activeCurtailmentEventStateSet.has(state);
}

export function mapCurtailmentEventState(state: ProtoEventState): CurtailmentMappedEventState {
  switch (state) {
    case ProtoEventState.PENDING:
      return "pending";
    case ProtoEventState.ACTIVE:
      return "active";
    case ProtoEventState.RESTORING:
      return "restoring";
    case ProtoEventState.COMPLETED:
      return "completed";
    case ProtoEventState.COMPLETED_WITH_FAILURES:
      return "completedWithFailures";
    case ProtoEventState.CANCELLED:
      return "cancelled";
    case ProtoEventState.FAILED:
      return "failed";
    case ProtoEventState.UNSPECIFIED:
    default:
      return "pending";
  }
}

export function mapCurtailmentEventStateToProto(state: CurtailmentMappedEventState): ProtoEventState {
  switch (state) {
    case "pending":
      return ProtoEventState.PENDING;
    case "active":
      return ProtoEventState.ACTIVE;
    case "restoring":
      return ProtoEventState.RESTORING;
    case "completed":
      return ProtoEventState.COMPLETED;
    case "completedWithFailures":
      return ProtoEventState.COMPLETED_WITH_FAILURES;
    case "cancelled":
      return ProtoEventState.CANCELLED;
    case "failed":
      return ProtoEventState.FAILED;
  }
}

export function mapCurtailmentTargetState(state: ProtoTargetState): CurtailmentMappedTargetState {
  switch (state) {
    case ProtoTargetState.PENDING:
      return "pending";
    case ProtoTargetState.DISPATCHED:
      return "dispatched";
    case ProtoTargetState.CONFIRMED:
      return "confirmed";
    case ProtoTargetState.DRIFTED:
      return "drifted";
    case ProtoTargetState.RESOLVED:
      return "resolved";
    case ProtoTargetState.RELEASED:
      return "released";
    case ProtoTargetState.RESTORE_FAILED:
      return "restoreFailed";
    case ProtoTargetState.UNSPECIFIED:
    default:
      return "pending";
  }
}

export function getCurtailmentScopeLabel(event: CurtailmentEvent): string {
  switch (event.scope.case) {
    case "wholeOrg":
      return "Whole org";
    case "deviceSetIds": {
      const count = event.scope.value.deviceSetIds.length;
      return formatCountLabel(count, "device set");
    }
    case "deviceIdentifiers": {
      const count = event.scope.value.deviceIdentifiers.length;
      return formatCountLabel(count, "miner");
    }
    default:
      return "Unknown scope";
  }
}

export function getCurtailmentSelectedMinerCount(event: CurtailmentEvent): number {
  return event.targetRollup?.total ?? event.targets.length;
}

export function getCurtailmentTargetSummary(event: CurtailmentEvent): string {
  return formatCurtailmentSelectedMinerCount(getCurtailmentSelectedMinerCount(event));
}

export function getCurtailmentDecisionSnapshotNumber(event: CurtailmentEvent, keys: string[]): number | undefined {
  for (const key of keys) {
    const value = event.decisionSnapshot?.[key];

    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
  }

  return undefined;
}

export function getCurtailmentEstimatedReductionKw(event: CurtailmentEvent): number {
  const snapshotReductionKw = getCurtailmentDecisionSnapshotNumber(event, [
    "estimated_reduction_kw",
    "estimatedReductionKw",
  ]);

  if (snapshotReductionKw !== undefined) {
    return snapshotReductionKw;
  }

  const baselineWatts = event.targets.reduce((total, target) => total + (target.baselinePowerW ?? 0), 0);
  return baselineWatts / 1000;
}
