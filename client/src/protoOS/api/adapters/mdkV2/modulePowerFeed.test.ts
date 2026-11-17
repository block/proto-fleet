import { describe, expect, test, vi } from "vitest";
import type { V2GetContainerMapResponse, V2Slot } from "./generated/generatedApi";
import { V2PowerFeedState } from "./generated/generatedApi";
import { MdkV2Adapter, type MdkV2Client, POWER_FEED_READ_TIMEOUT_MS } from "./MdkV2Adapter";
import { feedReading, moduleFeedMapping, recoveryCycle } from "./modulePowerFeed";

const FEED = "pdu-1-outlet-05";
const target = { tank: 1, module: 5, reportedId: "M-1-5" };

// A board in position `slot` naming `feed` everywhere, as the controller reports it.
const board = (
  slot: number,
  overrides: Partial<V2Slot> = {},
  location: Record<string, unknown> = {},
  moduleId = "M-1-5",
): V2Slot => ({
  occupied: true,
  powerFeedId: FEED,
  configuredLocation: { tank: 1, module: 5, hashboardPosition: slot },
  hashboard: {
    macAddress: `aa:bb:cc:00:05:0${slot}`,
    moduleId,
    location: { hashboardPosition: String(slot) as unknown as number, powerFeedId: FEED, ...location },
  },
  ...overrides,
});

const map = (slots: V2Slot[], others: { tank: number; module: number; slots: V2Slot[] }[] = []) =>
  ({
    containerMap: {
      tanks: [
        {
          id: 1,
          modules: [
            { id: 5, slots },
            ...others.filter((o) => o.tank === 1).map(({ module, slots }) => ({ id: module, slots })),
          ],
        },
        ...others
          .filter((o) => o.tank !== 1)
          .map(({ tank, module, slots }) => ({ id: tank, modules: [{ id: module, slots }] })),
      ],
    },
  }) as V2GetContainerMapResponse;

const healthy = [board(1), board(2), board(3)];

