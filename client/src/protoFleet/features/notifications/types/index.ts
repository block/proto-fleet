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
  // Write-only: reads return empty since the URL embeds a capability token; has_secret signals one is stored.
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
