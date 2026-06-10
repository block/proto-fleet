import type { Timestamp } from "@bufbuild/protobuf/wkt";

import {
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  getActiveCurtailmentDisplayState,
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventObservedReductionKw,
  getCurtailmentEventScopeLabel,
  getCurtailmentEventSelectedMinerCount,
  getCurtailmentTargetRollups,
  isActiveCurtailmentEventState,
  mapCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { CurtailmentHistoryEvent, CurtailmentPriority } from "@/protoFleet/features/energy/CurtailmentHistory";
import type { CurtailmentMode, CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

const wattsPerKilowatt = 1000;

interface ObservedPowerSummary {
  observedReductionKw: number;
  remainingPowerKw?: number;
}

export function timestampToIsoString(timestamp?: Timestamp): string | undefined {
  if (!timestamp) {
    return undefined;
  }

  const date = new Date(Number(timestamp.seconds) * 1000 + Math.floor(timestamp.nanos / 1_000_000));
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

export function getFixedKwTarget(event: ProtoCurtailmentEvent): number | undefined {
  return event.modeParams.case === "fixedKw" ? event.modeParams.value.targetKw : undefined;
}

export function getFixedKwTolerance(event: ProtoCurtailmentEvent): number | undefined {
  return event.modeParams.case === "fixedKw" ? event.modeParams.value.toleranceKw : undefined;
}

function formatPositiveNumberField(value: number | undefined): string {
  if (value === undefined || value <= 0) {
    return "";
  }

  return String(value);
}

function mapCurtailmentModeToFormValue(event: ProtoCurtailmentEvent): CurtailmentMode {
  return event.mode === ProtoCurtailmentMode.FULL_FLEET ? "fullFleet" : "fixedKwReduction";
}

function mapCurtailmentEventScopeToFormValues(
  event: ProtoCurtailmentEvent,
): Pick<CurtailmentSubmitValues, "scopeType" | "scopeId" | "siteId" | "deviceSetIds" | "deviceIdentifiers"> {
  switch (event.scope.case) {
    case "site":
      return {
        scopeType: "site",
        scopeId: `site-${event.scope.value.siteId.toString()}`,
        siteId: event.scope.value.siteId.toString(),
        deviceSetIds: [],
        deviceIdentifiers: [],
      };
    case "deviceIdentifiers":
      return {
        scopeType: "explicitMiners",
        scopeId: "explicit-miners",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [...event.scope.value.deviceIdentifiers],
      };
    case "deviceSetIds":
      return {
        scopeType: "deviceSet",
        scopeId: "device-sets",
        siteId: "",
        deviceSetIds: [...event.scope.value.deviceSetIds],
        deviceIdentifiers: [],
      };
    case "wholeOrg":
    default:
      return {
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteId: "",
        deviceSetIds: [],
        deviceIdentifiers: [],
      };
  }
}

export function mapCurtailmentEventToFormValues(event: ProtoCurtailmentEvent): CurtailmentSubmitValues {
  const fixedKwTarget = getFixedKwTarget(event);
  const fixedKwTolerance = getFixedKwTolerance(event);

  return {
    ...mapCurtailmentEventScopeToFormValues(event),
    responseProfileId: "customPlan",
    curtailmentMode: mapCurtailmentModeToFormValue(event),
    minerSelectionStrategy: "leastEfficientFirst",
    targetKw: fixedKwTarget !== undefined ? String(fixedKwTarget) : "",
    toleranceKw: fixedKwTolerance !== undefined ? String(fixedKwTolerance) : "",
    priority: event.priority === ProtoCurtailmentPriority.EMERGENCY ? "emergency" : "normal",
    minDurationSec: formatPositiveNumberField(event.minCurtailedDurationSec),
    maxDurationSec: formatPositiveNumberField(event.maxDurationSeconds),
    restoreBatchSize: formatPositiveNumberField(event.restoreBatchSize),
    restoreIntervalSec: formatPositiveNumberField(event.restoreBatchIntervalSec),
    reason: event.reason || "Curtailment",
    includeMaintenance: event.includeMaintenance,
  };
}

export function mapCurtailmentPriority(priority: ProtoCurtailmentPriority): CurtailmentPriority {
  switch (priority) {
    case ProtoCurtailmentPriority.EMERGENCY:
      return "emergency";
    case ProtoCurtailmentPriority.HIGH:
      return "high";
    case ProtoCurtailmentPriority.NORMAL:
    case ProtoCurtailmentPriority.UNSPECIFIED:
    default:
      return "normal";
  }
}

function getSourceLabel(event: ProtoCurtailmentEvent): string {
  return event.externalSource.trim() || "Manual";
}

function getObservedPowerSummary(event: ProtoCurtailmentEvent, estimatedReductionKw: number): ObservedPowerSummary {
  let observedPowerTotalW = 0;
  let hasObservedPower = false;

  for (const { observedPowerW } of event.targets) {
    if (observedPowerW !== undefined) {
      hasObservedPower = true;
      observedPowerTotalW += observedPowerW;
    }
  }

  return {
    observedReductionKw: getCurtailmentEventObservedReductionKw(event, estimatedReductionKw),
    remainingPowerKw: hasObservedPower ? observedPowerTotalW / wattsPerKilowatt : undefined,
  };
}

export function mapActiveCurtailmentEvent(event: ProtoCurtailmentEvent): ActiveCurtailmentEvent {
  const estimatedReductionKw = getCurtailmentEventEstimatedReductionKw(event);
  const observedPowerSummary = getObservedPowerSummary(event, estimatedReductionKw);

  return {
    reason: event.reason || "Curtailment",
    state: mapCurtailmentEventState(event.state),
    scopeLabel: getCurtailmentEventScopeLabel(event),
    endedAt: timestampToIsoString(event.endedAt),
    selectedMiners: getCurtailmentEventSelectedMinerCount(event),
    estimatedReductionKw,
    targetKw: getFixedKwTarget(event),
    observedReductionKw: observedPowerSummary.observedReductionKw,
    remainingPowerKw: observedPowerSummary.remainingPowerKw,
    restoreBatchSize: event.effectiveBatchSize || event.restoreBatchSize,
    restoreBatchIntervalSec: event.restoreBatchIntervalSec,
    rollups: getCurtailmentTargetRollups(event),
  };
}

export function mapCurtailmentHistoryEvent(event: ProtoCurtailmentEvent): CurtailmentHistoryEvent {
  return {
    id: event.eventUuid,
    reason: event.reason || "Curtailment",
    state: mapCurtailmentEventState(event.state),
    priority: mapCurtailmentPriority(event.priority),
    scopeLabel: getCurtailmentEventScopeLabel(event),
    selectedMiners: getCurtailmentEventSelectedMinerCount(event),
    estimatedReductionKw: getCurtailmentEventEstimatedReductionKw(event),
    targetKw: getFixedKwTarget(event),
    sourceLabel: getSourceLabel(event),
    startedAt: timestampToIsoString(event.startedAt),
    endedAt: timestampToIsoString(event.endedAt),
    scheduledAt: timestampToIsoString(event.scheduledStartAt),
    createdAt: timestampToIsoString(event.createdAt),
  };
}

export function mapActiveCurtailmentHistoryEvent(event: ProtoCurtailmentEvent): CurtailmentHistoryEvent {
  const historyEvent = mapCurtailmentHistoryEvent(event);

  if (!isActiveCurtailmentEventState(historyEvent.state)) {
    return historyEvent;
  }

  return {
    ...historyEvent,
    displayState: getActiveCurtailmentDisplayState(mapActiveCurtailmentEvent(event), {
      dispatchStartedAsCurtailing: true,
    }),
  };
}
