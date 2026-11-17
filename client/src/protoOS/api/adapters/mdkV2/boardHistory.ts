import type { AdapterRequestOptions } from "../types";
import type { MdkV2Client } from "./connectClient";
import { type V2HashboardEvent, V2HashboardEventType, type V2RecoveryEvidenceSummary } from "./generated/generatedApi";
import type {
  BoardEvent,
  BoardEventKind,
  BoardEventPage,
  BoardEventQuery,
  RecoveryEvidenceRecord,
  RecoveryEvidenceSummary,
} from "@/shared/features/boardHistory/types";
import { RECOVERY_EVIDENCE_PER_BOARD } from "@/shared/features/boardHistory/types";

type Method<K extends keyof MdkV2Client> = NonNullable<MdkV2Client[K]>;

// ISOLATION is the board's current isolation record, appended at the window
// end; it is current state, so it never appears as history.
const kinds: Partial<Record<string, BoardEventKind>> = {
  [V2HashboardEventType.HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE]: "stateChange",
  [V2HashboardEventType.HASHBOARD_EVENT_TYPE_ERROR]: "error",
  [V2HashboardEventType.HASHBOARD_EVENT_TYPE_WARNING]: "warning",
  [V2HashboardEventType.HASHBOARD_EVENT_TYPE_LAST_SHUTDOWN]: "lastShutdown",
  [V2HashboardEventType.HASHBOARD_EVENT_TYPE_STACK_DROPOUT]: "stackDropout",
};

const text = (value: string | undefined) => value?.trim() || undefined;

const toBoardEvent = (wire: V2HashboardEvent, index: number): BoardEvent[] => {
  const kind = kinds[wire.type ?? ""];
  const time = text(wire.time);
  if (!kind || !time) return [];
  const aggregated = kind === "error" || kind === "warning" || kind === "lastShutdown";
  return [
    {
      id: `${index}:${kind}:${time}`,
      kind,
      time,
      lastSeen: aggregated ? text(wire.lastSeen) : undefined,
      samples: aggregated && wire.count ? wire.count : undefined,
      cause: aggregated ? wire.cause : undefined,
      fromState: kind === "stateChange" ? wire.fromState : undefined,
      toState: kind === "stateChange" ? wire.toState : undefined,
      stack: kind === "stackDropout" ? wire.stack : undefined,
    },
  ];
};

export async function readBoardEvents(
  getEvents: Method<"hashboardServiceGetHashboardEvents">,
  identifier: string,
  { start, end }: BoardEventQuery,
  params: AdapterRequestOptions,
): Promise<BoardEventPage> {
  const { data } = await getEvents({ identifier, "window.start": start, "window.end": end }, params);
  const window = data.effectiveWindow;
  return {
    events: (data.events ?? []).flatMap(toBoardEvent),
    window: window?.start && window.end ? { start: window.start, end: window.end } : undefined,
    fetchedAt: new Date().toISOString(),
  };
}

const toSummary = (wire: V2RecoveryEvidenceSummary | undefined, fallbackId = ""): RecoveryEvidenceSummary => ({
  id: text(wire?.evidenceId) ?? fallbackId,
  episodeId: text(wire?.episodeId),
  capturedAt: text(wire?.capturedAt),
  capturedBy: text(wire?.capturedBy),
  trigger: text(wire?.trigger),
  causes: wire?.causes ?? [],
  truncated: wire?.truncated ?? false,
});

export async function listRecoveryEvidence(
  list: Method<"hashboardServiceListRecoveryEvidence">,
  identifier: string,
  params: AdapterRequestOptions,
): Promise<RecoveryEvidenceSummary[]> {
  const { data } = await list({ identifier, limit: RECOVERY_EVIDENCE_PER_BOARD }, params);
  return (data.evidence ?? []).map((wire) => toSummary(wire)).filter((summary) => summary.id);
}

export async function getRecoveryEvidence(
  get: Method<"hashboardServiceGetRecoveryEvidence">,
  evidenceId: string,
  params: AdapterRequestOptions,
): Promise<RecoveryEvidenceRecord> {
  const { data } = await get({ evidenceId }, params);
  return { summary: toSummary(data.summary, evidenceId), json: data.evidenceJson ?? "" };
}
