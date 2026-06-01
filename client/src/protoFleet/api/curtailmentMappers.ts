import type { Timestamp } from "@bufbuild/protobuf/wkt";

import {
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentPriority as ProtoCurtailmentPriority,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventScopeLabel,
  getCurtailmentEventSelectedMinerCount,
  getCurtailmentTargetRollups,
  mapCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { CurtailmentHistoryEvent, CurtailmentPriority } from "@/protoFleet/features/energy/CurtailmentHistory";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

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

function mapCurtailmentEventScopeToFormValues(
  event: ProtoCurtailmentEvent,
): Pick<CurtailmentSubmitValues, "scopeType" | "scopeId" | "deviceSetIds" | "deviceIdentifiers"> {
  switch (event.scope.case) {
    case "deviceIdentifiers":
      return {
        scopeType: "explicitMiners",
        scopeId: "explicit-miners",
        deviceSetIds: [],
        deviceIdentifiers: [...event.scope.value.deviceIdentifiers],
      };
    case "deviceSetIds":
      return {
        scopeType: "deviceSet",
        scopeId: "device-sets",
        deviceSetIds: [...event.scope.value.deviceSetIds],
        deviceIdentifiers: [],
      };
    case "wholeOrg":
    default:
      return {
        scopeType: "wholeOrg",
        scopeId: "whole-org",
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
    curtailmentMode: "fixedKwReduction",
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
  let observedReductionTotalW = 0;
  let hasObservedPower = false;
  let hasObservedReduction = false;

  for (const { baselinePowerW, observedPowerW } of event.targets) {
    if (observedPowerW !== undefined) {
      hasObservedPower = true;
      observedPowerTotalW += observedPowerW;
    }

    if (baselinePowerW !== undefined && observedPowerW !== undefined) {
      hasObservedReduction = true;
      observedReductionTotalW += Math.max(baselinePowerW - observedPowerW, 0);
    }
  }

  return {
    observedReductionKw: hasObservedReduction ? observedReductionTotalW / wattsPerKilowatt : estimatedReductionKw,
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
