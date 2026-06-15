// Single translation point between the notifications.v1 Connect/protobuf contract and the feature's snake_case view model.
import { type Timestamp, timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  notificationChannelClient,
  notificationHistoryClient,
  notificationRuleClient,
  notificationSilenceClient,
} from "@/protoFleet/api/clients";
import {
  type Channel as ProtoChannel,
  ChannelKind as ProtoChannelKind,
  type NotificationHistoryEntry as ProtoHistoryEntry,
  type Rule as ProtoRule,
  RuleTemplate as ProtoRuleTemplate,
  type Silence as ProtoSilence,
  SilenceScopeKind as ProtoSilenceScopeKind,
  ValidationState as ProtoValidationState,
} from "@/protoFleet/api/generated/notifications/v1/notifications_pb";
import type {
  Channel,
  ChannelKind,
  NotificationHistoryEntry,
  NotificationHistoryStatus,
  Rule,
  RuleTemplate,
  Silence,
  SilenceScope,
  SilenceScopeKind,
  SlackConfig,
  SmtpConfig,
  ValidationState,
  WebhookConfig,
} from "@/protoFleet/features/notifications/types";

const isoFromTs = (ts?: Timestamp): string => (ts ? timestampDate(ts).toISOString() : "");
const isoOrNull = (ts?: Timestamp): string | null => (ts ? timestampDate(ts).toISOString() : null);
const tsFromIso = (iso: string): Timestamp => timestampFromDate(new Date(iso));

// Unwraps a field the server always sets but the generated type marks optional; a missing value is a server bug.
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

const ruleTemplateFromProto = (t: ProtoRuleTemplate): RuleTemplate => {
  switch (t) {
    case ProtoRuleTemplate.OFFLINE:
      return "offline";
    case ProtoRuleTemplate.HASHRATE:
      return "hashrate";
    case ProtoRuleTemplate.TEMPERATURE:
      return "temperature";
    case ProtoRuleTemplate.POOL:
      return "pool";
    case ProtoRuleTemplate.COMMAND_FAILURE:
      return "command_failure";
    case ProtoRuleTemplate.TELEMETRY_POLL:
      return "telemetry-poll";
    default:
      return "";
  }
};

const scopeKindToProto = (k: SilenceScopeKind): ProtoSilenceScopeKind => {
  switch (k) {
    case "rule":
      return ProtoSilenceScopeKind.RULE;
    case "group":
      return ProtoSilenceScopeKind.GROUP;
    case "site":
      return ProtoSilenceScopeKind.SITE;
    case "device":
      return ProtoSilenceScopeKind.DEVICE;
  }
};

const scopeKindFromProto = (k: ProtoSilenceScopeKind): SilenceScopeKind => {
  switch (k) {
    case ProtoSilenceScopeKind.GROUP:
      return "group";
    case ProtoSilenceScopeKind.SITE:
      return "site";
    case ProtoSilenceScopeKind.DEVICE:
      return "device";
    default:
      return "rule";
  }
};

