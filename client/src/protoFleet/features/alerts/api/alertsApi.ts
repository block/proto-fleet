import { create } from "@bufbuild/protobuf";
import { type Timestamp, timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  alertChannelClient,
  alertHistoryClient,
  alertMaintenanceWindowClient,
  alertRuleClient,
} from "@/protoFleet/api/clients";
import {
  type Channel as ProtoChannel,
  ChannelKind as ProtoChannelKind,
  HashrateMode as ProtoHashrateMode,
  HashrateUnit as ProtoHashrateUnit,
  type AlertHistoryEntry as ProtoHistoryEntry,
  type MaintenanceWindow as ProtoMaintenanceWindow,
  MaintenanceWindowScopeKind as ProtoMaintenanceWindowScopeKind,
  type Rule as ProtoRule,
  type RuleConfig as ProtoRuleConfig,
  RuleConfigSchema as ProtoRuleConfigSchema,
  RuleOrigin as ProtoRuleOrigin,
  RuleTemplate as ProtoRuleTemplate,
  ValidationState as ProtoValidationState,
} from "@/protoFleet/api/generated/alerts/v1/alerts_pb";
import type {
  AlertHistoryEntry,
  AlertHistoryStatus,
  Channel,
  ChannelKind,
  HashrateMode,
  HashrateUnit,
  MaintenanceWindow,
  MaintenanceWindowScope,
  MaintenanceWindowScopeKind,
  Rule,
  RuleConfig,
  RuleTemplate,
  SlackConfig,
  ValidationState,
  WebhookConfig,
} from "@/protoFleet/features/alerts/types";

const isoFromTs = (ts?: Timestamp): string => (ts ? timestampDate(ts).toISOString() : "");
const isoOrNull = (ts?: Timestamp): string | null => (ts ? timestampDate(ts).toISOString() : null);
const tsFromIso = (iso: string): Timestamp => timestampFromDate(new Date(iso));

function required<T>(value: T | undefined, name: string): T {
  if (value == null) {
    throw new Error(`alerts: response missing ${name}`);
  }
  return value;
}

const channelKindToProto = (k: ChannelKind): ProtoChannelKind => {
  switch (k) {
    case "webhook":
      return ProtoChannelKind.WEBHOOK;
    case "slack":
      return ProtoChannelKind.SLACK;
  }
};

const channelKindFromProto = (k: ProtoChannelKind): ChannelKind => {
  switch (k) {
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
    case ProtoRuleTemplate.MQTT_CURTAILMENT:
      return "mqtt-curtailment";
    case ProtoRuleTemplate.MQTT_DISCONNECTED:
      return "mqtt-disconnected";
    default:
      return "";
  }
};

const scopeKindToProto = (k: MaintenanceWindowScopeKind): ProtoMaintenanceWindowScopeKind => {
  switch (k) {
    case "rule":
      return ProtoMaintenanceWindowScopeKind.RULE;
    case "group":
      return ProtoMaintenanceWindowScopeKind.GROUP;
    case "site":
      return ProtoMaintenanceWindowScopeKind.SITE;
    case "device":
      return ProtoMaintenanceWindowScopeKind.DEVICE;
  }
};

const scopeKindFromProto = (k: ProtoMaintenanceWindowScopeKind): MaintenanceWindowScopeKind => {
  switch (k) {
    case ProtoMaintenanceWindowScopeKind.GROUP:
      return "group";
    case ProtoMaintenanceWindowScopeKind.SITE:
      return "site";
    case ProtoMaintenanceWindowScopeKind.DEVICE:
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
  webhook: c.webhook ? { url: c.webhook.url, bearer_header: null } : null,
  slack: c.slack ? {} : null,
  created_at: isoFromTs(c.createdAt),
  updated_at: isoFromTs(c.updatedAt),
  validated_at: isoOrNull(c.validatedAt),
  validation_state: validationStateFromProto(c.validationState),
  validation_error: c.validationError,
  has_secret: c.hasSecret,
});

const hashrateModeFromProto = (m: ProtoHashrateMode): HashrateMode =>
  m === ProtoHashrateMode.ABSOLUTE ? "absolute" : "pct_expected";

