export type CurtailmentHealth = "connected" | "waitingForSignal" | "noSignal" | "offline";
export type AutomationTriggerType = "MQTT";

export const DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC = 240;
export const MAX_SOURCE_STALENESS_THRESHOLD_SEC = 24 * 60 * 60;
export const DEFAULT_POST_EVENT_COOLDOWN_SEC = 600;
export const MAX_POST_EVENT_COOLDOWN_SEC = 24 * 60 * 60;

export type CurtailmentSettings = {
  postEventCooldownSec: number;
};

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
export type ResponseProfileRestoreBehavior = "automaticBatchRestore" | "automaticImmediateRestore";

export type ResponseProfileFormValues = {
  name: string;
  actionType: ResponseProfileActionType;
  targetKw: string;
  deviceIdentifiers: string[];
  siteId: string;
  siteName: string;
  selectionStrategy: ResponseProfileSelectionStrategy;
  restoreBehavior: ResponseProfileRestoreBehavior;
  minDurationSec: string;
  maxDurationSec: string;
  curtailBatchSize: string;
  curtailBatchIntervalSec: string;
  restoreBatchSize: string;
  restoreIntervalSec: string;
  responseDeadlineMinutes: string;
  includeMaintenance: boolean;
};

export type ResponseProfile = {
  id: string;
  name: string;
  targetSummary: string;
  scope: string;
  selectionStrategy: string;
  restoreBehavior: string;
  deadlineSummary: string;
  formValues?: ResponseProfileFormValues;
};

export type AutomationConditionType = "mqttTriggerTargetOff" | "marketPriceAbove" | "hashpriceBelow" | "capacityAbove";

export type AutomationRuleFormValues = {
  name: string;
  sourceId: string;
  responseProfileId: string;
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
