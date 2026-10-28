import { getCurtailmentScopeCountLabel, getCurtailmentScopeSelectionCount } from "@/protoFleet/api/curtailmentScopes";
import type {
  ActiveCurtailmentDisplayState,
  CurtailmentEventState,
  CurtailmentFormValues,
  CurtailmentTargetRollup,
  CurtailmentTargetState,
} from "@/protoFleet/api/curtailmentTypes";
import {
  estimatedReductionKwSnapshotKeys,
  formatCurtailmentKw,
  selectedCountSnapshotKeys,
  wattsPerKilowatt,
} from "@/protoFleet/api/curtailmentUtils";
import {
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentEventState as ProtoCurtailmentEventState,
  CurtailmentTargetState as ProtoCurtailmentTargetState,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getSiteDisplayName, type SiteNameById } from "@/protoFleet/api/siteNames";

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
} as const satisfies Record<CurtailmentEventState, { label: string; dotClassName: string; order: number }>;

export const curtailmentEventStates = Object.keys(curtailmentEventStateConfigs) as CurtailmentEventState[];
export const activeCurtailmentEventStates = [
  "pending",
  "active",
  "restoring",
] as const satisfies readonly CurtailmentEventState[];

export type ActiveCurtailmentEventState = (typeof activeCurtailmentEventStates)[number];

export const activeCurtailmentDisplayStateConfigs = {
  cancelled: {
    label: "Cancelled",
    dotClassName: "bg-intent-critical-fill",
  },
  curtailed: {
    label: "Curtailed",
    dotClassName: "bg-text-primary",
  },
  curtailing: {
    label: "Curtailing",
    dotClassName: "bg-core-accent-fill",
  },
  failed: {
    label: "Failed",
    dotClassName: "bg-intent-critical-fill",
  },
  pending: {
    label: "Pending",
    dotClassName: "bg-core-accent-fill",
  },
  restoreIncomplete: {
    label: "Restore incomplete",
    dotClassName: "bg-text-primary-30",
  },
  restored: {
    label: "Restored",
    dotClassName: "bg-text-primary-30",
  },
  restoring: {
    label: "Restoring",
    dotClassName: "bg-core-accent-fill",
  },
} as const satisfies Record<ActiveCurtailmentDisplayState, { label: string; dotClassName: string }>;

interface ActiveCurtailmentDisplayEvent {
  state: CurtailmentEventState;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  observedReductionKw: number;
  rollups: CurtailmentTargetRollup[];
}

interface ActiveCurtailmentDisplayOptions {
  dispatchStartedAsCurtailing?: boolean;
}

export interface ActiveCurtailmentMinerCompliance {
  curtailedCount: number;
  restoreFailedCount: number;
  restoredCount: number;
  totalCount: number;
}

// Curtail-phase dispatch progress derived from live rollups (issue #660).
// "Reached" means the sleep command went out (sent or already confirmed);
// confirmed is the stronger telemetry-backed signal. DRIFTED targets —
// observed uncurtailed after confirmation, awaiting a successful redispatch —
// count toward the denominator but never as reached, so under-curtailment is
// never masked as progress during an SLA obligation. The dispatchable
// denominator excludes unavailable targets (can't be commanded) and
// released/resolved rows (no longer curtail-targeted), so events with
// unavailable targets can still present as fully dispatched.
//
// `percent` is the operator-facing completion number: confirmed-based
// (telemetry-verified curtailed share), matching the rendered summary and the
// restore shape's percent. Issue #660 originally framed progress reached-based;
// #670's design pass consolidated the display to confirmed vs a single
// "Curtailing" bucket, so the confirmed share is the number that must never
// overstate. Reached-based pacing signals stay available via reachedCount.
//
// Known precision bound: the server's wire rollup deliberately folds the
// DISPATCHING pre-command transient into the `dispatched` bucket (see
// populateEventTargets in server/internal/handlers/curtailment/translate.go),
// so a just-claimed batch counts as sent for the sub-tick window between
// claim and command send. The client cannot split the bucket, and counting
// it as pending instead would understate progress for the far longer
// sent-awaiting-confirmation phase.
export interface ActiveCurtailmentCurtailProgress {
  confirmedCount: number;
  sentCount: number;
  driftedCount: number;
  pendingCount: number;
  unavailableCount: number;
  dispatchableCount: number;
  reachedCount: number;
  percent: number;
}