const hashrateUnitFromProto = (u: ProtoHashrateUnit): HashrateUnit | undefined => {
  switch (u) {
    case ProtoHashrateUnit.TERAHASH:
      return "TH";
    case ProtoHashrateUnit.PETAHASH:
      return "PH";
    default:
      return undefined;
  }
};

const ruleConfigFromProto = (c: ProtoRuleConfig): RuleConfig => {
  const out: RuleConfig = { name: c.name, duration_seconds: c.durationSeconds };
  switch (c.templateConfig.case) {
    case "offline":
      out.offline = {};
      break;
    case "hashrate":
      out.hashrate = {
        mode: hashrateModeFromProto(c.templateConfig.value.mode),
        value: c.templateConfig.value.value,
        unit: hashrateUnitFromProto(c.templateConfig.value.unit),
      };
      break;
    case "temperature":
      out.temperature = { max_celsius: c.templateConfig.value.maxCelsius };
      break;
  }
  return out;
};

const ruleConfigToProto = (c: RuleConfig): ProtoRuleConfig => {
  const base = { name: c.name, durationSeconds: c.duration_seconds };
  if (c.hashrate) {
    return create(ProtoRuleConfigSchema, {
      ...base,
      templateConfig: {
        case: "hashrate",
        value: {
          mode: c.hashrate.mode === "absolute" ? ProtoHashrateMode.ABSOLUTE : ProtoHashrateMode.PCT_EXPECTED,
          value: c.hashrate.value,
          unit:
            c.hashrate.unit === "PH"
              ? ProtoHashrateUnit.PETAHASH
              : c.hashrate.unit === "TH"
                ? ProtoHashrateUnit.TERAHASH
                : ProtoHashrateUnit.UNSPECIFIED,
        },
      },
    });
  }
  if (c.temperature) {
    return create(ProtoRuleConfigSchema, {
      ...base,
      templateConfig: { case: "temperature", value: { maxCelsius: c.temperature.max_celsius } },
    });
  }
  return create(ProtoRuleConfigSchema, { ...base, templateConfig: { case: "offline", value: {} } });
};

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
  origin: r.origin === ProtoRuleOrigin.USER ? "user" : "provisioned",
  config: r.config ? ruleConfigFromProto(r.config) : null,
});

