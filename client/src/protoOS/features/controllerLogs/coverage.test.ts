import { describe, expect, it } from "vitest";

import { auditCoverageNotice, entriesInWindow, logCoverageNotice } from "./coverage";
import type { ControllerLogEntry, ControllerLogSnapshot } from "@/protoOS/api/adapters/controllerLogs";

const at = (iso: string): ControllerLogEntry => ({ timestamp: iso, severity: "info", component: "pool", message: iso });
const snapshot = (overrides: Partial<ControllerLogSnapshot> = {}): ControllerLogSnapshot => ({
  entries: [at("2026-11-13T10:00:00Z"), at("2026-11-13T10:30:00Z")],
  limit: 1000,
  coverage: { state: "retained" },
  fetchedAt: "2026-11-13T11:00:00Z",
  ...overrides,
});
const ms = (iso: string) => Date.parse(iso);

describe("logCoverageNotice", () => {
  it("needs no notice when the time range starts after the oldest retained entry", () => {
    expect(logCoverageNotice(snapshot(), ms("2026-11-13T10:15:00Z"))).toBeUndefined();
    expect(logCoverageNotice(snapshot({ coverage: { state: "limit-reached" } }), ms("2026-11-13T10:00:00Z"))).toBe(
      undefined,
    );
  });

  it("says where retained logs start when the range reaches past them", () => {
    expect(logCoverageNotice(snapshot(), ms("2026-11-13T09:00:00Z"))).toMatchObject({
      intent: "information",
      title: expect.stringMatching(/^Logs available from /),
      subtitle: expect.stringContaining("controller memory"),
    });
    expect(logCoverageNotice(snapshot(), undefined)).toMatchObject({ title: expect.stringMatching(/^Logs available/) });
  });

  it("says when the limit cut off older matching entries", () => {
    expect(logCoverageNotice(snapshot({ coverage: { state: "limit-reached" } }), undefined)).toMatchObject({
      subtitle: "Only the latest 1,000 matching entries are returned.",
    });
  });

  it("warns that an interrupted read is missing newer entries even inside the range", () => {
    const notice = logCoverageNotice(
      snapshot({ coverage: { state: "interrupted", reason: "log stream reset by controller" } }),
      ms("2026-11-13T10:15:00Z"),
    );
    expect(notice).toEqual({
      intent: "warning",
      title: "Log stream interrupted",
      subtitle:
        "Received 2 entries before the read stopped: log stream reset by controller. Newer entries may be missing.",
    });
  });

  it("does not present a silent controller as an empty log", () => {
    expect(logCoverageNotice(snapshot({ entries: [], coverage: { state: "no-response" } }), undefined)).toEqual({
      intent: "information",
      title: "No log entries received",
      subtitle: "Either no entries match these filters, or the controller is slow to respond.",
    });
  });

  it("explains an empty tail rather than implying there were no events", () => {
    expect(logCoverageNotice(snapshot({ entries: [] }), ms("2026-11-13T10:15:00Z"))).toMatchObject({
      title: "Logs are kept in controller memory only",
    });
  });
});

describe("entriesInWindow", () => {
  it("keeps entries in the range and entries without a time", () => {
    const untimed: ControllerLogEntry = { severity: "warn", component: "power", message: "no time" };
    expect(
      entriesInWindow([at("2026-11-13T10:00:00Z"), untimed, at("2026-11-13T10:30:00Z")], ms("2026-11-13T10:15:00Z")),
    ).toEqual([untimed, at("2026-11-13T10:30:00Z")]);
  });
});

describe("auditCoverageNotice", () => {
  const event = (time: string) => ({
    id: time,
    time,
    actor: "operator@bridge",
    source: "rest:10.0.0.2",
    node: "ct1-a",
    action: "SetBreaker",
    target: "breaker-1",
    result: "ok",
  });

  it("says nothing when the page holds every event in the range", () => {
    expect(
      auditCoverageNotice({ events: [event("2026-11-13T10:00:00Z")], limit: 1000, limitReached: false, fetchedAt: "" }),
    ).toBeUndefined();
  });

  it("names the oldest shown event when the page is full", () => {
    expect(
      auditCoverageNotice({
        events: [event("2026-11-13T10:00:00Z"), event("2026-11-13T09:00:00Z")],
        limit: 2,
        limitReached: true,
        fetchedAt: "",
      }),
    ).toMatchObject({
      title: "Showing the newest 2 events",
      subtitle: expect.stringMatching(/^Events before .+ aren't shown\.$/),
    });
  });
});
