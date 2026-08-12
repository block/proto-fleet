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

// Which miners a rule fires for: the union of every listed placement (current membership) and the explicit device_ids. Absent/empty means the whole org.
export interface RuleScope {
  site_ids: string[];
  device_ids: string[];
  building_ids: string[];
  rack_ids: string[];
  group_ids: string[];
  // Live "any site": any current or future site (excludes unassigned miners); supersedes site_ids.
  all_sites: boolean;
  // Server-set: this rule has device_ids but the caller lacks miner:read; render as a restricted subset, never as org-wide.
  device_ids_redacted?: boolean;
}

// Exactly one of offline/hashrate/temperature is set.
export interface RuleConfig {
  name: string;
  duration_seconds: number;
  offline?: Record<string, never>;
  hashrate?: HashrateRuleConfig;
  temperature?: TemperatureRuleConfig;
  scope?: RuleScope;
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
  // Server-set: the stored config no longer matches the SQL the rule evaluates (interrupted save); re-saving converges.
  config_out_of_sync?: boolean;
  // Server-set on mutation responses whose config read failed: keep the last-known config (like null routing) instead of reading the absent config as "not editable".
  config_unknown?: boolean;
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

// One currently-firing alert and its blast radius. Rule identity and counts only: severity, summary and the
// rest of the per-instance detail come back from the drill-in, which reports them per affected miner.
export interface ActiveAlertGroup {
  // Display title, which retired-rule mapping may rewrite; stored_alert_name is what the server filters on.
  alert_name: string;
  stored_alert_name: string;
  rule_group: string;
  // The (rule group, alert name) pair the server groups on, as one string: React key and drill-in identity.
  key: string;
  // 0 for fleet-wide and source-scoped alerts, which carry no device: the shape the views render is the
  // server's to state, so no client-side rule-group list has to track the scopes a rule can fire on.
  device_count: number;
  // Firing instances, which exceeds device_count only for a rule firing on a non-device dimension (per MQTT source, say).
  alert_count: number;
  first_started_at: string;
}

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
