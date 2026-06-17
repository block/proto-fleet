export type ChannelKind = "webhook" | "slack";
export type ValidationState = "ok" | "failed" | "pending";

export interface WebhookConfig {
  url: string;
  bearer_header: string | null;
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