const maintenanceWindowFromProto = (s: ProtoMaintenanceWindow): MaintenanceWindow => ({
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

// History rows persist the rule title they fired under; map retired titles to
// the current ones so old rows read consistently with the renamed rules.
const RENAMED_ALERTS: Record<string, string> = {
  "Miners Curtailed by Curtailment Source": "Curtailment Active",
  "Curtailment Source Disconnected": "Curtailment Source Unreachable",
};

const historyFromProto = (n: ProtoHistoryEntry): AlertHistoryEntry => ({
  id: n.id,
  received_at: isoFromTs(n.receivedAt),
  alert_name: RENAMED_ALERTS[n.alertName] ?? n.alertName,
  status: n.status as AlertHistoryStatus,
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
  w
    ? { url: w.url, bearerHeader: w.bearer_header ?? "", clearBearerHeader: w.clear_bearer_header ?? false }
    : undefined;

const slackToProto = (s?: SlackConfig | null) => (s ? { webhookUrl: s.webhook_url ?? "" } : undefined);

const scopeToProto = (s: MaintenanceWindowScope) => ({
  kind: scopeKindToProto(s.kind),
  ruleId: s.rule_id ?? "",
  groupId: s.group_id ?? "",
  siteId: s.site_id ?? "",
  deviceIds: s.device_ids,
});

const channelDestinationFields = (input: ChannelMutationInput) => ({
  kind: channelKindToProto(input.kind),
  webhook: webhookToProto(input.webhook),
  slack: slackToProto(input.slack),
});

export async function listChannels(): Promise<Channel[]> {
  const res = await alertChannelClient.listChannels({});
  return res.channels.map(channelFromProto);
}

export interface ChannelMutationInput {
  id?: string;
  name: string;
  kind: ChannelKind;
  webhook?: WebhookConfig | null;
  slack?: SlackConfig | null;
}

export async function createChannel(input: ChannelMutationInput): Promise<Channel> {
  const res = await alertChannelClient.createChannel({
    name: input.name,
    ...channelDestinationFields(input),
  });
  return channelFromProto(required(res.channel, "channel"));
}

export async function updateChannel(input: ChannelMutationInput & { id: string }): Promise<Channel> {
  const res = await alertChannelClient.updateChannel({
    id: input.id,
    name: input.name,
    ...channelDestinationFields(input),
  });
  return channelFromProto(required(res.channel, "channel"));
}

export async function deleteChannel(id: string): Promise<void> {
  await alertChannelClient.deleteChannel({ id });
}

export interface TestChannelResult {
  ok: boolean;
  error: string;
  response_code: number;
}

export async function testChannel(input: ChannelMutationInput): Promise<TestChannelResult> {
  const res = await alertChannelClient.testChannel({
    id: input.id ?? "",
    ...channelDestinationFields(input),
  });
  return { ok: res.ok, error: res.error, response_code: res.responseCode };
}

export async function listRules(): Promise<Rule[]> {
  const res = await alertRuleClient.listRules({});
  return res.rules.map(ruleFromProto);
}

export async function pauseRule(id: string): Promise<Rule> {
  const res = await alertRuleClient.pauseRule({ id });
  return ruleFromProto(required(res.rule, "rule"));
}

export async function resumeRule(id: string): Promise<Rule> {
  const res = await alertRuleClient.resumeRule({ id });
  return ruleFromProto(required(res.rule, "rule"));
}

export async function createRule(config: RuleConfig): Promise<Rule> {
  const res = await alertRuleClient.createRule({ config: ruleConfigToProto(config) });
  return ruleFromProto(required(res.rule, "rule"));
}

export async function updateRule(id: string, config: RuleConfig): Promise<Rule> {
  const res = await alertRuleClient.updateRule({ id, config: ruleConfigToProto(config) });
  return ruleFromProto(required(res.rule, "rule"));
}

export async function deleteRule(id: string): Promise<void> {
  await alertRuleClient.deleteRule({ id });
}

export async function listMaintenanceWindows(): Promise<MaintenanceWindow[]> {
  const res = await alertMaintenanceWindowClient.listMaintenanceWindows({});
  return res.maintenanceWindows.map(maintenanceWindowFromProto);
}

export interface MaintenanceWindowMutationInput {
  id?: string;
  scope: MaintenanceWindowScope;
  starts_at: string;
  ends_at: string | null;
  comment: string;
}

export async function createMaintenanceWindow(input: MaintenanceWindowMutationInput): Promise<MaintenanceWindow> {
  const res = await alertMaintenanceWindowClient.createMaintenanceWindow({
    scope: scopeToProto(input.scope),
    startsAt: tsFromIso(input.starts_at),
    endsAt: input.ends_at ? tsFromIso(input.ends_at) : undefined,
    comment: input.comment,
  });
  return maintenanceWindowFromProto(required(res.maintenanceWindow, "maintenanceWindow"));
}

export async function updateMaintenanceWindow(
  input: MaintenanceWindowMutationInput & { id: string },
): Promise<MaintenanceWindow> {
  const res = await alertMaintenanceWindowClient.updateMaintenanceWindow({
    id: input.id,
    scope: scopeToProto(input.scope),
    startsAt: tsFromIso(input.starts_at),
    endsAt: input.ends_at ? tsFromIso(input.ends_at) : undefined,
    comment: input.comment,
  });
  return maintenanceWindowFromProto(required(res.maintenanceWindow, "maintenanceWindow"));
}

export async function deleteMaintenanceWindow(id: string): Promise<void> {
  await alertMaintenanceWindowClient.deleteMaintenanceWindow({ id });
}

export interface HistoryPage {
  alerts: AlertHistoryEntry[];
  has_more: boolean;
}

export async function listHistory(input: {
  before_id?: string;
  page_size?: number;
  active_only?: boolean;
}): Promise<HistoryPage> {
  const res = await alertHistoryClient.listAlerts({
    beforeId: input.before_id ?? "",
    pageSize: input.page_size ?? 0,
    activeOnly: input.active_only ?? false,
  });
  return { alerts: res.alerts.map(historyFromProto), has_more: res.hasMore };
}
