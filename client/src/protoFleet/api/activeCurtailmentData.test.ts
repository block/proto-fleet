import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  applyActiveCurtailmentEvent,
  dismissActiveCurtailmentEvent,
  fetchActiveCurtailmentData,
  getActiveCurtailmentSnapshot,
  refreshActiveCurtailmentData,
  resetActiveCurtailmentData,
  selectActiveCurtailmentEvent,
} from "@/protoFleet/api/activeCurtailmentData";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentTargetRollupSchema,
  CurtailmentTargetSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

const { mockListActiveCurtailments, mockGetCurtailmentEvent } = vi.hoisted(() => ({
  mockListActiveCurtailments: vi.fn(),
  mockGetCurtailmentEvent: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => {
  let activeEvents: CurtailmentEvent[] = [];

  return {
    curtailmentClient: {
      listActiveCurtailments: async (...args: unknown[]) => {
        const response = (await mockListActiveCurtailments(...args)) as {
          event?: CurtailmentEvent;
          events?: CurtailmentEvent[];
        };
        activeEvents = response.events ?? (response.event ? [response.event] : []);
        return { events: activeEvents };
      },
      getCurtailmentEvent: async (request: { eventUuid: string }, ...args: unknown[]) =>
        (await mockGetCurtailmentEvent(request, ...args)) ?? {
          event: activeEvents.find((event) => event.eventUuid === request.eventUuid),
        },
    },
  };
});

function curtailmentEvent(
  eventUuid: string,
  state = CurtailmentEventState.ACTIVE,
  overrides: Partial<CurtailmentEvent> = {},
): CurtailmentEvent {
  const event = create(CurtailmentEventSchema, { eventUuid, state });
  return Object.assign(event, overrides);
}

describe("activeCurtailmentData", () => {
  beforeEach(() => {
    resetActiveCurtailmentData();
    vi.clearAllMocks();
    mockGetCurtailmentEvent.mockReset();
  });

  it("keeps dismissed events suppressed when an older refresh is discarded", async () => {
    let resolveRefresh: (value: { event: CurtailmentEvent }) => void = () => {};
    mockListActiveCurtailments
      .mockReturnValueOnce(
        new Promise<{ event: CurtailmentEvent }>((resolve) => {
          resolveRefresh = resolve;
        }),
      )
      .mockResolvedValueOnce({ event: curtailmentEvent("dismissed-event") });

    const staleRefreshPromise = refreshActiveCurtailmentData();
    dismissActiveCurtailmentEvent("dismissed-event");
    resolveRefresh({ event: curtailmentEvent("different-event") });

    await staleRefreshPromise;
    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it("dismisses a terminal selected event without clearing other active curtailments", async () => {
    const activeEvent = curtailmentEvent("active-event", CurtailmentEventState.ACTIVE);
    const activeDetail = curtailmentEvent("active-event", CurtailmentEventState.ACTIVE, {
      decisionSnapshot: {
        estimated_reduction_kw: 5,
        selected_count: 2,
      },
    });
    const restoredEvent = curtailmentEvent("restored-event", CurtailmentEventState.COMPLETED);

    applyActiveCurtailmentEvent(activeEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(restoredEvent, { mergeActiveEvents: true });

    let snapshot = dismissActiveCurtailmentEvent(restoredEvent.eventUuid);

    expect(snapshot.event).toBeUndefined();
    expect(snapshot.events.map((event) => event.eventUuid)).toEqual([activeEvent.eventUuid]);

    mockListActiveCurtailments.mockResolvedValueOnce({ events: [restoredEvent, activeEvent] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: activeDetail });
    await refreshActiveCurtailmentData();
    snapshot = getActiveCurtailmentSnapshot();

    expect(snapshot.event?.eventUuid).toBe(activeEvent.eventUuid);
    expect(snapshot.events.map((event) => event.eventUuid)).toEqual([activeEvent.eventUuid]);
  });

  it("selects a remaining detailed active curtailment after terminal dismissal", () => {
    const detailedActiveEvent = curtailmentEvent("active-event", CurtailmentEventState.ACTIVE, {
      decisionSnapshot: {
        estimated_reduction_kw: 5,
        selected_count: 2,
      },
    });
    const restoredEvent = curtailmentEvent("restored-event", CurtailmentEventState.COMPLETED);

    applyActiveCurtailmentEvent(detailedActiveEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(restoredEvent, { mergeActiveEvents: true });

    const snapshot = dismissActiveCurtailmentEvent(restoredEvent.eventUuid);

    expect(snapshot.event?.eventUuid).toBe(detailedActiveEvent.eventUuid);
    expect(snapshot.events.map((event) => event.eventUuid)).toEqual([detailedActiveEvent.eventUuid]);
  });

  it("starts a fresh request after all shared request subscribers abort", async () => {
    mockListActiveCurtailments
      .mockImplementationOnce(
        (_request: unknown, options?: { signal?: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener(
              "abort",
              () => reject(new DOMException("The operation was aborted.", "AbortError")),
              { once: true },
            );
          }),
      )
      .mockResolvedValueOnce({ event: curtailmentEvent("fresh-event") });

    const abortController = new AbortController();
    const abortedRequest = fetchActiveCurtailmentData({ signal: abortController.signal }).catch((error) => error);

    abortController.abort();

    const freshRefresh = await fetchActiveCurtailmentData();

    expect(freshRefresh.event?.eventUuid).toBe("fresh-event");
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(2);
    await expect(abortedRequest).resolves.toBeInstanceOf(DOMException);
  });

  it("keeps a newer applied event when a later subscriber commits a stale shared refresh", async () => {
    let resolveRefresh: (value: { event?: CurtailmentEvent }) => void = () => undefined;
    mockListActiveCurtailments.mockReturnValueOnce(
      new Promise<{ event?: CurtailmentEvent }>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    const preMutationRefresh = fetchActiveCurtailmentData();
    applyActiveCurtailmentEvent(curtailmentEvent("started-event"));
    const postMutationRefresh = fetchActiveCurtailmentData();

    resolveRefresh({ event: undefined });
    const [preMutationSnapshot, postMutationSnapshot] = await Promise.all([preMutationRefresh, postMutationRefresh]);
    preMutationSnapshot.commit();
    postMutationSnapshot.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("started-event");
  });

  it("preserves a mutation-backed event until active polling catches up", async () => {
    const startedEvent = curtailmentEvent("started-event");
    applyActiveCurtailmentEvent(startedEvent, { preserveAgainstStaleRefresh: true });
    mockListActiveCurtailments
      .mockResolvedValueOnce({ event: undefined })
      .mockResolvedValueOnce({ event: undefined })
      .mockResolvedValueOnce({ event: startedEvent })
      .mockResolvedValueOnce({ event: undefined });

    const firstRefresh = fetchActiveCurtailmentData();
    const secondRefresh = fetchActiveCurtailmentData();
    const [firstSnapshot, secondSnapshot] = await Promise.all([firstRefresh, secondRefresh]);
    firstSnapshot.commit();
    secondSnapshot.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("started-event");

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("started-event");

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("started-event");

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it("clears a mutation-backed event when active polling keeps omitting it", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-05-01T10:00:00Z"));

    const restoringEvent = curtailmentEvent("stopping-event", CurtailmentEventState.RESTORING);

    try {
      applyActiveCurtailmentEvent(restoringEvent, { preserveAgainstStaleRefresh: true });
      mockListActiveCurtailments.mockResolvedValue({ event: undefined });

      await refreshActiveCurtailmentData();
      expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe(restoringEvent.eventUuid);

      await refreshActiveCurtailmentData();
      expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe(restoringEvent.eventUuid);

      await vi.advanceTimersByTimeAsync(30_001);
      await refreshActiveCurtailmentData();
      expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
    } finally {
      vi.useRealTimers();
    }
  });

  it("accepts same-state mutation-backed active polling once the event is visible", async () => {
    applyActiveCurtailmentEvent(
      curtailmentEvent("updated-event", CurtailmentEventState.ACTIVE, { reason: "Updated" }),
      {
        preserveAgainstStaleRefresh: true,
      },
    );
    mockListActiveCurtailments.mockResolvedValueOnce({
      event: curtailmentEvent("updated-event", CurtailmentEventState.ACTIVE, { reason: "Previous" }),
    });

    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event?.reason).toBe("Previous");
  });

  it("rejects same-event active polling that would move a mutation-backed event backward", async () => {
    applyActiveCurtailmentEvent(
      curtailmentEvent("stopping-event", CurtailmentEventState.RESTORING, { reason: "Stopping" }),
      {
        preserveAgainstStaleRefresh: true,
      },
    );
    mockListActiveCurtailments
      .mockResolvedValueOnce({
        event: curtailmentEvent("stopping-event", CurtailmentEventState.ACTIVE, { reason: "Still active" }),
      })
      .mockResolvedValueOnce({
        event: curtailmentEvent("stopping-event", CurtailmentEventState.RESTORING, { reason: "Server caught up" }),
      });

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toEqual(
      expect.objectContaining({
        reason: "Stopping",
        state: CurtailmentEventState.RESTORING,
      }),
    );

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toEqual(
      expect.objectContaining({
        reason: "Server caught up",
        state: CurtailmentEventState.RESTORING,
      }),
    );
  });

  it("preserves a mutation-backed event after selecting another active curtailment", async () => {
    const activeEvent = curtailmentEvent("active-event", CurtailmentEventState.ACTIVE, { reason: "Still active" });
    const selectedEvent = curtailmentEvent("selected-event", CurtailmentEventState.ACTIVE, { reason: "Selected" });
    const restoringEvent = curtailmentEvent("active-event", CurtailmentEventState.RESTORING, { reason: "Stopping" });
    applyActiveCurtailmentEvent(activeEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(selectedEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true, preserveAgainstStaleRefresh: true });
    applyActiveCurtailmentEvent(selectedEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments
      .mockResolvedValueOnce({ events: [selectedEvent, activeEvent] })
      .mockResolvedValueOnce({ events: [selectedEvent, restoringEvent] });

    await refreshActiveCurtailmentData();

    let snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event?.eventUuid).toBe("selected-event");
    expect(snapshot.events.find((event) => event.eventUuid === "active-event")).toEqual(
      expect.objectContaining({
        reason: "Stopping",
        state: CurtailmentEventState.RESTORING,
      }),
    );

    await refreshActiveCurtailmentData();

    snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.events.find((event) => event.eventUuid === "active-event")).toEqual(
      expect.objectContaining({
        reason: "Stopping",
        state: CurtailmentEventState.RESTORING,
      }),
    );
  });

  it("hydrates only the selected active curtailment before committing the active list", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    const otherSummary = curtailmentEvent("active-b", CurtailmentEventState.ACTIVE, { reason: "Summary B" });
    const activeDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Detail A" });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeSummary, otherSummary] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: activeDetail });

    await refreshActiveCurtailmentData();

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event?.reason).toBe("Detail A");
    expect(snapshot.events.map((event) => event.reason)).toEqual(["Detail A", "Summary B"]);
    expect(mockGetCurtailmentEvent).toHaveBeenCalledOnce();
    expect(mockGetCurtailmentEvent).toHaveBeenCalledWith(
      expect.objectContaining({ eventUuid: "active-a" }),
      expect.anything(),
    );
  });

  it("keeps current selected detail fields with fresh active-list state when active detail hydration fails", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.RESTORING, { reason: "Summary A" });
    const otherSummary = curtailmentEvent("active-b", CurtailmentEventState.ACTIVE, { reason: "Summary B" });
    const currentDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Current Detail A",
      decisionSnapshot: {
        estimated_reduction_kw: 5,
        selected_count: 2,
      },
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-1" })],
    });
    applyActiveCurtailmentEvent(currentDetail, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeSummary, otherSummary] });
    mockGetCurtailmentEvent.mockRejectedValueOnce(new Error("detail unavailable"));

    await refreshActiveCurtailmentData();

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event).toEqual(
      expect.objectContaining({
        eventUuid: "active-a",
        reason: "Summary A",
        state: CurtailmentEventState.RESTORING,
        decisionSnapshot: {
          estimated_reduction_kw: 5,
          selected_count: 2,
        },
      }),
    );
    expect(snapshot.event?.targets.map((target) => target.deviceIdentifier)).toEqual(["miner-1"]);
    expect(snapshot.events.map((event) => [event.eventUuid, event.state])).toEqual([
      ["active-a", CurtailmentEventState.RESTORING],
      ["active-b", CurtailmentEventState.ACTIVE],
    ]);
  });

  it("does not select an unhydrated active summary when active detail hydration fails", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    const otherSummary = curtailmentEvent("active-b", CurtailmentEventState.ACTIVE, { reason: "Summary B" });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeSummary, otherSummary] });
    mockGetCurtailmentEvent.mockRejectedValueOnce(new Error("detail unavailable"));

    await refreshActiveCurtailmentData();

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event).toBeUndefined();
    expect(snapshot.events.map((event) => event.reason)).toEqual(["Summary A", "Summary B"]);
  });

  it("surfaces auth failures from selected active detail hydration", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    applyActiveCurtailmentEvent(curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Current A" }), {
      mergeActiveEvents: true,
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeSummary] });
    mockGetCurtailmentEvent.mockRejectedValueOnce(new ConnectError("permission denied", Code.PermissionDenied));

    await expect(refreshActiveCurtailmentData()).rejects.toMatchObject({ code: Code.PermissionDenied });

    expect(getActiveCurtailmentSnapshot().event?.reason).toBe("Current A");
  });

  it("does not keep partial selected targets during active polling detail hydration", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    const firstPageDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Detail A",
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-1" })],
    });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeSummary] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: firstPageDetail, nextTargetPageToken: "page-2" });

    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event?.targets).toEqual([]);
    expect(mockGetCurtailmentEvent).toHaveBeenCalledOnce();
    expect(mockGetCurtailmentEvent.mock.calls.map(([request]) => request.targetPageToken)).toEqual([""]);
  });

  it("loads every selected active detail target page for explicit event selection", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    const firstPageDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Detail A",
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-1" })],
    });
    const secondPageDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Detail A",
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-2" })],
    });
    applyActiveCurtailmentEvent(activeSummary, { mergeActiveEvents: true });
    mockGetCurtailmentEvent
      .mockResolvedValueOnce({ event: firstPageDetail, nextTargetPageToken: "page-2" })
      .mockResolvedValueOnce({ event: secondPageDetail, nextTargetPageToken: "" });

    await selectActiveCurtailmentEvent(activeSummary.eventUuid);

    expect(getActiveCurtailmentSnapshot().event?.targets.map((target) => target.deviceIdentifier)).toEqual([
      "miner-1",
      "miner-2",
    ]);
    expect(mockGetCurtailmentEvent.mock.calls.map(([request]) => request.targetPageToken)).toEqual(["", "page-2"]);
  });

  it("does not reselect an active curtailment removed while explicit detail loads", async () => {
    const removedSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    const remainingSummary = curtailmentEvent("active-b", CurtailmentEventState.ACTIVE, { reason: "Summary B" });
    const removedDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Stale Detail A" });
    let resolveDetail: (value: { event: CurtailmentEvent }) => void = () => undefined;

    applyActiveCurtailmentEvent(removedSummary, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(remainingSummary, { mergeActiveEvents: true });
    mockGetCurtailmentEvent.mockReturnValueOnce(
      new Promise<{ event: CurtailmentEvent }>((resolve) => {
        resolveDetail = resolve;
      }),
    );

    const selectionPromise = selectActiveCurtailmentEvent(removedSummary.eventUuid);
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [remainingSummary] });
    await refreshActiveCurtailmentData();
    resolveDetail({ event: removedDetail });
    await selectionPromise;

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event?.eventUuid).toBe(remainingSummary.eventUuid);
    expect(snapshot.events.map((event) => event.eventUuid)).toEqual([remainingSummary.eventUuid]);
  });

  it("drops stale selected targets when polling detail is partial", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    const firstPageDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Detail A",
      decisionSnapshot: {
        estimated_reduction_kw: 5,
        selected_count: 2,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, { total: 2, dispatched: 1 }),
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-1" })],
    });
    const secondPageDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Detail A",
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-2" })],
    });
    const pollingDetail = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
      reason: "Polling Detail A",
      decisionSnapshot: {
        estimated_reduction_kw: 3,
        selected_count: 4,
      },
      targetRollup: create(CurtailmentTargetRollupSchema, { total: 4, confirmed: 3 }),
      targets: [create(CurtailmentTargetSchema, { deviceIdentifier: "miner-3" })],
    });
    applyActiveCurtailmentEvent(activeSummary, { mergeActiveEvents: true });
    mockGetCurtailmentEvent
      .mockResolvedValueOnce({ event: firstPageDetail, nextTargetPageToken: "page-2" })
      .mockResolvedValueOnce({ event: secondPageDetail, nextTargetPageToken: "" });
    await selectActiveCurtailmentEvent(activeSummary.eventUuid);

    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeSummary] });
    mockGetCurtailmentEvent.mockResolvedValueOnce({ event: pollingDetail, nextTargetPageToken: "page-2" });
    await refreshActiveCurtailmentData();

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event?.reason).toBe("Polling Detail A");
    expect(snapshot.event?.decisionSnapshot).toEqual({
      estimated_reduction_kw: 3,
      selected_count: 4,
    });
    expect(snapshot.event?.targetRollup).toEqual(create(CurtailmentTargetRollupSchema, { total: 4, confirmed: 3 }));
    expect(snapshot.event?.targets).toEqual([]);
  });

  it("keeps explicit detail hydration usable when target pagination exceeds the safety cap", async () => {
    const activeSummary = curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, { reason: "Summary A" });
    applyActiveCurtailmentEvent(activeSummary, { mergeActiveEvents: true });
    mockGetCurtailmentEvent.mockImplementation(async ({ targetPageToken }: { targetPageToken: string }) => ({
      event: curtailmentEvent("active-a", CurtailmentEventState.ACTIVE, {
        reason: "Detail A",
        decisionSnapshot: {
          estimated_reduction_kw: 5,
          selected_count: 2,
        },
        targets: [create(CurtailmentTargetSchema, { deviceIdentifier: targetPageToken || "miner-1" })],
      }),
      nextTargetPageToken: targetPageToken ? `${targetPageToken}-next` : "page-2",
    }));

    await selectActiveCurtailmentEvent(activeSummary.eventUuid);

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event).toEqual(
      expect.objectContaining({
        eventUuid: "active-a",
        reason: "Detail A",
        decisionSnapshot: {
          estimated_reduction_kw: 5,
          selected_count: 2,
        },
      }),
    );
    expect(snapshot.event?.targets).toEqual([]);
    expect(mockGetCurtailmentEvent).toHaveBeenCalledTimes(25);
  });

  it("preserves a selected restored curtailment while another active curtailment remains listed", async () => {
    const activeEvent = curtailmentEvent("active-event", CurtailmentEventState.ACTIVE);
    const restoredEvent = curtailmentEvent("restored-event", CurtailmentEventState.COMPLETED);

    applyActiveCurtailmentEvent(activeEvent, { mergeActiveEvents: true });
    applyActiveCurtailmentEvent(restoredEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeEvent] });

    await refreshActiveCurtailmentData();

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event?.eventUuid).toBe(restoredEvent.eventUuid);
    expect(snapshot.events.map((event) => event.eventUuid)).toEqual([activeEvent.eventUuid]);
  });

  it("drops a selected restoring curtailment from active-only refresh when it is no longer listed", async () => {
    const restoringEvent = curtailmentEvent("restoring-event", CurtailmentEventState.RESTORING);
    const activeEvent = curtailmentEvent("active-event", CurtailmentEventState.ACTIVE);

    applyActiveCurtailmentEvent(restoringEvent, { mergeActiveEvents: true });
    mockListActiveCurtailments.mockResolvedValueOnce({ events: [activeEvent] });

    await refreshActiveCurtailmentData();

    const snapshot = getActiveCurtailmentSnapshot();
    expect(snapshot.event?.eventUuid).toBe(activeEvent.eventUuid);
    expect(snapshot.events.map((event) => event.eventUuid)).toEqual([activeEvent.eventUuid]);
  });

  it("rejects a reset-aborted shared request as an AbortError", async () => {
    mockListActiveCurtailments.mockImplementationOnce(
      (_request: unknown, options?: { signal?: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => reject(new ConnectError("canceled", Code.Canceled)), {
            once: true,
          });
        }),
    );

    const pendingRefresh = refreshActiveCurtailmentData();
    resetActiveCurtailmentData();

    await expect(pendingRefresh).rejects.toBeInstanceOf(DOMException);
  });

  it("clears a restoring curtailment after an empty active response", async () => {
    applyActiveCurtailmentEvent(curtailmentEvent("restoring", CurtailmentEventState.RESTORING));
    mockListActiveCurtailments.mockResolvedValueOnce({ event: undefined });

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it("does not let stale empty refreshes clear a newer restoring event", async () => {
    let resolveStaleRefresh: (value: { event?: CurtailmentEvent }) => void = () => undefined;
    mockListActiveCurtailments
      .mockReturnValueOnce(
        new Promise<{ event?: CurtailmentEvent }>((resolve) => {
          resolveStaleRefresh = resolve;
        }),
      )
      .mockResolvedValueOnce({ event: undefined });

    const staleRefresh = fetchActiveCurtailmentData();
    applyActiveCurtailmentEvent(curtailmentEvent("restoring", CurtailmentEventState.RESTORING));
    resolveStaleRefresh({ event: undefined });

    const staleSnapshot = await staleRefresh;
    staleSnapshot.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("restoring");

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it.each([
    ["restored", CurtailmentEventState.COMPLETED],
    ["incomplete restore", CurtailmentEventState.COMPLETED_WITH_FAILURES],
  ])("preserves a %s curtailment through empty active responses until dismissal", async (eventUuid, state) => {
    applyActiveCurtailmentEvent(curtailmentEvent(eventUuid, state));
    mockListActiveCurtailments.mockResolvedValue({ event: undefined });

    await refreshActiveCurtailmentData();
    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe(eventUuid);

    dismissActiveCurtailmentEvent(eventUuid);
    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });
});
