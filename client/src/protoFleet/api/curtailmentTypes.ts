import type { CurtailmentTerminalScopeType } from "@/protoFleet/api/curtailmentScopes";

export type CurtailmentEventState =
  "pending" | "active" | "restoring" | "completed" | "completedWithFailures" | "cancelled" | "failed";
export type ActiveCurtailmentEventState = Extract<CurtailmentEventState, "pending" | "active" | "restoring">;
export type CurtailmentTargetState =
  "pending" | "dispatched" | "confirmed" | "drifted" | "resolved" | "released" | "unavailable" | "restoreFailed";
export interface CurtailmentTargetRollup {
  state: CurtailmentTargetState;
  count: number;
}
export type ActiveCurtailmentDisplayState =
  "cancelled" | "curtailed" | "curtailing" | "failed" | "pending" | "restoreIncomplete" | "restored" | "restoring";

export type CurtailmentPriority = "normal" | "high" | "emergency";
export type EditableCurtailmentPriority = Exclude<CurtailmentPriority, "high">;
export type CurtailmentScopeType = CurtailmentTerminalScopeType;
export type CurtailmentSiteSelection = "none" | "allSites" | "site";
export type CurtailmentMinerSelectionMode = "subset" | "all";
export type ResponseProfileId = string;
export type CurtailmentMode = "fixedKwReduction" | "fullFleet";
export type MinerSelectionStrategy = "leastEfficientFirst";
export type CurtailmentStartModalMode = "create" | "edit";
export type CurtailmentStartModalVariant = "curtailment" | "responseProfile";
export type ResponseProfileModalMode = "create" | "edit";

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
  siteSelection?: CurtailmentSiteSelection;
  siteId?: string;
  siteIds?: string[];
  siteNamesById?: Record<string, string>;
  buildingTargetIds?: string[];
  rackTargetIds?: string[];
  groupTargetIds?: string[];
  deviceIdentifiers: string[];
  minerSelectionMode?: CurtailmentMinerSelectionMode;
  responseProfileId: ResponseProfileId;
  responseProfileRevision?: string;
  curtailmentMode: CurtailmentMode;
  minerSelectionStrategy: MinerSelectionStrategy;
  targetKw: string;
  toleranceKw: string;
  priority: EditableCurtailmentPriority;
  postEventCooldownSec?: string;
  minDurationSec: string;
  maxDurationSec: string;
  curtailBatchSize: string;
  curtailBatchIntervalSec: string;
  restoreBatchSize: string;
  restoreIntervalSec: string;
  facilityFanDeviceIds?: string[];
  fanOffDelaySec?: string;
  fanRestoreDelaySec?: string;
  reason: string;
  includeMaintenance: boolean;
  forceIncludeAllPairedMiners: boolean;
}

export type CurtailmentSubmitValues = CurtailmentFormValues;

export interface CurtailmentResponseProfileOption {
  id: ResponseProfileId;
  label: string;
  revision?: string;
  values: Partial<Omit<CurtailmentFormValues, "responseProfileId" | "responseProfileRevision">>;
}

export interface CurtailmentSiteOption {
  id: string;
  name: string;
}

export interface CurtailmentPlanPreview {
  selectedMinerCount: number;
  facilityFanDeviceCount?: number;
  unavailableMinerCount?: number;
  targetKw: number;
  estimatedReductionKw: number;
  curtailEstimate: string;
  restoreEstimate: string;
  scopeLabel: string;
}

export type CurtailmentFormErrors = Partial<Record<keyof CurtailmentFormValues, string>>;

export interface ActiveCurtailmentEvent {
  reason: string;
  state: CurtailmentEventState;
  scopeLabel: string;
  sourceLabel: string;
  isAutomationOwned: boolean;
  targetSiteCoverage?: ActiveCurtailmentTargetSiteCoverage;
  createdAt?: string;
  scheduledStartAt?: string;
  startedAt?: string;
  endedAt?: string;
  selectedMiners: number;
  estimatedReductionKw: number;
  targetKw?: number;
  hasTargetMetrics?: boolean;
  observedReductionKw: number;
  remainingPowerKw?: number;
  facilityFanDeviceCount?: number;
  fanOffSentAt?: string;
  fanOnSentAt?: string;
  fanAirflowReopenedAt?: string;
  fanLastError?: string;
  // Curtail dispatch pacing for the rough time-to-curtail estimate; absent
  // when the event has no explicit batch size (reconciler-side defaults).
  curtailBatchSize?: number;
  curtailBatchIntervalSec?: number;
  // Configured restore wave size; 0 means "up to the safety limit" per wave,
  // matching the reconciler's restore claim sizing.
  restoreBatchSize: number;
  restoreBatchIntervalSec: number;
  rollups: CurtailmentTargetRollup[];
  unavailableReasonCounts?: ActiveCurtailmentUnavailableReasonCount[];
}

export interface ActiveCurtailmentTargetSiteCoverage {
  complete: boolean;
  targetCount: number;
  mappedTargetCount: number;
  unknownTargetCount: number;
}

