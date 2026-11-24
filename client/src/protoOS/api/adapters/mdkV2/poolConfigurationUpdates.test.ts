import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { MdkV2Client } from "./connectClient";
import type { V2AuditEvent } from "./generated/generatedApi";
import { readPoolConfigurationUpdates } from "./poolConfigurationUpdates";
import { AUDIT_EVENT_LIMIT, AUDIT_WINDOW_MAX_MS } from "@/shared/features/controllerLogs/types";

const now = Date.parse("2026-11-20T10:00:00Z");
beforeEach(() => {
  vi.spyOn(Date, "now").mockReturnValue(now);
});
afterEach(() => vi.restoreAllMocks());

// The controller returns newest-first rows from a half-open time window.
const timeKey = (time: string) =>
  time.replace(/(?:\.(\d+))?Z$/, (_, fraction: string = "") => `.${fraction.padEnd(9, "0")}Z`);

const listEvents = (events: V2AuditEvent[]) =>
  vi.fn<NonNullable<MdkV2Client["systemServiceListAuditEvents"]>>(
    async ({ action, target, limit, "range.start": start, "range.end": end }) => ({
      data: {
        events: events
          .filter(
            (event) =>
              event.action === action &&
              (target === undefined || event.target === target) &&
              (!start || timeKey(event.time!) >= timeKey(start)) &&
              (!end || timeKey(event.time!) < timeKey(end)),
          )
          .slice(0, limit),
      },
    }),
  );

test("reads past a full transfer page without losing outgoing records tied at the boundary", async () => {
  const boundary = "2026-11-18T11:00:00.000456Z";
  const incoming = {
    id: "1",
    action: "mining-group.transfer",
    target: "selected",
    time: "2026-11-18T10:00:00Z",
    result: "ok",
  };
  const unrelated = Array.from({ length: AUDIT_EVENT_LIMIT }, (_, index) => ({
    ...incoming,
    id: String(AUDIT_EVENT_LIMIT + 2 - index),
    target: "another-group",
    time: index === AUDIT_EVENT_LIMIT - 1 ? boundary : "2026-11-18T12:00:00Z",
  }));
  const outgoing = {
    ...incoming,
    id: "2",
    target: "another-group",
    time: boundary,
    detailsJson: JSON.stringify({ source_group_id: "selected", mac_addresses: ["00:11:22:33:44:a1"] }),
  };
  const listAuditEvents = listEvents([...unrelated, outgoing, incoming]);

  expect(await readPoolConfigurationUpdates(listAuditEvents, "selected", {})).toEqual({
    updates: [
      { id: "2", action: "Transfer out of this group", time: boundary, result: "ok", macs: ["00:11:22:33:44:a1"] },
      { id: "1", action: "Transfer into this group", time: incoming.time, result: "ok", macs: [] },
    ],
    limitReached: false,
  });
  expect(listAuditEvents).toHaveBeenCalledWith(
    expect.objectContaining({
      action: "mining-group.transfer",
      target: undefined,
      "range.start": new Date(now - AUDIT_WINDOW_MAX_MS).toISOString(),
      "range.end": "2026-11-18T11:00:00.000457Z",
      limit: AUDIT_EVENT_LIMIT,
    }),
    {},
  );
});

test("reports incomplete coverage and keeps scoped incoming results when the transfer cursor cannot advance", async () => {
  const incoming = {
    id: "1",
    action: "mining-group.transfer",
    target: "selected",
    time: "2026-11-18T10:00:00Z",
    result: "ok",
  };
  const unrelated = Array.from({ length: AUDIT_EVENT_LIMIT }, (_, index) => ({
    ...incoming,
    id: String(index + 2),
    target: "another-group",
    time: "2026-11-18T11:00:00Z",
  }));
  const listAuditEvents = listEvents([...unrelated, incoming]);

  expect(await readPoolConfigurationUpdates(listAuditEvents, "selected", {})).toEqual({
    updates: [{ id: "1", action: "Transfer into this group", time: incoming.time, result: "ok", macs: [] }],
    limitReached: true,
  });
  expect(
    listAuditEvents.mock.calls.filter(([query]) => query.action === incoming.action && query.target === undefined),
  ).toHaveLength(2);
});