const channelFromProto = (c: ProtoChannel): Channel => ({
  id: c.id,
  organization_id: String(c.organizationId),
  name: c.name,
  kind: channelKindFromProto(c.kind),
  // Reads never carry secrets; emptiness is signalled by has_secret.
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

const ruleFromProto = (r: ProtoRule): Rule => ({
  id: r.id,
  organization_id: String(r.organizationId),
  name: r.name,
  template: ruleTemplateFromProto(r.template),
  group: r.group,
  severity: r.severity,
  summary: r.summary,
  description: r.description,
  duration_seconds: r.durationSeconds,
  enabled: r.enabled,
});

const silenceFromProto = (s: ProtoSilence): Silence => ({
  id: s.id,
  organization_id: String(s.organizationId),
  scope: {
    kind: s.scope ? scopeKindFromProto(s.scope.kind) : "rule",
    rule_id: s.scope?.ruleId || null,
    group_id: s.scope?.groupId || null,
    site_id: s.scope?.siteId || null,
    device_ids: s.scope?.deviceIds ?? [],
  },
  starts_at: isoFromTs(s.startsAt),
  ends_at: isoOrNull(s.endsAt),
  comment: s.comment,
  created_by: s.createdBy,
  created_at: isoFromTs(s.createdAt),
});

const historyFromProto = (n: ProtoHistoryEntry): NotificationHistoryEntry => ({
  id: n.id,
  received_at: isoFromTs(n.receivedAt),
  alert_name: n.alertName,
  status: n.status as NotificationHistoryStatus,
  severity: n.severity,
  rule_group: n.ruleGroup,
  fingerprint: n.fingerprint,
  device_id: n.deviceId,
  device_name: n.deviceName,
  device_mac: n.deviceMac,
  template: n.template,
  summary: n.summary,
  starts_at: isoOrNull(n.startsAt),
  ends_at: isoOrNull(n.endsAt),
});

const webhookToProto = (w?: WebhookConfig | null) =>
  w ? { url: w.url, bearerHeader: w.bearer_header ?? "" } : undefined;

const smtpToProto = (s?: SmtpConfig | null) =>
  s
    ? { host: s.host, port: s.port, username: s.username, from: s.from, to: s.to, password: s.password ?? "" }
    : undefined;

const slackToProto = (s?: SlackConfig | null) => (s ? { webhookUrl: s.webhook_url ?? "" } : undefined);

const scopeToProto = (s: SilenceScope) => ({
  kind: scopeKindToProto(s.kind),
  ruleId: s.rule_id ?? "",
  groupId: s.group_id ?? "",
  siteId: s.site_id ?? "",
  deviceIds: s.device_ids,
});

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

export async function listRules(): Promise<Rule[]> {
  const res = await notificationRuleClient.listRules({});
  return res.rules.map(ruleFromProto);
}

export async function pauseRule(id: string): Promise<Rule> {
  const res = await notificationRuleClient.pauseRule({ id });
  return ruleFromProto(required(res.rule, "rule"));
}

export async function resumeRule(id: string): Promise<Rule> {
  const res = await notificationRuleClient.resumeRule({ id });
  return ruleFromProto(required(res.rule, "rule"));
}

export async function listSilences(): Promise<Silence[]> {
  const res = await notificationSilenceClient.listSilences({});
  return res.silences.map(silenceFromProto);
}

export interface SilenceMutationInput {
  id?: string;
  scope: SilenceScope;
  starts_at: string;
  ends_at: string | null;
  comment: string;
}

export async function createSilence(input: SilenceMutationInput): Promise<Silence> {
  const res = await notificationSilenceClient.createSilence({
    scope: scopeToProto(input.scope),
    startsAt: tsFromIso(input.starts_at),
    endsAt: input.ends_at ? tsFromIso(input.ends_at) : undefined,
    comment: input.comment,
  });
  return silenceFromProto(required(res.silence, "silence"));
}

export async function updateSilence(input: SilenceMutationInput & { id: string }): Promise<Silence> {
  const res = await notificationSilenceClient.updateSilence({
    id: input.id,
    scope: scopeToProto(input.scope),
    startsAt: tsFromIso(input.starts_at),
    endsAt: input.ends_at ? tsFromIso(input.ends_at) : undefined,
    comment: input.comment,
  });
  return silenceFromProto(required(res.silence, "silence"));
}

export async function deleteSilence(id: string): Promise<void> {
  await notificationSilenceClient.deleteSilence({ id });
}

export interface HistoryPage {
  notifications: NotificationHistoryEntry[];
  has_more: boolean;
}

export async function listHistory(input: { before_id?: string; page_size?: number }): Promise<HistoryPage> {
  const res = await notificationHistoryClient.listNotifications({
    beforeId: input.before_id ?? "",
    pageSize: input.page_size ?? 0,
  });
  return { notifications: res.notifications.map(historyFromProto), has_more: res.hasMore };
}
