export type CurtailmentHealth = "connected" | "waitingForSignal" | "noSignal" | "offline";
export type AutomationTriggerType = "MQTT";

export type CurtailmentSource = {
  id: string;
  name: string;
  triggerType: AutomationTriggerType;
  site: string;
  brokerHosts: string[];
  brokerPrimaryHost?: string;
  brokerSecondaryHost?: string;
  port: number;
  topic: string;
  protocol: string;
  qos: number;
  username: string;
  hasPassword?: boolean;
  scope: string;
  curtailmentMode: string;
  lastTarget: string;
  lastSeen: string;
  health: CurtailmentHealth;
  enabled: boolean;
};

export type CurtailmentSourceFormValues = {
  name: string;
  brokerPrimaryHost: string;
  brokerSecondaryHost: string;
  brokerPort: string;
  topic: string;
  username: string;
  password: string;
};

export type ResponseProfile = {
  id: string;
  name: string;
  targetSummary: string;
  scope: string;
  selectionStrategy: string;
  restoreBehavior: string;
  deadlineSummary: string;
};

export type AutomationConditionType = "mqttTriggerTargetOff" | "marketPriceAbove" | "hashpriceBelow" | "capacityAbove";

export type AutomationRule = {
  id: string;
  priority: number;
  name: string;
  conditionType: AutomationConditionType;
  conditionSummary: string;
  sourceId?: string;
  responseProfileId: string;
  enabled: boolean;
};
