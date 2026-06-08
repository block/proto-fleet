export type CurtailmentHealth = "connected" | "stale" | "offline";
export type AutomationTriggerType = "MQTT";

export type CurtailmentSource = {
  id: string;
  name: string;
  triggerType: AutomationTriggerType;
  site: string;
  brokerHosts: string[];
  port: number;
  topic: string;
  protocol: string;
  qos: number;
  username: string;
  scope: string;
  curtailmentMode: string;
  lastTarget: 0 | 100;
  lastSeen: string;
  health: CurtailmentHealth;
  enabled: boolean;
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
