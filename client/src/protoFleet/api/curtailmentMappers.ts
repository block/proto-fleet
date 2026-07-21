import type { Timestamp } from "@bufbuild/protobuf/wkt";

import {
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getSiteDisplayName, type SiteNameById } from "@/protoFleet/api/siteNames";
import type {
  ActiveCurtailmentEvent,
  ActiveCurtailmentTargetSiteCoverage,
  ActiveCurtailmentUnavailableReasonCount,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  getActiveCurtailmentDisplayState,
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventLiveTargetCount,
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
const unavailableReasonLabels: Record<string, string> = {
  active_event: "in another event",
  authentication_needed: "needs authentication",
  below_candidate_min_power_w: "below power threshold",
  cooldown: "in cooldown",
  curtail_full_unsupported: "full curtailment unsupported",
  maintenance: "in maintenance",
  missing_status: "missing status",
  non_actionable_status: "not ready",
  offline: "offline",
  pairing: "not paired",
  phantom_load_no_hash: "not hashing",
  power_telemetry_unreliable: "unreliable power telemetry",
  reboot_required: "reboot required",
  stale_telemetry: "stale telemetry",
  unreachable_residual_load: "offline",
  unknown: "reason unknown",
  updating: "updating",
};

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
    facilityFanDeviceIds: event.facilityFanDeviceIds.map((id) => id.toString()),
    fanOffDelaySec: formatNonNegativeNumberField(event.fanOffDelaySec),
    fanRestoreDelaySec: formatNonNegativeNumberField(event.fanRestoreDelaySec),
    reason: event.reason || "Curtailment",
    includeMaintenance: event.includeMaintenance,
    forceIncludeAllPairedMiners: event.forceIncludeAllPairedMiners,
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

function mapTargetSiteCoverage(event: ProtoCurtailmentEvent): ActiveCurtailmentTargetSiteCoverage | undefined {
  const coverage = event.targetSiteCoverage;
  if (!coverage) {
    return undefined;
  }

  return {
    complete: coverage.complete,
    targetCount: coverage.targetCount,
    mappedTargetCount: coverage.mappedTargetCount,
    unknownTargetCount: coverage.unknownTargetCount,
  };
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

// A live rollup proves target counts but not estimated kW: active-list rows
// carry rollups while their decision snapshot stays scrubbed, so kW estimates
// need a snapshot number or hydrated target baselines. Target rows alone are
// not enough — baseline_power_w is optional (telemetry gaps at selection), so
// baseline-less targets would sum to a fabricated 0.0 kW estimate.
export function hasCurtailmentEstimatedReductionKw(event: ProtoCurtailmentEvent): boolean {
  return (
    hasSnapshotNumber(event, estimatedReductionKwSnapshotKeys) ||
    event.targets.some((target) => target.baselinePowerW !== undefined)
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

function getUnavailableReasonLabel(reason: string): string {
  const normalizedReason = reason.trim().toLowerCase();
  if (!normalizedReason) {
    return "reason unknown";
  }

  return unavailableReasonLabels[normalizedReason] ?? normalizedReason.replace(/[_-]+/g, " ");
}

function getUnavailableReasonCounts(
  event: ProtoCurtailmentEvent,
): ActiveCurtailmentUnavailableReasonCount[] | undefined {
  const unavailableReasons = event.targetRollup?.unavailableReasons;
  if (!unavailableReasons?.length) {
    return undefined;
  }

  return unavailableReasons
    .filter((reason) => reason.count > 0)
    .map(({ reason, count }) => ({
      label: getUnavailableReasonLabel(reason),
      count,
    }))
    .sort((left, right) => right.count - left.count || left.label.localeCompare(right.label));
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
    targetSiteCoverage: mapTargetSiteCoverage(event),
    createdAt: timestampToIsoString(event.createdAt),
    scheduledStartAt: timestampToIsoString(event.scheduledStartAt),
    startedAt: timestampToIsoString(event.startedAt),
    endedAt: timestampToIsoString(event.endedAt),
    selectedMiners: getCurtailmentEventLiveTargetCount(event),
    estimatedReductionKw,
    targetKw: getFixedKwTarget(event),
    observedReductionKw: observedPowerSummary.observedReductionKw,
    remainingPowerKw: observedPowerSummary.remainingPowerKw,
    facilityFanDeviceCount: event.facilityFanDeviceIds.length,
    fanOffSentAt: timestampToIsoString(event.fanOffSentAt),
    fanOnSentAt: timestampToIsoString(event.fanOnSentAt),
    fanLastError: event.fanLastError,
    curtailBatchSize: event.curtailBatchSize,
    curtailBatchIntervalSec: event.curtailBatchIntervalSec,
    // Restore displays follow the configured restore_batch_size, which is
    // what the reconciler's restore claims obey (0 = up to the safety limit
    // per wave). effective_batch_size is a start-time stamp of the selected
    // count; all-paired and closed-loop growth leaves it stale, so it must
    // not masquerade as the restore wave size.
    restoreBatchSize: event.restoreBatchSize,
    restoreBatchIntervalSec: event.restoreBatchIntervalSec,
    rollups: getCurtailmentTargetRollups(event),
    unavailableReasonCounts: getUnavailableReasonCounts(event),
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
    facilityFanDeviceCount: event.facilityFanDeviceIds.length,
    estimatedReductionKw: getCurtailmentEventEstimatedReductionKw(event),
    targetMetricsAvailable: hasCurtailmentTargetMetrics(event),
    estimatedReductionAvailable: hasCurtailmentEstimatedReductionKw(event),
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

  // Injected active rows represent live events, so they share the active
  // card's live target count instead of the snapshot count.
  const activeEvent = mapActiveCurtailmentEvent(event, options);
  return {
    ...historyEvent,
    selectedMiners: activeEvent.selectedMiners,
    displayState: getActiveCurtailmentDisplayState(activeEvent, {
      dispatchStartedAsCurtailing: true,
    }),
  };
}
