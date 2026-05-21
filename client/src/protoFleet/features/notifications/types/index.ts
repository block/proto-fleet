// Wire types for the notifications surface. Snake_case keys match the
// JSON shapes the server-side handler emits — see
// server/internal/handlers/notifications/handler.go for the source of truth.

export type ChannelKind = "webhook" | "smtp";
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
  // password is write-only — present only on requests, never on reads.
  password?: string;
}

export interface Channel {
  id: string;
  organization_id: string;
  name: string;
  kind: ChannelKind;
  webhook: WebhookConfig | null;
  smtp: SmtpConfig | null;
  created_at: string;
  updated_at: string;
  validated_at: string | null;
  validation_state: ValidationState;
  validation_error?: string;
  has_secret?: boolean;
}

// Rule — read-only descriptor of a provisioned Grafana alert rule.
// There is no client-side rule creation flow: the rule set is owned
// by the ops Grafana YAML and operators can only pause / resume /
// silence the rules that ship with the deploy.

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

// Silence — temporary mute that blocks a rule from delivering during a window.

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
