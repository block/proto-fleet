import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";

import { notificationChannelClient } from "@/protoFleet/api/clients";
import {
  type Channel as ProtoChannel,
  ChannelKind as ProtoChannelKind,
  ValidationState as ProtoValidationState,
} from "@/protoFleet/api/generated/notifications/v1/notifications_pb";
import type {
  Channel,
  ChannelKind,
  SlackConfig,
  SmtpConfig,
  ValidationState,
  WebhookConfig,
} from "@/protoFleet/features/notifications/types";

const isoFromTs = (ts?: Timestamp): string => (ts ? timestampDate(ts).toISOString() : "");
const isoOrNull = (ts?: Timestamp): string | null => (ts ? timestampDate(ts).toISOString() : null);

function required<T>(value: T | undefined, name: string): T {
  if (value == null) {
    throw new Error(`notifications: response missing ${name}`);
  }
  return value;
}

const channelKindToProto = (k: ChannelKind): ProtoChannelKind => {
  switch (k) {
    case "webhook":
      return ProtoChannelKind.WEBHOOK;
    case "smtp":
      return ProtoChannelKind.SMTP;
    case "slack":
      return ProtoChannelKind.SLACK;
  }
};

const channelKindFromProto = (k: ProtoChannelKind): ChannelKind => {
  switch (k) {
    case ProtoChannelKind.SMTP:
      return "smtp";
    case ProtoChannelKind.SLACK:
      return "slack";
    default:
      return "webhook";
  }
};

const validationStateFromProto = (s: ProtoValidationState): ValidationState => {
  switch (s) {
    case ProtoValidationState.OK:
      return "ok";
    case ProtoValidationState.FAILED:
      return "failed";
    default:
      return "pending";
  }
};
const channelFromProto = (c: ProtoChannel): Channel => ({
  id: c.id,
  organization_id: String(c.organizationId),
  name: c.name,
  kind: channelKindFromProto(c.kind),
  webhook: c.webhook ? { url: c.webhook.url, bearer_header: null } : null,
  smtp: c.smtp
    ? { host: c.smtp.host, port: c.smtp.port, username: c.smtp.username, from: c.smtp.from, to: c.smtp.to }
    : null,
  slack: c.slack ? {} : null,
  created_at: isoFromTs(c.createdAt),
  updated_at: isoFromTs(c.updatedAt),
  validated_at: isoOrNull(c.validatedAt),
  validation_state: validationStateFromProto(c.validationState),
  validation_error: c.validationError,
  has_secret: c.hasSecret,
});
const webhookToProto = (w?: WebhookConfig | null) =>
  w ? { url: w.url, bearerHeader: w.bearer_header ?? "" } : undefined;

const smtpToProto = (s?: SmtpConfig | null) =>
  s
    ? { host: s.host, port: s.port, username: s.username, from: s.from, to: s.to, password: s.password ?? "" }
    : undefined;

const slackToProto = (s?: SlackConfig | null) => (s ? { webhookUrl: s.webhook_url ?? "" } : undefined);
const channelDestinationFields = (input: ChannelMutationInput) => ({
  kind: channelKindToProto(input.kind),
  webhook: webhookToProto(input.webhook),
  smtp: smtpToProto(input.smtp),
  slack: slackToProto(input.slack),
});

export async function listChannels(): Promise<Channel[]> {
  const res = await notificationChannelClient.listChannels({});
  return res.channels.map(channelFromProto);
}

export interface ChannelMutationInput {
  id?: string;
  name: string;
  kind: ChannelKind;
  webhook?: WebhookConfig | null;
  smtp?: SmtpConfig | null;
  slack?: SlackConfig | null;
}

export async function createChannel(input: ChannelMutationInput): Promise<Channel> {
  const res = await notificationChannelClient.createChannel({
    name: input.name,
    ...channelDestinationFields(input),
  });
  return channelFromProto(required(res.channel, "channel"));
}

export async function updateChannel(input: ChannelMutationInput & { id: string }): Promise<Channel> {
  const res = await notificationChannelClient.updateChannel({
    id: input.id,
    name: input.name,
    ...channelDestinationFields(input),
  });
  return channelFromProto(required(res.channel, "channel"));
}

export async function deleteChannel(id: string): Promise<void> {
  await notificationChannelClient.deleteChannel({ id });
}

export interface TestChannelResult {
  ok: boolean;
  error: string;
  response_code: number;
}

export async function testChannel(input: ChannelMutationInput): Promise<TestChannelResult> {
  const res = await notificationChannelClient.testChannel({
    id: input.id ?? "",
    ...channelDestinationFields(input),
  });
  return { ok: res.ok, error: res.error, response_code: res.responseCode };
}