describe("moduleFeedMapping", () => {
  test("one feed across every board is configured", () => {
    expect(moduleFeedMapping(map(healthy), target)).toEqual({
      feedId: FEED,
      confidence: "configured",
      reasons: [],
      sharedWith: [],
    });
  });

  test("every occupied board verified by the wiring check is wiring verified", () => {
    const slots = [1, 2, 3].map((slot) => board(slot, {}, { wiringStatus: "VERIFIED" }));
    expect(moduleFeedMapping(map(slots), target).confidence).toBe("wiring_verified");
  });

  test("an unverified wiring check still reads configured", () => {
    const slots = [board(1, {}, { wiringStatus: "unverified" }), board(2), board(3)];
    expect(moduleFeedMapping(map(slots), target).confidence).toBe("configured");
  });

  test.each([
    ["a feed also named by another module", [board(1), board(2), board(3)], "feed_shared", [{ tank: 2, module: 6 }]],
    [
      "boards naming different feeds",
      [board(1), board(2, {}, { powerFeedId: "pdu-1-outlet-06" }), board(3)],
      "multiple_feeds",
    ],
    [
      "a board that names no feed",
      [board(1), board(2, { powerFeedId: "" }, { powerFeedId: "" }), board(3)],
      "feed_evidence_incomplete",
    ],
    ["a wiring mismatch", [board(1, {}, { wiringStatus: "mismatch" }), board(2), board(3)], "wiring_mismatch"],
    [
      "a slot configured for another module",
      [board(1, { configuredLocation: { tank: 1, module: 6, hashboardPosition: 1 } }), board(2), board(3)],
      "configured_location_mismatch",
    ],
    ["a board outside the module's positions", [board(1), board(2), board(4)], "slot_placement_invalid"],
    [
      "a board payload the plugin would reject",
      [board(1), board(2), board(3, { hashboard: { ...board(3).hashboard, serial: "SN\0" } })],
      "slot_placement_invalid",
    ],
  ])("%s is ambiguous", (_, slots, reason, sharedWith = []) => {
    const others = reason === "feed_shared" ? [{ tank: 2, module: 6, slots: [board(1, {}, {}, "M-2-6")] }] : [];
    const mapping = moduleFeedMapping(map(slots as V2Slot[], others), target);
    expect(mapping.confidence).toBe("ambiguous");
    expect(mapping.reasons).toEqual([reason]);
    expect(mapping.feedId).toBe(reason === "multiple_feeds" ? undefined : FEED);
    expect(mapping.sharedWith).toEqual(sharedWith);
  });

  test("a different module at the position is ambiguous", () => {
    expect(moduleFeedMapping(map(healthy), { ...target, reportedId: "M-9-9" }).reasons).toEqual([
      "module_identity_changed",
    ]);
  });

  test.each([
    ["boards at the position reporting different module IDs", [board(1), board(2), board(3, {}, {}, "M-OTHER")], []],
    ["a board reporting the module ID with padding", [board(1), board(2), board(3, {}, {}, " M-1-5")], []],
    [
      "the same module ID reported at another position",
      healthy,
      [
        {
          tank: 2,
          module: 6,
          slots: [board(1, { powerFeedId: "pdu-2-outlet-06" }, { powerFeedId: "pdu-2-outlet-06" })],
        },
      ],
    ],
  ])("%s reads as a changed identity", (_, slots, others) => {
    expect(moduleFeedMapping(map(slots, others), target)).toMatchObject({
      confidence: "ambiguous",
      reasons: ["module_identity_changed"],
      feedId: FEED,
    });
  });

  test("a module with no reported identity is ambiguous", () => {
    const mapping = moduleFeedMapping(map(healthy), { tank: 1, module: 5 });
    expect(mapping).toMatchObject({ confidence: "ambiguous", reasons: ["module_identity_unconfirmed"], feedId: FEED });
  });

  test("no feed named anywhere is unavailable", () => {
    const slots = [1, 2, 3].map((slot) => board(slot, { powerFeedId: "" }, { powerFeedId: "" }));
    expect(moduleFeedMapping(map(slots), target)).toEqual({
      confidence: "unavailable",
      reasons: ["no_feed_assigned"],
      sharedWith: [],
    });
  });

  test("duplicate entries for one position read the last one, like the plugin", () => {
    const other = [1, 2, 3].map((slot) =>
      board(slot, { powerFeedId: "pdu-1-outlet-06" }, { powerFeedId: "pdu-1-outlet-06" }),
    );
    const mapping = moduleFeedMapping(map(healthy, [{ tank: 1, module: 5, slots: other }]), target);
    expect(mapping).toMatchObject({ feedId: "pdu-1-outlet-06", confidence: "ambiguous" });
    expect(mapping.reasons).toContain("duplicate_module");
  });

  test("a missing map or module is unavailable", () => {
    expect(moduleFeedMapping({}, target).reasons).toEqual(["topology_unavailable"]);
    expect(moduleFeedMapping(map(healthy), { ...target, module: 7 }).reasons).toEqual(["module_not_in_topology"]);
  });
});

describe("feedReading", () => {
  const feed = { powerFeedId: FEED, state: V2PowerFeedState.POWER_FEED_STATE_DISABLED, overridden: true, voltageV: 0 };

  test("reads exactly one feed with its controller state", () => {
    expect(feedReading({ snapshotReady: true, powerFeeds: [feed] }, FEED).reading).toMatchObject({
      state: "disabled",
      overridden: true,
      voltageV: 0,
      active: undefined,
    });
    expect(feedReading({ powerFeeds: [{ powerFeedId: FEED }] }, FEED).reading?.state).toBe("unknown");
  });

  test.each([
    [{ snapshotReady: false, powerFeeds: [feed] }, "snapshot_not_ready"],
    [{ snapshotReady: true, powerFeeds: [feed, feed] }, "duplicate_records"],
    [{ snapshotReady: true, powerFeeds: [] }, "not_in_snapshot"],
  ])("says why there is no reading", (response, reason) => {
    expect(feedReading(response, FEED).reason).toBe(reason);
  });
});

