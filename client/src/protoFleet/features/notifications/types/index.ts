// Wire types for the notifications surface; snake_case keys match server/internal/handlers/notifications/handler.go.
export type ChannelKind = "webhook" | "smtp" | "slack";
export type ValidationState = "ok" | "failed" | "pending";

export interface WebhookConfig {
  url: string;
  bearer_header: string | null;
}

export interface SmtpConfig {
  host: string;
  port: number;
  username: string;
  from: string;
  to: string[];
  // Write-only: present on requests, never on reads.
  password?: string;
}

export interface SlackConfig {
  // Write-only: the URL embeds a capability token, so reads return it empty and has_secret signals one is stored.
  webhook_url?: string;
}

export interface Channel {
  id: string;
  organization_id: string;
  name: string;
  kind: ChannelKind;
  webhook: WebhookConfig | null;
  smtp: SmtpConfig | null;
  slack: SlackConfig | null;
  created_at: string;
  updated_at: string;
  validated_at: string | null;
  validation_state: ValidationState;
  validation_error?: string;
  has_secret?: boolean;
}

// Read-only descriptor of a provisioned Grafana alert rule; the ops YAML owns the set, clients only pause/resume/silence.
export type RuleTemplate = "offline" | "temperature" | "hashrate" | "pool" | "command_failure" | "telemetry-poll" | "";

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
}

export type SilenceScopeKind = "rule" | "group" | "site" | "device";

export interface SilenceScope {
  kind: SilenceScopeKind;
  rule_id: string | null;
  group_id: string | null;
  site_id: string | null;
  device_ids: string[];
}

export interface Silence {
  id: string;
  organization_id: string;
  scope: SilenceScope;
  starts_at: string;
  ends_at: string | null;
  comment: string;
  created_by: string;
  created_at: string;
}

export interface SilenceWithActive extends Silence {
  active: boolean;
}

export type NotificationHistoryStatus = "firing" | "resolved";

export interface NotificationHistoryEntry {
  id: string;
  received_at: string;
  alert_name: string;
  status: NotificationHistoryStatus;
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
