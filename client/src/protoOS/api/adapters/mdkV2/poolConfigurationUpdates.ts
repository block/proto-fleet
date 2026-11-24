import { create, fromJson, toJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import type { PoolConfigurationUpdate, PoolConfigurationUpdatePage } from "../poolAssignment";
import type { AdapterRequestOptions } from "../types";
import { mapWithConcurrency } from "../utils/async";
import type { MdkV2Client } from "./connectClient";
import type { V2AuditEvent } from "./generated/generatedApi";
import { isMacAddress, macKey } from "@/protoOS/utils/macAddress";
import { AUDIT_EVENT_LIMIT, AUDIT_WINDOW_MAX_MS } from "@/shared/features/controllerLogs/types";

const TRANSFER = "mining-group.transfer";
const OUTGOING_PAGE_LIMIT = 3;
const AUDIT_READ_CONCURRENCY = 3;
const NANOS_PER_MICROSECOND = 1_000n;
const NANOS_PER_MILLISECOND = 1_000_000n;
const NANOS_PER_SECOND = 1_000_000_000n;
const DEFAULT_POOL_ACTIONS = [
  { action: "pool.create", label: "Pool addition" },
  { action: "pool.update", label: "Pool settings update" },
  { action: "pool.delete", label: "Pool deletion" },
  { action: "pool.set-allocation-mode", target: "container:pools", label: "Pool allocation mode update" },
];

const auditTimestamp = (time?: string) => {
  if (!time) return;
  try {
    const { seconds, nanos } = fromJson(TimestampSchema, time);
    return seconds * NANOS_PER_SECOND + BigInt(nanos);
  } catch {
    return undefined;
  }
};

const auditRangeEnd = (time: bigint) =>
  toJson(
    TimestampSchema,
    create(TimestampSchema, { seconds: time / NANOS_PER_SECOND, nanos: Number(time % NANOS_PER_SECOND) }),
  );

const normalizeAuditMac = (value: string) => {
  const mac = macKey(value);
  if (/^(?:[0-9a-f]{2}-){5}[0-9a-f]{2}$/.test(mac)) return mac.replaceAll("-", ":");
  if (/^(?:[0-9a-f]{4}\.){2}[0-9a-f]{4}$/.test(mac)) return mac.replaceAll(".", "").match(/../g)?.join(":");
  return isMacAddress(mac) ? mac : undefined;
};

const readTransfer = (event: V2AuditEvent, groupId: string) => {
  let details: { source_group_id?: string; mac_addresses?: unknown } | undefined;
  try {
    details = JSON.parse(event.detailsJson || "{}") ?? undefined;
  } catch {
    // The target still identifies incoming requests without usable details.
  }
  const incoming = event.target === groupId;
  if (!incoming && details?.source_group_id !== groupId) return;
  const macs = new Set<string>();
  if (Array.isArray(details?.mac_addresses)) {
    for (const value of details.mac_addresses) {
      if (typeof value !== "string") continue;
      const mac = normalizeAuditMac(value);
      if (mac) macs.add(mac);
    }
  }
  return { action: incoming ? "Transfer into this group" : "Transfer out of this group", macs: [...macs] };
};

export async function readPoolConfigurationUpdates(
  listAuditEvents: NonNullable<MdkV2Client["systemServiceListAuditEvents"]>,
  id: string,
  options: AdapterRequestOptions,
): Promise<PoolConfigurationUpdatePage> {
  const actions: Array<{ action: string; label: string; target?: string }> = [
    { action: "mining-group.pools.replace", target: id, label: "Pool settings update" },
    { action: "mining-group.update", target: id, label: "Configuration details update" },
    // Audit events use the destination as their target. Read inbound transfers
    // separately so transfers between other groups cannot crowd them out.
    { action: TRANSFER, label: "Transfer" },
    { action: TRANSFER, target: id, label: "Transfer" },
    ...(id === "default" ? DEFAULT_POOL_ACTIONS : []),
  ];
  const now = Date.now();
  const start = new Date(now - AUDIT_WINDOW_MAX_MS).toISOString();
  const pages = await mapWithConcurrency(
    actions,
    AUDIT_READ_CONCURRENCY,
    async ({ action, target, label }) => {
      const maxPages = action === TRANSFER && target === undefined ? OUTGOING_PAGE_LIMIT : 1;
      const updates: PoolConfigurationUpdate[] = [];
      let end = BigInt(now) * NANOS_PER_MILLISECOND;
      for (let page = 0; page < maxPages; page++) {
        options.signal?.throwIfAborted();
        const { data } = await listAuditEvents(
          { action, target, "range.start": start, "range.end": auditRangeEnd(end), limit: AUDIT_EVENT_LIMIT },
          options,
        );
        const events = data.events ?? [];
        for (const event of events) {
          if (!event.id || event.action !== action || (target !== undefined && event.target !== target)) continue;
          const update = action === TRANSFER ? readTransfer(event, id) : { action: label, macs: [] };
          if (update) updates.push({ ...update, id: event.id, time: event.time, result: event.result ?? "" });
        }
        if (events.length < AUDIT_EVENT_LIMIT) return { updates, limitReached: false };
        if (page + 1 === maxPages) break;
        const oldest = auditTimestamp(events[events.length - 1]?.time);
        if (oldest === undefined) break;
        // Frigate's end bound is exclusive and PostgreSQL uses microseconds.
        // Include the oldest timestamp so this read can find any unseen ties.
        const nextEnd = oldest + NANOS_PER_MICROSECOND;
        if (nextEnd >= end) break;
        end = nextEnd;
      }
      return { updates, limitReached: true };
    },
    options.signal ?? undefined,
  );
  const unique = new Map(pages.flatMap(({ updates }) => updates.map((update) => [update.id, update] as const)));
  return {
    updates: [...unique.values()]
      .map((update) => ({
        update,
        time: auditTimestamp(update.time) ?? 0n,
        id: /^\d+$/.test(update.id) ? BigInt(update.id) : 0n,
      }))
      .sort((a, b) => Number(b.time - a.time || b.id - a.id))
      .map(({ update }) => update),
    limitReached: pages.some(({ limitReached }) => limitReached),
  };
}
