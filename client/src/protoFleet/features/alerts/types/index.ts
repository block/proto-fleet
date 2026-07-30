export type ChannelKind = "webhook" | "slack";
export type ValidationState = "ok" | "failed" | "pending";

export interface WebhookConfig {
  url: string;
  bearer_header: string | null;
  // Write-only: on update, revoke the stored bearer even when the URL is unchanged (an empty bearer_header alone means "keep").
  clear_bearer_header?: boolean;
}

export interface SlackConfig {
  // Write-only: reads return empty since the URL embeds a capability token; has_secret signals one is stored.
  webhook_url?: string;
}

export interface Channel {
  id: string;
  organization_id: string;
  name: string;
  kind: ChannelKind;
  webhook: WebhookConfig | null;
  slack: SlackConfig | null;
  created_at: string;
  updated_at: string;
  validated_at: string | null;
  validation_state: ValidationState;
  validation_error?: string;
  has_secret?: boolean;
}

export type RuleTemplate =
  | "offline"
  | "temperature"
  | "hashrate"
  | "pool"
  | "command_failure"
  | "telemetry-poll"
  | "mqtt-curtailment"
  | "mqtt-disconnected"
  | "";

// Origin decides mutability: only user rules can be edited or deleted.
export type RuleOrigin = "provisioned" | "user";

export type HashrateMode = "pct_expected" | "absolute";
export type HashrateUnit = "TH" | "PH";

export interface HashrateRuleConfig {
  mode: HashrateMode;
  // Percent of expected in (0, 100] for pct_expected; hashrate in `unit` for absolute.
  value: number;
  unit?: HashrateUnit;
}

export interface TemperatureRuleConfig {
  max_celsius: number;
}

// Exactly one of offline/hashrate/temperature is set.
export interface RuleConfig {
  name: string;
  duration_seconds: number;
  offline?: Record<string, never>;
  hashrate?: HashrateRuleConfig;
  temperature?: TemperatureRuleConfig;
}

// Where a rule's firing alerts deliver: every org channel, only the listed ones, or nowhere (in-app history only).
export type RoutingMode = "default" | "custom" | "none";

export interface RuleRouting {
  mode: RoutingMode;
  // Non-empty only for custom.
  channel_ids: string[];
}

export interface Rule {
  id: string;
  organization_id: string;
  name: string;
  template: RuleTemplate;
  group: string;
  severity: string;
  summary: string;
  description: string;
  duration_seconds: number;
  enabled: boolean;
  origin: RuleOrigin;
  // Null for provisioned rules.
  config: RuleConfig | null;
  // Null when the server couldn't read routing; keep the last-known value instead of treating it as default.
  routing: RuleRouting | null;
}

export type MaintenanceWindowScopeKind = "rule" | "group" | "site" | "device";

export interface MaintenanceWindowScope {
  kind: MaintenanceWindowScopeKind;
  rule_id: string | null;
  group_id: string | null;
  site_id: string | null;
  device_ids: string[];
}

export interface MaintenanceWindow {
  id: string;
  organization_id: string;
  scope: MaintenanceWindowScope;
  starts_at: string;
  ends_at: string | null;
  comment: string;
  created_by: string;
  created_at: string;
}

export interface MaintenanceWindowWithActive extends MaintenanceWindow {
  active: boolean;
}

export type AlertHistoryStatus = "firing" | "resolved";

export interface AlertHistoryEntry {
  id: string;
  received_at: string;
  alert_name: string;
  status: AlertHistoryStatus;
  severity: string;
  rule_group: string;
  fingerprint: string;
  device_id: string;
  device_name: string;
  device_mac: string;
  template: string;
  summary: string;
  starts_at: string | null;
  ends_at: string | null;
}