export interface ActiveCurtailmentUnavailableReasonCount {
  label: string;
  count: number;
}

export interface CurtailmentHistoryEvent {
  id: string;
  reason: string;
  state: CurtailmentEventState;
  displayState?: ActiveCurtailmentDisplayState;
  injectedActive?: boolean;
  priority: CurtailmentPriority;
  scopeLabel: string;
  selectedMiners: number;
  facilityFanDeviceCount?: number;
  estimatedReductionKw: number;
  targetMetricsAvailable: boolean;
  // Distinct from targetMetricsAvailable: live rollups prove counts without
  // proving a kW estimate (active-list rows scrub the decision snapshot).
  // Optional so fixture-driven surfaces default to the count signal.
  estimatedReductionAvailable?: boolean;
  targetKw?: number;
  sourceLabel: string;
  startedAt?: string;
  endedAt?: string;
  scheduledAt?: string;
  createdAt?: string;
}

export type CurtailmentHealth = "connected" | "waitingForSignal" | "noSignal" | "offline";
export type AutomationTriggerType = "MQTT";

export const DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC = 240;
export const MAX_SOURCE_STALENESS_THRESHOLD_SEC = 24 * 60 * 60;

export type CurtailmentSource = {
  id: string;
  name: string;
  triggerType: AutomationTriggerType;
  brokerHosts: string[];
  brokerPrimaryHost?: string;
  brokerSecondaryHost?: string;
  port: number;
  topic: string;
  protocol: string;
  qos: number;
  username: string;
  hasPassword?: boolean;
  lastTarget: string;
  lastSeen: string;
  health: CurtailmentHealth;
  enabled: boolean;
  stalenessThresholdSec: number;
};

export type CurtailmentSourceFormValues = {
  name: string;
  brokerPrimaryHost: string;
  brokerSecondaryHost: string;
  brokerPort: string;
  topic: string;
  username: string;
  password: string;
  stalenessThresholdSec: string;
};

export type ResponseProfileActionType = "fullFleet" | "fixedKwReduction";
export type ResponseProfileSelectionStrategy = "leastEfficientFirst";
export type ResponseProfileMinerSelectionMode = "subset" | "all";
export type ResponseProfileRestoreBehavior = "automaticBatchRestore" | "automaticImmediateRestore";
export type ResponseProfileSiteSelection = "none" | "allSites" | "site";
export type ResponseProfileScopeType = CurtailmentTerminalScopeType;

export function isResponseProfileAutomationReady(scopeType: ResponseProfileScopeType | undefined): boolean {
  return scopeType !== undefined;
}

export type ResponseProfileFormValues = {
  name: string;
  actionType: ResponseProfileActionType;
  targetKw: string;
  toleranceKw: string;
  priority: EditableCurtailmentPriority;
  postEventCooldownSec: string;
  scopeType: ResponseProfileScopeType;
  buildingTargetIds: string[];
  rackTargetIds: string[];
  groupTargetIds: string[];
  deviceIdentifiers: string[];
  minerSelectionMode?: ResponseProfileMinerSelectionMode;
  siteSelection?: ResponseProfileSiteSelection;
  siteId: string;
  siteName: string;
  siteIds?: string[];
  siteNamesById?: Record<string, string>;
  selectionStrategy: ResponseProfileSelectionStrategy;
  restoreBehavior: ResponseProfileRestoreBehavior;
  minDurationSec: string;
  maxDurationSec: string;
  curtailBatchSize: string;
  curtailBatchIntervalSec: string;
  restoreBatchSize: string;
  restoreIntervalSec: string;
  facilityFanDeviceIds?: string[];
  fanOffDelaySec?: string;
  fanRestoreDelaySec?: string;
  responseDeadlineMinutes: string;
  includeMaintenance: boolean;
  forceIncludeAllPairedMiners?: boolean;
};

export type ResponseProfile = {
  id: string;
  revision?: string;
  name: string;
  targetSummary: string;
  scope: string;
  selectionStrategy: string;
  restoreBehavior: string;
  formValues?: ResponseProfileFormValues;
  isReadOnly?: boolean;
  isAutomationReady: boolean;
};

export type EditableResponseProfile = ResponseProfile & { formValues: ResponseProfileFormValues };

export function isEditableResponseProfile(profile: ResponseProfile): profile is EditableResponseProfile {
  return !profile.isReadOnly && profile.formValues !== undefined;
}

export type AutomationConditionType = "mqttTriggerTargetOff" | "marketPriceAbove" | "hashpriceBelow" | "capacityAbove";

export type AutomationRuleFormValues = {
  name: string;
  sourceId: string;
  responseProfileId: string;
  responseProfileRevision: string;
};

export type AutomationRule = {
  id: string;
  priority: number;
  name: string;
  conditionType: AutomationConditionType;
  conditionSummary: string;
  sourceId?: string;
  responseProfileId: string;
  responseProfileName?: string;
  enabled: boolean;
};