const activeCurtailmentEventStateSet = new Set<CurtailmentEventState>(activeCurtailmentEventStates);
const countedTargetStates: CurtailmentTargetState[] = [
  "pending",
  "dispatched",
  "confirmed",
  "drifted",
  "resolved",
  "released",
  "unavailable",
  "restoreFailed",
];
const startedCurtailmentDispatchTargetStates: CurtailmentTargetState[] = [
  "dispatched",
  "confirmed",
  "drifted",
  "restoreFailed",
];
const curtailedTargetStates: CurtailmentTargetState[] = ["confirmed", "resolved"];
const restoreFailedTargetStates: CurtailmentTargetState[] = ["restoreFailed"];
const restoredTargetStates: CurtailmentTargetState[] = ["resolved", "released"];

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

function formatCurtailmentScopeParts(parts: string[]): string {
  return parts.length > 0 ? parts.join(" + ") : "Unknown scope";
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

export function mapCurtailmentTargetState(state: ProtoCurtailmentTargetState): CurtailmentTargetState {
  switch (state) {
    case ProtoCurtailmentTargetState.DISPATCHING:
    case ProtoCurtailmentTargetState.DISPATCHED:
      return "dispatched";
    case ProtoCurtailmentTargetState.CONFIRMED:
      return "confirmed";
    case ProtoCurtailmentTargetState.DRIFTED:
      return "drifted";
    case ProtoCurtailmentTargetState.RESOLVED:
      return "resolved";
    case ProtoCurtailmentTargetState.RELEASED:
      return "released";
    case ProtoCurtailmentTargetState.UNAVAILABLE:
      return "unavailable";
    case ProtoCurtailmentTargetState.RESTORE_FAILED:
      return "restoreFailed";
    case ProtoCurtailmentTargetState.PENDING:
    case ProtoCurtailmentTargetState.UNSPECIFIED:
    default:
      return "pending";
  }
}

function getRollupsFromTargets(event: ProtoCurtailmentEvent): CurtailmentTargetRollup[] {
  const counts = new Map<CurtailmentTargetState, number>();

  for (const target of event.targets) {
    const state = mapCurtailmentTargetState(target.state);
    counts.set(state, (counts.get(state) ?? 0) + 1);
  }

  return Array.from(counts, ([state, count]) => ({ state, count }));
}

export function getCurtailmentTargetRollups(event: ProtoCurtailmentEvent): CurtailmentTargetRollup[] {
  const rollup = event.targetRollup;
  if (!rollup) {
    return getRollupsFromTargets(event);
  }

  const rollups: CurtailmentTargetRollup[] = [
    { state: "pending", count: rollup.pending },
    { state: "dispatched", count: rollup.dispatched },
    { state: "confirmed", count: rollup.confirmed },
    { state: "drifted", count: rollup.drifted },
    { state: "resolved", count: rollup.resolved },
    { state: "released", count: rollup.released },
    { state: "unavailable", count: rollup.unavailable },
    { state: "restoreFailed", count: rollup.restoreFailed },
  ];

  return rollups.filter((targetRollup) => targetRollup.count > 0);
}

// Audit-context count: prefers the event-start decision snapshot, so
// history rows keep describing the original selection. Active surfaces
// should use getCurtailmentEventLiveTargetCount instead.
export function getCurtailmentEventSelectedMinerCount(event: ProtoCurtailmentEvent): number {
  const snapshotSelectedCount = getSnapshotNumber(event, selectedCountSnapshotKeys);
  return snapshotSelectedCount ?? event.targetRollup?.total ?? event.targets.length;
}

// Live operational count for active surfaces: the target rollup describes the
// event's current target set, which closed-loop claims and all-paired policy
// changes can grow far past the event-start snapshot count. When no rollup is
// available the legacy fallbacks apply: hydrated target rows, then the
// event-start snapshot count.
export function getCurtailmentEventLiveTargetCount(event: ProtoCurtailmentEvent): number {
  if (event.targetRollup) {
    return event.targetRollup.total;
  }
  if (event.targets.length > 0) {
    return event.targets.length;
  }
  return getSnapshotNumber(event, selectedCountSnapshotKeys) ?? 0;
}

export function getCurtailmentEventEstimatedReductionKw(event: ProtoCurtailmentEvent): number {
  const snapshotEstimatedReductionKw = getSnapshotNumber(event, estimatedReductionKwSnapshotKeys);
  if (snapshotEstimatedReductionKw !== undefined) {
    return snapshotEstimatedReductionKw;
  }

  const baselinePowerW = event.targets.reduce((total, target) => total + (target.baselinePowerW ?? 0), 0);
  return baselinePowerW / wattsPerKilowatt;
}

function getConfirmedTargetCount(event: ProtoCurtailmentEvent): number {
  if (event.targetRollup) {
    return event.targetRollup.confirmed;
  }

  return event.targets.filter((target) => target.state === ProtoCurtailmentTargetState.CONFIRMED).length;
}

function getEstimatedObservedReductionKw(event: ProtoCurtailmentEvent, estimatedReductionKw: number): number {
  // The confirmed numerator is rollup-derived, so pair it with the live
  // rollup total; a stale snapshot denominator would push progress past 100%
  // once the live target set outgrows the event-start selection.
  const selectedCount = getCurtailmentEventLiveTargetCount(event);
  if (selectedCount <= 0) {
    return 0;
  }

  return estimatedReductionKw * (getConfirmedTargetCount(event) / selectedCount);
}

export function getCurtailmentEventObservedReductionKw(
  event: ProtoCurtailmentEvent,
  estimatedReductionKw = getCurtailmentEventEstimatedReductionKw(event),
): number {
  let observedReductionTotalW = 0;
  let hasObservedReduction = false;

  for (const { baselinePowerW, observedPowerW } of event.targets) {
    if (baselinePowerW !== undefined && observedPowerW !== undefined) {
      hasObservedReduction = true;
      observedReductionTotalW += Math.max(baselinePowerW - observedPowerW, 0);
    }
  }

  return hasObservedReduction
    ? observedReductionTotalW / wattsPerKilowatt
    : getEstimatedObservedReductionKw(event, estimatedReductionKw);
}

export function getCurtailmentEventScopeLabel(event: ProtoCurtailmentEvent, siteNameById?: SiteNameById): string {
  if (event.scopes.length > 0) {
    const siteLabelsById = new Map<string, string>();
    const buildingIds = new Set<bigint>();
    const rackIds = new Set<bigint>();
    const groupIds = new Set<bigint>();
    const deviceIdentifiers = new Set<string>();

    for (const scope of event.scopes) {
      switch (scope.scope.case) {
        case "wholeOrg":
          return "Whole fleet";
        case "site": {
          const siteId = scope.scope.value.siteId.toString();
          siteLabelsById.set(siteId, getSiteDisplayName(siteId, siteNameById));
          break;
        }
        case "building":
          buildingIds.add(scope.scope.value.buildingId);
          break;
        case "rack":
          rackIds.add(scope.scope.value.rackId);
          break;
        case "group":
          groupIds.add(scope.scope.value.groupId);
          break;
        case "deviceIdentifiers":
          scope.scope.value.deviceIdentifiers.forEach((deviceIdentifier) => deviceIdentifiers.add(deviceIdentifier));
          break;
        case undefined:
          break;
      }
    }

    const parts: string[] = [];
    if (siteLabelsById.size === 1) {
      parts.push([...siteLabelsById.values()][0]);
    } else if (siteLabelsById.size > 1) {
      parts.push(
        `${siteLabelsById.size.toLocaleString()} ${getCurtailmentScopeCountLabel(siteLabelsById.size, "site")}`,
      );
    }

    if (buildingIds.size > 0) {
      parts.push(`${buildingIds.size.toLocaleString()} ${getCurtailmentScopeCountLabel(buildingIds.size, "building")}`);
    }
    if (rackIds.size > 0) {
      parts.push(`${rackIds.size.toLocaleString()} ${getCurtailmentScopeCountLabel(rackIds.size, "rack")}`);
    }
    if (groupIds.size > 0) {
      parts.push(`${groupIds.size.toLocaleString()} ${getCurtailmentScopeCountLabel(groupIds.size, "group")}`);
    }

    if (deviceIdentifiers.size > 0) {
      parts.push(
        `${deviceIdentifiers.size.toLocaleString()} ${getCurtailmentScopeCountLabel(deviceIdentifiers.size, "miner")}`,
      );
    }

    return formatCurtailmentScopeParts(parts);
  }

  switch (event.scope.case) {
    case "wholeOrg":
      return "Whole fleet";
    case "site":
      return getSiteDisplayName(event.scope.value.siteId, siteNameById);
    case "deviceIdentifiers": {
      const count = event.scope.value.deviceIdentifiers.length;
      return `${count.toLocaleString()} ${getCurtailmentScopeCountLabel(count, "miner")}`;
    }
    default:
      return "Unknown scope";
  }
}

export function getCurtailmentTargetKw(event: CurtailmentTargetKwEvent): number {
  return event.targetKw ?? event.estimatedReductionKw;
}

export function getCurtailmentProgressPercent(value: number, total: number): number {
  if (!Number.isFinite(value) || !Number.isFinite(total) || total <= 0) {
    return 0;
  }

  return Math.min(Math.max((value / total) * 100, 0), 100);
}

export function getActiveCurtailmentRollupCount(
  event: Pick<ActiveCurtailmentDisplayEvent, "rollups">,
  states: CurtailmentTargetState[],
): number {
  return event.rollups.reduce((total, rollup) => {
    if (!states.includes(rollup.state)) {
      return total;
    }

    return total + rollup.count;
  }, 0);
}

const sentCurtailTargetStates: CurtailmentTargetState[] = ["dispatched"];
const confirmedCurtailTargetStates: CurtailmentTargetState[] = ["confirmed"];
const driftedCurtailTargetStates: CurtailmentTargetState[] = ["drifted"];
const pendingCurtailTargetStates: CurtailmentTargetState[] = ["pending"];
const unavailableCurtailTargetStates: CurtailmentTargetState[] = ["unavailable"];

export function getActiveCurtailmentCurtailProgress(
  event: Pick<ActiveCurtailmentDisplayEvent, "rollups">,
): ActiveCurtailmentCurtailProgress {
  const confirmedCount = getActiveCurtailmentRollupCount(event, confirmedCurtailTargetStates);
  const sentCount = getActiveCurtailmentRollupCount(event, sentCurtailTargetStates);
  const driftedCount = getActiveCurtailmentRollupCount(event, driftedCurtailTargetStates);
  const pendingCount = getActiveCurtailmentRollupCount(event, pendingCurtailTargetStates);
  const unavailableCount = getActiveCurtailmentRollupCount(event, unavailableCurtailTargetStates);
  const dispatchableCount = confirmedCount + sentCount + driftedCount + pendingCount;
  const reachedCount = confirmedCount + sentCount;

  return {
    confirmedCount,
    sentCount,
    driftedCount,
    pendingCount,
    unavailableCount,
    dispatchableCount,
    reachedCount,
    // Floor so 100% is only reported when every dispatchable target has
    // confirmed; rounding 99.6% up would claim completion with targets pending.
    percent: Math.floor(getCurtailmentProgressPercent(confirmedCount, dispatchableCount)),
  };
}

// Restore-phase progress derived from live rollups: resolved and released
// targets are back online; restore failures are terminal and reported
// separately (they never count as restored); everything still curtailed or
// in-flight is awaiting. The restorable denominator excludes unavailable
// targets, mirroring the curtail-phase progress semantics.
export interface ActiveCurtailmentRestoreProgress {
  restoredCount: number;
  failedCount: number;
  awaitingCount: number;
  unavailableCount: number;
  restorableCount: number;
  percent: number;
}

const awaitingRestoreTargetStates: CurtailmentTargetState[] = ["pending", "dispatched", "drifted", "confirmed"];

export function getActiveCurtailmentRestoreProgress(
  event: Pick<ActiveCurtailmentDisplayEvent, "rollups">,
): ActiveCurtailmentRestoreProgress {
  const restoredCount = getActiveCurtailmentRollupCount(event, restoredTargetStates);
  const failedCount = getActiveCurtailmentRollupCount(event, restoreFailedTargetStates);
  const awaitingCount = getActiveCurtailmentRollupCount(event, awaitingRestoreTargetStates);
  const unavailableCount = getActiveCurtailmentRollupCount(event, unavailableCurtailTargetStates);
  const restorableCount = restoredCount + failedCount + awaitingCount;

  return {
    restoredCount,
    failedCount,
    awaitingCount,
    unavailableCount,
    restorableCount,
    // Floored for the same reason as the curtail percent: never overstate
    // completion on an SLA surface.
    percent: Math.floor(getCurtailmentProgressPercent(restoredCount, restorableCount)),
  };
}

export function getActiveCurtailmentMinerCompliance(
  event: ActiveCurtailmentDisplayEvent,
): ActiveCurtailmentMinerCompliance {
  const curtailedCount = getActiveCurtailmentRollupCount(event, curtailedTargetStates);
  const restoreFailedCount = getActiveCurtailmentRollupCount(event, restoreFailedTargetStates);
  const restoredCount = getActiveCurtailmentRollupCount(event, restoredTargetStates);
  const countedTargetCount = getActiveCurtailmentRollupCount(event, countedTargetStates);
  const totalCount = Math.max(event.selectedMiners, countedTargetCount);

  return {
    curtailedCount,
    restoreFailedCount,
    restoredCount,
    totalCount,
  };
}

export function hasActiveCurtailmentDispatchStarted(event: ActiveCurtailmentDisplayEvent): boolean {
  return getActiveCurtailmentRollupCount(event, startedCurtailmentDispatchTargetStates) > 0;
}

function isRestoredEventState(state: CurtailmentEventState): boolean {
  return state === "completed";
}

function isRestoreIncompleteEventState(state: CurtailmentEventState): boolean {
  return state === "completedWithFailures";
}

export function getActiveCurtailmentDisplayState(
  event: ActiveCurtailmentDisplayEvent,
  options: ActiveCurtailmentDisplayOptions = {},
): ActiveCurtailmentDisplayState {
  if (event.state === "restoring") {
    return "restoring";
  }

  if (event.state === "pending") {
    return options.dispatchStartedAsCurtailing && hasActiveCurtailmentDispatchStarted(event) ? "curtailing" : "pending";
  }

  if (isRestoreIncompleteEventState(event.state)) {
    return "restoreIncomplete";
  }

  if (isRestoredEventState(event.state)) {
    return "restored";
  }

  if (event.state === "cancelled") {
    return "cancelled";
  }

  if (event.state === "failed") {
    return "failed";
  }

  const targetKw = getCurtailmentTargetKw(event);
  const compliance = getActiveCurtailmentMinerCompliance(event);
  const powerShedPercent = getCurtailmentProgressPercent(event.observedReductionKw, targetKw);
  const curtailedPercent = getCurtailmentProgressPercent(compliance.curtailedCount, compliance.totalCount);

  return powerShedPercent >= 100 || curtailedPercent >= 100 ? "curtailed" : "curtailing";
}

export function formatCurtailmentMinerCount(minerCount: number): string {
  return `${minerCount.toLocaleString()} ${getCurtailmentScopeCountLabel(minerCount, "miner")}`;
}

export function formatCurtailmentFacilityFanCount(deviceCount: number): string {
  return `${deviceCount.toLocaleString()} ${getCurtailmentScopeCountLabel(deviceCount, "device")}`;
}

export function formatCurtailmentAppliesToSummary(minerCount: number, facilityFanDeviceCount = 0): string {
  const parts = [formatCurtailmentMinerCount(minerCount)];

  if (facilityFanDeviceCount > 0) {
    parts.push(formatCurtailmentFacilityFanCount(facilityFanDeviceCount));
  }

  return parts.join(", ");
}

export function formatCurtailmentSelectedMinerCount(minerCount: number): string {
  return `${minerCount.toLocaleString()} selected ${getCurtailmentScopeCountLabel(minerCount, "miner")}`;
}

export function formatCurtailmentTargetVsActual(event: CurtailmentTargetKwEvent): string {
  return `${formatCurtailmentKw(getCurtailmentTargetKw(event))} / ${formatCurtailmentKw(event.estimatedReductionKw)}`;
}

function formatSelectedScopeLabel(count: number, singular: string): string {
  return count === 1 ? `from 1 selected ${singular}` : `from ${count} selected ${singular}s`;
}

function formatSelectedMinerScopeLabel(count: number): string {
  return count === 1 ? "from the selected miner" : "from selected miners";
}

export function formatCurtailmentPlanScopeLabel(values: CurtailmentFormValues): string {
  if (values.minerSelectionMode === "all") {
    return "across the fleet";
  }

  const selectedMinerCount = values.deviceIdentifiers?.length ?? 0;
  const selectedSiteIds =
    values.siteSelection === "site" || values.siteSelection === "allSites"
      ? values.siteIds?.length
        ? values.siteIds
        : values.siteId
          ? [values.siteId]
          : []
      : [];
  const selectedSiteLabel =
    values.siteSelection === "allSites"
      ? "all sites"
      : selectedSiteIds.length === 1
        ? values.scopeId?.trim() || `site ${selectedSiteIds[0]}`
        : `${selectedSiteIds.length} selected sites`;
  const siteLabel = `from ${selectedSiteLabel}`;
  const canonicalScopeCount = getCurtailmentScopeSelectionCount(values) ?? 0;
  if (values.scopeType === "explicitMiners" && selectedMinerCount > 0) {
    return formatSelectedMinerScopeLabel(selectedMinerCount);
  }
  if (values.scopeType === "site" && selectedSiteIds.length > 0) {
    return siteLabel;
  }

  switch (values.scopeType) {
    case "building":
      return formatSelectedScopeLabel(canonicalScopeCount, "building");
    case "rack":
      return formatSelectedScopeLabel(canonicalScopeCount, "rack");
    case "group":
      return formatSelectedScopeLabel(canonicalScopeCount, "group");
    case "site":
    case "explicitMiners":
    case "wholeOrg":
    default:
      return "across the fleet";
  }
}

type CurtailmentDurationUnit = "hour" | "minute" | "second";
type CurtailmentDurationOptions = {
  style?: "elapsed" | "long" | "estimate";
  approximate?: boolean;
};

export function formatCurtailmentDuration(
  totalSeconds: number,
  { style = "elapsed", approximate = true }: CurtailmentDurationOptions = {},
): string {
  if (totalSeconds <= 0 || (style !== "estimate" && !Number.isFinite(totalSeconds))) {
    return style === "elapsed" ? "0s" : style === "long" ? "Immediate" : "Immediately";
  }

  let parts: [number, CurtailmentDurationUnit][];
  if (style === "elapsed") {
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = Math.floor(totalSeconds % 60);
    parts =
      hours > 0
        ? [
            [hours, "hour"],
            [minutes, "minute"],
          ]
        : minutes > 0
          ? [
              [minutes, "minute"],
              [seconds, "second"],
            ]
          : [[seconds, "second"]];
    parts = parts.filter(([value], index) => index === 0 || value > 0);
  } else if (style === "long") {
    parts = [
      [Math.floor(totalSeconds / 60), "minute"],
      [totalSeconds % 60, "second"],
    ];
    parts = parts.filter(([value]) => value > 0);
  } else if (totalSeconds < 60) {
    parts = [[totalSeconds, "second"]];
  } else {
    const minutes = Math.ceil(totalSeconds / 60);
    if (minutes < 60) {
      parts = [[minutes, "minute"]];
    } else {
      const remainingMinutes = minutes % 60;
      parts = [[Math.floor(minutes / 60), "hour"]];
      if (remainingMinutes !== 0) {
        parts.push([remainingMinutes, "minute"]);
      }
    }
  }

  const prefix = style === "estimate" && approximate ? "~" : "";
  return (
    prefix +
    parts
      .map(([value, unit]) => {
        const magnitude =
          style === "long" || (style === "elapsed" && unit === "hour") ? value.toLocaleString() : String(value);
        return style === "elapsed" ? `${magnitude}${unit[0]}` : `${magnitude} ${unit}${value === 1 ? "" : "s"}`;
      })
      .join(style === "long" ? ", " : " ")
  );
}