test("bounds outgoing pages and concurrent Default reads", async () => {
  const events = Array.from({ length: AUDIT_EVENT_LIMIT * 3 + 1 }, (_, index) => ({
    id: String(index + 1),
    action: "mining-group.transfer",
    target: "another-group",
    time: new Date(now - 1 - index).toISOString(),
  }));
  const listAuditEvents = listEvents(events);
  let inFlight = 0;
  let maxInFlight = 0;
  const countInFlight: NonNullable<MdkV2Client["systemServiceListAuditEvents"]> = async (query, options) => {
    inFlight++;
    maxInFlight = Math.max(maxInFlight, inFlight);
    try {
      return await listAuditEvents(query, options);
    } finally {
      inFlight--;
    }
  };

  expect(await readPoolConfigurationUpdates(countInFlight, "default", {})).toEqual({ updates: [], limitReached: true });
  expect(maxInFlight).toBeLessThanOrEqual(3);
  expect(
    listAuditEvents.mock.calls.filter(
      ([query]) => query.action === "mining-group.transfer" && query.target === undefined,
    ),
  ).toHaveLength(3);
});

test("deduplicates incoming events and reads the supported spellings of hashboard addresses", async () => {
  const incoming = {
    id: "1",
    action: "mining-group.transfer",
    target: "selected",
    time: "2026-11-18T10:00:00Z",
    result: "ok",
    detailsJson: JSON.stringify({
      source_group_id: "other",
      mac_addresses: [
        "00-11-22-33-44-A1",
        "00:11:22:33:44:a1",
        "0011.2233.44A2",
        "00-11-22-33-44-a2",
        "00:11:22:33:44:A3",
        "00:11-22:33:44:a4",
      ],
    }),
  };
  const outgoing = {
    ...incoming,
    id: "2",
    target: "other",
    time: "2026-11-18T11:00:00Z",
    result: "error",
    detailsJson: JSON.stringify({ source_group_id: "selected", mac_addresses: ["0011.2233.44a4"] }),
  };

  expect(await readPoolConfigurationUpdates(listEvents([outgoing, incoming]), "selected", {})).toEqual({
    updates: [
      {
        id: "2",
        action: "Transfer out of this group",
        time: outgoing.time,
        result: "error",
        macs: ["00:11:22:33:44:a4"],
      },
      {
        id: "1",
        action: "Transfer into this group",
        time: incoming.time,
        result: "ok",
        macs: ["00:11:22:33:44:a1", "00:11:22:33:44:a2", "00:11:22:33:44:a3"],
      },
    ],
    limitReached: false,
  });
});

test("reports allocation-mode changes only for Default", async () => {
  const event = {
    id: "1",
    action: "pool.set-allocation-mode",
    target: "container:pools",
    time: "2026-11-18T10:00:00Z",
    result: "ok",
  };
  const listAuditEvents = listEvents([event, { ...event, id: "2", target: "other" }]);

  expect(await readPoolConfigurationUpdates(listAuditEvents, "default", {})).toEqual({
    updates: [{ id: event.id, action: "Pool allocation mode update", time: event.time, result: "ok", macs: [] }],
    limitReached: false,
  });
  expect(await readPoolConfigurationUpdates(listAuditEvents, "selected", {})).toEqual({
    updates: [],
    limitReached: false,
  });
});

test("sorts by full timestamp and breaks ties with the controller's numeric audit ID", async () => {
  const older = {
    id: "9007199254740995",
    action: "mining-group.pools.replace",
    time: "2026-11-18T10:00:00.100123Z",
  };
  const newer = {
    id: "9007199254740992",
    action: "mining-group.pools.replace",
    time: "2026-11-18T10:00:00.100456Z",
  };
  const last = { ...newer, id: "9007199254740993", action: "mining-group.update" };
  const listAuditEvents = listEvents([last, newer, older].map((event) => ({ ...event, target: "selected" })));

  const { updates } = await readPoolConfigurationUpdates(listAuditEvents, "selected", {});
  expect(updates.map(({ id }) => id)).toEqual([last.id, newer.id, older.id]);
});
