import type { Timestamp } from "@bufbuild/protobuf/wkt";

import {
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getSiteDisplayName, type SiteNameById } from "@/protoFleet/api/siteNames";
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
const automationExternalSource = "curtailment_automation";
const automationSourceLabel = "Curtailment automation";
const estimatedReductionKwSnapshotKeys = ["estimated_reduction_kw", "estimatedReductionKw"] as const;
const selectedCountSnapshotKeys = ["selected_count", "selectedCount"] as const;

interface ObservedPowerSummary {
  observedReductionKw: number;
  remainingPowerKw?: number;
}

interface CurtailmentMapperOptions {
  siteNameById?: SiteNameById;
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

function formatNonNegativeNumberField(value: number | undefined): string {
  if (value === undefined || value < 0) {
    return "";
  }

  return String(value);
}

function mapCurtailmentModeToFormValue(event: ProtoCurtailmentEvent): CurtailmentMode {
  return event.mode === ProtoCurtailmentMode.FULL_FLEET ? "fullFleet" : "fixedKwReduction";
}

function mapCurtailmentEventScopeToFormValues(
  event: ProtoCurtailmentEvent,
  options: CurtailmentMapperOptions = {},
): Pick<
  CurtailmentSubmitValues,
  | "scopeType"
  | "scopeId"
  | "siteSelection"
  | "siteId"
  | "siteIds"
  | "siteNamesById"
  | "deviceSetIds"
  | "deviceIdentifiers"
> {
  if (event.scopes.length > 0) {
    const siteIds: string[] = [];
    const siteNamesById: Record<string, string> = {};
    const deviceIdentifiers: string[] = [];
    for (const scope of event.scopes) {
      switch (scope.scope.case) {
        case "wholeOrg":
          return {
            scopeType: "wholeOrg",
            scopeId: "whole-org",
            siteSelection: "allSites",
            siteId: "",
            siteIds: [],
            siteNamesById: {},
            deviceSetIds: [],
            deviceIdentifiers: [],
          };
        case "site":
          {
            const siteId = scope.scope.value.siteId.toString();
            siteIds.push(siteId);
            siteNamesById[siteId] = getSiteDisplayName(siteId, options.siteNameById);
          }
          break;
        case "deviceIdentifiers":
          deviceIdentifiers.push(...scope.scope.value.deviceIdentifiers);
          break;
        case "deviceSetIds":
        case undefined:
          break;
      }
    }
    const uniqueSiteIds = [...new Set(siteIds)];
    const siteId = uniqueSiteIds[0] ?? "";
    return {
      scopeType: deviceIdentifiers.length > 0 ? "explicitMiners" : uniqueSiteIds.length > 0 ? "site" : "wholeOrg",
      scopeId:
        uniqueSiteIds.length === 1
          ? siteNamesById[siteId]
          : uniqueSiteIds.length > 1
            ? `${uniqueSiteIds.length} sites`
            : deviceIdentifiers.length > 0
              ? undefined
              : "whole-org",
      siteSelection: siteId ? "site" : "none",
      siteId,
      siteIds: uniqueSiteIds,
      siteNamesById,
      deviceSetIds: [],
      deviceIdentifiers: [...new Set(deviceIdentifiers)],
    };
  }

  switch (event.scope.case) {
    case "site": {
      const siteId = event.scope.value.siteId.toString();
      return {
        scopeType: "site",
        scopeId: getSiteDisplayName(siteId, options.siteNameById),
        siteSelection: "site",
        siteId,
        siteIds: [siteId],
        siteNamesById: { [siteId]: getSiteDisplayName(siteId, options.siteNameById) },
        deviceSetIds: [],
        deviceIdentifiers: [],
      };
    }
    case "deviceIdentifiers":
      return {
        scopeType: "explicitMiners",
        scopeId: "explicit-miners",
        siteSelection: "none",
        siteId: "",
        siteIds: [],
        siteNamesById: {},
        deviceSetIds: [],
        deviceIdentifiers: [...event.scope.value.deviceIdentifiers],
      };
    case "deviceSetIds":
      return {
        scopeType: "deviceSet",
        scopeId: "device-sets",
        siteSelection: "none",
        siteId: "",
        siteIds: [],
        siteNamesById: {},
        deviceSetIds: [...event.scope.value.deviceSetIds],
        deviceIdentifiers: [],
      };
    case "wholeOrg":
    default:
      return {
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        siteSelection: "allSites",
        siteId: "",
        siteIds: [],
        siteNamesById: {},
        deviceSetIds: [],
        deviceIdentifiers: [],
      };
  }
}

export function mapCurtailmentEventToFormValues(
  event: ProtoCurtailmentEvent,
  options: CurtailmentMapperOptions = {},
): CurtailmentSubmitValues {
  const fixedKwTarget = getFixedKwTarget(event);
  const fixedKwTolerance = getFixedKwTolerance(event);
  const hasCurtailBatchSize = (event.curtailBatchSize ?? 0) > 0;

  return {
    ...mapCurtailmentEventScopeToFormValues(event, options),
    responseProfileId: "customPlan",
    curtailmentMode: mapCurtailmentModeToFormValue(event),
    minerSelectionStrategy: "leastEfficientFirst",
    targetKw: fixedKwTarget !== undefined ? String(fixedKwTarget) : "",
    toleranceKw: fixedKwTolerance !== undefined ? String(fixedKwTolerance) : "",
    priority: event.priority === ProtoCurtailmentPriority.EMERGENCY ? "emergency" : "normal",
    minDurationSec: formatPositiveNumberField(event.minCurtailedDurationSec),
    maxDurationSec: formatPositiveNumberField(event.maxDurationSeconds),
    curtailBatchSize: hasCurtailBatchSize ? formatPositiveNumberField(event.curtailBatchSize) : "",
    curtailBatchIntervalSec: hasCurtailBatchSize ? formatNonNegativeNumberField(event.curtailBatchIntervalSec) : "",
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

function isAutomationExternalSource(externalSource: string): boolean {
  return externalSource === automationExternalSource;
}

function getSourceLabel(externalSource: string): string {
  if (isAutomationExternalSource(externalSource)) {
    return automationSourceLabel;
  }

  return externalSource || "Manual";
}

function hasSnapshotNumber(event: ProtoCurtailmentEvent, keys: readonly string[]): boolean {
  return keys.some((key) => typeof event.decisionSnapshot?.[key] === "number");
}

export function hasCurtailmentTargetMetrics(event: ProtoCurtailmentEvent): boolean {
  return (
    Boolean(event.targetRollup) ||
    event.targets.length > 0 ||
    hasSnapshotNumber(event, estimatedReductionKwSnapshotKeys) ||
    hasSnapshotNumber(event, selectedCountSnapshotKeys)
  );
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

export function mapActiveCurtailmentEvent(
  event: ProtoCurtailmentEvent,
  options: CurtailmentMapperOptions = {},
): ActiveCurtailmentEvent {
  const estimatedReductionKw = getCurtailmentEventEstimatedReductionKw(event);
  const observedPowerSummary = getObservedPowerSummary(event, estimatedReductionKw);
  const externalSource = event.externalSource.trim();

  return {
    reason: event.reason || "Curtailment",
    state: mapCurtailmentEventState(event.state),
    scopeLabel: getCurtailmentEventScopeLabel(event, options.siteNameById),
    sourceLabel: getSourceLabel(externalSource),
    isAutomationOwned: isAutomationExternalSource(externalSource),
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

export function mapCurtailmentHistoryEvent(
  event: ProtoCurtailmentEvent,
  options: CurtailmentMapperOptions = {},
): CurtailmentHistoryEvent {
  const externalSource = event.externalSource.trim();

  return {
    id: event.eventUuid,
    reason: event.reason || "Curtailment",
    state: mapCurtailmentEventState(event.state),
    priority: mapCurtailmentPriority(event.priority),
    scopeLabel: getCurtailmentEventScopeLabel(event, options.siteNameById),
    selectedMiners: getCurtailmentEventSelectedMinerCount(event),
    estimatedReductionKw: getCurtailmentEventEstimatedReductionKw(event),
    targetMetricsAvailable: hasCurtailmentTargetMetrics(event),
    targetKw: getFixedKwTarget(event),
    sourceLabel: getSourceLabel(externalSource),
    startedAt: timestampToIsoString(event.startedAt),
    endedAt: timestampToIsoString(event.endedAt),
    scheduledAt: timestampToIsoString(event.scheduledStartAt),
    createdAt: timestampToIsoString(event.createdAt),
  };
}

export function mapActiveCurtailmentHistoryEvent(
  event: ProtoCurtailmentEvent,
  options: CurtailmentMapperOptions = {},
): CurtailmentHistoryEvent {
  const historyEvent = mapCurtailmentHistoryEvent(event, options);

  if (!isActiveCurtailmentEventState(historyEvent.state)) {
    return historyEvent;
  }

  return {
    ...historyEvent,
    displayState: getActiveCurtailmentDisplayState(mapActiveCurtailmentEvent(event, options), {
      dispatchStartedAsCurtailing: true,
    }),
  };
}
