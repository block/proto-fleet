import { describe, expect, it, vi } from "vitest";

import type { MdkV2Client } from "./connectClient";
import { V2HashboardErrorCause, V2HashboardEventType, V2MiningState } from "./generated/generatedApi";
import { MdkV2Adapter } from "./MdkV2Adapter";

const MAC = "02:00:00:00:00:0a";
const headers = { headers: { "X-Frigate-Actor": "proto-os" } };

const historyClient = (overrides: Partial<MdkV2Client> = {}) =>
  ({
    telemetryServiceFollowAsicHealth: vi.fn(),
    hashboardServiceGetHashboardEvents: vi.fn(),
    hashboardServiceListRecoveryEvidence: vi.fn(),
    hashboardServiceGetRecoveryEvidence: vi.fn(),
    ...overrides,
  }) as unknown as MdkV2Client;

describe("MdkV2 board history", () => {
  it("is offered only on the Connect client, which Fleet's REST proxy is not", () => {
    const rest = historyClient({ telemetryServiceFollowAsicHealth: undefined });
    const adapter = new MdkV2Adapter(rest);
    expect(adapter.getBoardEvents).toBeUndefined();
    expect(adapter.listRecoveryEvidence).toBeUndefined();
    expect(adapter.getRecoveryEvidence).toBeUndefined();
  });

  it("reads a window of past events and leaves out the current isolation record", async () => {
    const getEvents = vi.fn().mockResolvedValue({
      data: {
        macAddress: MAC,
        effectiveWindow: { start: "2026-11-15T00:00:00Z", end: "2026-11-16T00:00:00Z" },
        events: [
          {
            type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE,
            time: "2026-11-15T01:00:00Z",
            fromState: V2MiningState.MINING_STATE_MINING,
            toState: V2MiningState.MINING_STATE_ERROR,
          },
          {
            type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_ERROR,
            time: "2026-11-15T01:00:00Z",
            lastSeen: "2026-11-15T01:18:00Z",
            count: 36,
            cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE,
            detail: "engineering detail",
            asic: { id: 4 },
          },
          { type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_STACK_DROPOUT, time: "2026-11-15T01:05:00Z", stack: 2 },
          {
            type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_WARNING,
            time: "2026-11-15T03:00:00Z",
            lastSeen: "2026-11-15T03:02:00Z",
            count: 4,
            cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE,
            stack: 1,
          },
          {
            type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_LAST_SHUTDOWN,
            time: "2026-11-15T04:00:00Z",
            lastSeen: "2026-11-15T04:00:00Z",
            count: 1,
            cause: V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE,
          },
          { type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_ERROR, time: " " },
          { type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_ISOLATION, time: "2026-11-16T00:00:00Z" },
          { type: V2HashboardEventType.HASHBOARD_EVENT_TYPE_UNSPECIFIED, time: "2026-11-15T02:00:00Z" },
        ],
      },
    });
    const adapter = new MdkV2Adapter(historyClient({ hashboardServiceGetHashboardEvents: getEvents }));

    const page = await adapter.getBoardEvents!(MAC, { start: "2026-11-15T00:00:00Z", end: "2026-11-16T00:00:00Z" });

    expect(getEvents).toHaveBeenCalledWith(
      { identifier: MAC, "window.start": "2026-11-15T00:00:00Z", "window.end": "2026-11-16T00:00:00Z" },
      headers,
    );
    expect(page.window).toEqual({ start: "2026-11-15T00:00:00Z", end: "2026-11-16T00:00:00Z" });
    expect(page.events.map(({ id: _id, ...event }) => event)).toEqual([
      {
        kind: "stateChange",
        time: "2026-11-15T01:00:00Z",
        fromState: "MINING_STATE_MINING",
        toState: "MINING_STATE_ERROR",
      },
      {
        kind: "error",
        time: "2026-11-15T01:00:00Z",
        lastSeen: "2026-11-15T01:18:00Z",
        samples: 36,
        cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
      },
      { kind: "stackDropout", time: "2026-11-15T01:05:00Z", stack: 2 },
      // The stack index belongs to stack dropouts only.
      {
        kind: "warning",
        time: "2026-11-15T03:00:00Z",
        lastSeen: "2026-11-15T03:02:00Z",
        samples: 4,
        cause: "HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE",
      },
      {
        kind: "lastShutdown",
        time: "2026-11-15T04:00:00Z",
        lastSeen: "2026-11-15T04:00:00Z",
        samples: 1,
        cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
      },
    ]);
    expect(new Set(page.events.map((event) => event.id)).size).toBe(5);
  });

  it("lists retained evidence for the board and reads one snapshot", async () => {
    const summary = {
      evidenceId: "evidence-1",
      episodeId: "episode-3",
      macAddress: MAC,
      trigger: "power_recovery",
      causes: [V2HashboardErrorCause.HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE],
      capturedAt: "2026-11-14T08:00:00Z",
      capturedBy: "ct1-a",
      truncated: true,
      payloadBytes: 900,
    };
    const list = vi.fn().mockResolvedValue({ data: { evidence: [summary, { trigger: "ready_recovery" }] } });
    const get = vi.fn().mockResolvedValue({ data: { summary, evidenceJson: '{"version":1}' } });
    const adapter = new MdkV2Adapter(
      historyClient({ hashboardServiceListRecoveryEvidence: list, hashboardServiceGetRecoveryEvidence: get }),
    );

    const expected = {
      id: "evidence-1",
      episodeId: "episode-3",
      trigger: "power_recovery",
      causes: ["HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE"],
      capturedAt: "2026-11-14T08:00:00Z",
      capturedBy: "ct1-a",
      truncated: true,
    };
    await expect(adapter.listRecoveryEvidence!(MAC)).resolves.toEqual([expected]);
    expect(list).toHaveBeenCalledWith({ identifier: MAC, limit: 10 }, headers);
    await expect(adapter.getRecoveryEvidence!("evidence-1")).resolves.toEqual({
      summary: expected,
      json: '{"version":1}',
    });
    expect(get).toHaveBeenCalledWith({ evidenceId: "evidence-1" }, headers);
  });
});
