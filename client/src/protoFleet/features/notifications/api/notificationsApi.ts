// Thin fetch wrapper around the notifications Connect-RPC endpoints
// fleet-api mounts at /notifications.v1.<Service>/<Method>. The
// server hand-rolls the JSON shape rather than relying on the
// Connect generated bindings (the proto file in this repo lands
// ahead of the codegen step); the URL scheme and JSON-over-POST
// wire format here matches what `buf generate` would emit, so the
// switch to typed bindings is a mechanical change later.
//
// Each call:
//
//   - POSTs to the procedure URL with the request body serialised as
//     JSON.
//   - Includes session cookies via credentials: "include".
//   - Decodes the response, unwrapping the single top-level field
//     (e.g. `{"channels": [...]}` → `[...]`) so the callers don't
//     have to handle the envelope.
//   - Throws a NotificationsApiError on non-2xx that carries the
//     server's `code` so the toaster + getErrorMessage helper can
//     branch on it.
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import type {
  Channel,
  ChannelKind,
  NotificationHistoryEntry,
  Rule,
  Silence,
  SilenceScope,
  SmtpConfig,
  WebhookConfig,
} from "@/protoFleet/features/notifications/types";

// Connect-RPC's URL scheme is `/<package>.<Service>/<Method>` — the
// package and service are joined with a dot, not a slash. Each call
// below prefixes its path with the dotted base so the proxy and the
// server-side route patterns line up exactly with what `buf generate`
// would emit.
const BASE = `${API_PROXY_BASE}/notifications.v1.`;

export class NotificationsApiError extends Error {
  public readonly code: string;

  public constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

async function call<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(`${BASE}${path}`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (!response.ok) {
    let code = "internal";
    let message = response.statusText;
    try {
      const parsed = (await response.json()) as { code?: string; message?: string };
      if (parsed.code) code = parsed.code;
      if (parsed.message) message = parsed.message;
    } catch {
      /* response wasn't JSON */
    }
    throw new NotificationsApiError(code, message);
  }
  if (response.status === 204) return {} as T;
  return (await response.json()) as T;
}

// === Channels ===========================================================

export async function listChannels(): Promise<Channel[]> {
  const out = await call<{ channels?: Channel[] }>("ChannelService/ListChannels", {});
  return out.channels ?? [];
}

export interface ChannelMutationInput {
  id?: string;
  name: string;
  kind: ChannelKind;
  webhook?: WebhookConfig | null;
  smtp?: SmtpConfig | null;
}

export async function createChannel(input: ChannelMutationInput): Promise<Channel> {
  const out = await call<{ channel: Channel }>("ChannelService/CreateChannel", input);
  return out.channel;
}

export async function updateChannel(input: ChannelMutationInput & { id: string }): Promise<Channel> {
  const out = await call<{ channel: Channel }>("ChannelService/UpdateChannel", input);
  return out.channel;
}

export async function deleteChannel(id: string): Promise<void> {
  await call<Record<string, never>>("ChannelService/DeleteChannel", { id });
}

export interface TestChannelResult {
  ok: boolean;
  error: string;
  response_code: number;
}

export async function testChannel(input: ChannelMutationInput): Promise<TestChannelResult> {
  return await call<TestChannelResult>("ChannelService/TestChannel", input);
}

// === Rules ==============================================================

export async function listRules(): Promise<Rule[]> {
  const out = await call<{ rules?: Rule[] }>("RuleService/ListRules", {});
  return out.rules ?? [];
}

export async function pauseRule(id: string): Promise<Rule> {
  const out = await call<{ rule: Rule }>("RuleService/PauseRule", { id });
  return out.rule;
}

export async function resumeRule(id: string): Promise<Rule> {
  const out = await call<{ rule: Rule }>("RuleService/ResumeRule", { id });
  return out.rule;
}

// === Silences ===========================================================

export async function listSilences(): Promise<Silence[]> {
  const out = await call<{ silences?: Silence[] }>("SilenceService/ListSilences", {});
  return out.silences ?? [];
}

export interface SilenceMutationInput {
  id?: string;
  scope: SilenceScope;
  starts_at: string;
  ends_at: string | null;
  comment: string;
}

export async function createSilence(input: SilenceMutationInput): Promise<Silence> {
  const out = await call<{ silence: Silence }>("SilenceService/CreateSilence", input);
  return out.silence;
}

export async function updateSilence(input: SilenceMutationInput & { id: string }): Promise<Silence> {
  const out = await call<{ silence: Silence }>("SilenceService/UpdateSilence", input);
  return out.silence;
}

export async function deleteSilence(id: string): Promise<void> {
  await call<Record<string, never>>("SilenceService/DeleteSilence", { id });
}

// === History ============================================================

export interface HistoryPage {
  notifications: NotificationHistoryEntry[];
  has_more: boolean;
}

export async function listHistory(input: { before_id?: string; page_size?: number }): Promise<HistoryPage> {
  const out = await call<Partial<HistoryPage>>("HistoryService/ListNotifications", input);
  return { notifications: out.notifications ?? [], has_more: out.has_more ?? false };
}