test("an active recovery cycle needs an ID", () => {
  expect(recoveryCycle({})).toBeNull();
  expect(recoveryCycle({ cycle: { id: " " } })).toBeNull();
  expect(
    recoveryCycle({
      cycle: { id: "cycle-1", phase: "holding", outletIds: [FEED], restoreAfter: "2026-11-16T12:00:30Z" },
    }),
  ).toEqual({
    phase: "holding",
    outletIds: [FEED],
    restoreAfter: new Date("2026-11-16T12:00:30Z"),
    lastError: "",
  });
});

describe("MdkV2Adapter.getModulePowerFeed", () => {
  const client = (overrides: Partial<MdkV2Client>) =>
    ({
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: map(healthy) }),
      powerServiceListPowerFeeds: vi.fn().mockResolvedValue({
        data: {
          snapshotReady: true,
          powerFeeds: [{ powerFeedId: FEED, state: V2PowerFeedState.POWER_FEED_STATE_ENABLED }],
        },
      }),
      powerServiceGetActivePowerRecoveryCycle: vi.fn().mockResolvedValue({ data: {} }),
      ...overrides,
    }) as unknown as MdkV2Client;

  const request = { tank: 1, module: 5, moduleId: "container:ct1:module:M-1-5" };

  test("reads the map, feeds and recovery cycle together", async () => {
    const inspection = await new MdkV2Adapter(client({})).getModulePowerFeed(request);
    expect(inspection).toMatchObject({
      feedId: FEED,
      confidence: "configured",
      reading: { state: "enabled" },
      recoveryCycle: null,
    });
    expect(inspection.observedAt).toBeInstanceOf(Date);
  });

  test("keeps failed side reads unknown rather than empty", async () => {
    const failed = vi.fn().mockRejectedValue(new Error("unavailable"));
    const inspection = await new MdkV2Adapter(
      client({
        powerServiceListPowerFeeds: failed,
        powerServiceGetActivePowerRecoveryCycle: failed,
      }),
    ).getModulePowerFeed(request);
    expect(inspection).toMatchObject({
      feedId: FEED,
      reading: undefined,
      readingUnavailableReason: "read_failed",
      recoveryCycle: undefined,
    });
  });

  test("an ID built from the position alone leaves the module identity unconfirmed", async () => {
    const inspection = await new MdkV2Adapter(client({})).getModulePowerFeed({
      ...request,
      moduleId: "container:ct1:tank:1:module:5",
    });
    expect(inspection.reasons).toEqual(["module_identity_unconfirmed"]);
  });

  test("a side read that never answers times out as unknown", async () => {
    vi.useFakeTimers();
    // Hangs until the adapter's deadline aborts it.
    const stalled = vi.fn(
      ({ signal }: { signal: AbortSignal }) =>
        new Promise((_, reject) => signal.addEventListener("abort", () => reject(signal.reason))),
    );
    const pending = new MdkV2Adapter(
      client({ powerServiceGetActivePowerRecoveryCycle: stalled as never }),
    ).getModulePowerFeed(request);
    await vi.advanceTimersByTimeAsync(POWER_FEED_READ_TIMEOUT_MS);
    expect(await pending).toMatchObject({ feedId: FEED, reading: { state: "enabled" }, recoveryCycle: undefined });
    vi.useRealTimers();
  });

  test("fails when the controller map cannot be read", async () => {
    const adapter = new MdkV2Adapter(
      client({ hashboardServiceGetContainerMap: vi.fn().mockRejectedValue(new Error("map unavailable")) }),
    );
    await expect(adapter.getModulePowerFeed(request)).rejects.toThrow("map unavailable");
  });
});
