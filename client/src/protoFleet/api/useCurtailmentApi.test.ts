import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, TimestampSchema } from "@bufbuild/protobuf/wkt";

import { applyActiveCurtailmentEvent, resetActiveCurtailmentData } from "@/protoFleet/api/activeCurtailmentData";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentMode,
  CurtailmentPriority,
  CurtailmentTargetRollupSchema,
  CurtailmentTargetSchema,
  CurtailmentTargetState,
  FixedKwParamsSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

const {
  mockGetActiveCurtailment,
  mockHandleAuthErrors,
  mockListCurtailmentEvents,
  mockStartCurtailment,
  mockStopCurtailment,
  mockUpdateCurtailment,
} = vi.hoisted(() => ({
  mockGetActiveCurtailment: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListCurtailmentEvents: vi.fn(),
  mockStartCurtailment: vi.fn(),
  mockStopCurtailment: vi.fn(),
  mockUpdateCurtailment: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    getActiveCurtailment: mockGetActiveCurtailment,
    listCurtailmentEvents: mockListCurtailmentEvents,
    startCurtailment: mockStartCurtailment,
    stopCurtailment: mockStopCurtailment,
    updateCurtailmentEvent: mockUpdateCurtailment,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

const baseSubmitValues: CurtailmentSubmitValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: "customPlan",
  curtailmentMode: "fixedKwReduction",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "5",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "10",
  restoreIntervalSec: "60",
  reason: "Grid peak",
  includeMaintenance: false,
};

function timestamp(isoDate: string): Timestamp {
  const date = new Date(isoDate);
  const milliseconds = date.getTime();

  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(milliseconds / 1000)),
    nanos: (milliseconds % 1000) * 1_000_000,
  });
}

function curtailmentEvent(overrides: Partial<CurtailmentEvent> = {}): CurtailmentEvent {
  const event = create(CurtailmentEventSchema, {
    eventUuid: "curt-1",
    reason: "Grid peak",
    state: CurtailmentEventState.ACTIVE,
    mode: CurtailmentMode.FIXED_KW,
    priority: CurtailmentPriority.EMERGENCY,
    scope: {
      case: "wholeOrg",
      value: create(ScopeWholeOrgSchema, {}),
    },
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 5 }),
    },
    effectiveBatchSize: 10,
    restoreBatchIntervalSec: 60,
    targetRollup: create(CurtailmentTargetRollupSchema, {
      confirmed: 1,
      dispatched: 1,
      total: 2,
    }),
    targets: [
      create(CurtailmentTargetSchema, {
        state: CurtailmentTargetState.CONFIRMED,
        baselinePowerW: 3000,
        observedPowerW: 500,
      }),
      create(CurtailmentTargetSchema, {
        state: CurtailmentTargetState.DISPATCHED,
        baselinePowerW: 3000,
        observedPowerW: 500,
      }),
    ],
    decisionSnapshot: {
      estimated_reduction_kw: 6.2,
      selected_count: 2,
    },
    startedAt: timestamp("2026-05-01T12:00:00Z"),
    createdAt: timestamp("2026-05-01T11:58:00Z"),
  });

  return Object.assign(event, overrides);
}

describe("useCurtailmentApi", () => {
  beforeEach(() => {
    resetActiveCurtailmentData();
    vi.clearAllMocks();
    mockHandleAuthErrors.mockImplementation(({ onError }: { error: unknown; onError?: (error: unknown) => void }) =>
      onError?.(new Error("auth error")),
    );
    mockGetActiveCurtailment.mockResolvedValue({ event: undefined });
    mockListCurtailmentEvents.mockResolvedValue({ events: [], nextPageToken: "" });
  });

  it("loads and maps active curtailment plus history", async () => {
    const activeEvent = curtailmentEvent();
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-2",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEventId).toBe("curt-1");
    expect(result.current.activeEvent).toEqual(
      expect.objectContaining({
        reason: "Grid peak",
        state: "active",
        scopeLabel: "Whole fleet",
        selectedMiners: 2,
        estimatedReductionKw: 6.2,
        targetKw: 5,
        observedReductionKw: 5,
        remainingPowerKw: 1,
      }),
    );
    expect(result.current.activeEventFormValues).toEqual(
      expect.objectContaining({
        reason: "Grid peak",
        scopeType: "wholeOrg",
        targetKw: "5",
        priority: "emergency",
        restoreBatchSize: "",
        restoreIntervalSec: "60",
      }),
    );
    expect(result.current.activeEvent?.rollups).toEqual([
      { state: "dispatched", count: 1 },
      { state: "confirmed", count: 1 },
    ]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-1", "curt-2"]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        priority: "emergency",
        sourceLabel: "Manual",
        startedAt: "2026-05-01T12:00:00.000Z",
      }),
    );
  });

  it("estimates observed reduction from confirmed targets when telemetry is absent", async () => {
    const activeEvent = curtailmentEvent({
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        confirmed: 1,
        pending: 1,
        total: 3,
      }),
      targets: [
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.DISPATCHED,
          baselinePowerW: 3000,
        }),
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.CONFIRMED,
          baselinePowerW: 3000,
        }),
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.PENDING,
          baselinePowerW: 3000,
        }),
      ],
      decisionSnapshot: {
        estimated_reduction_kw: 6.2,
        selected_count: 3,
      },
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.observedReductionKw).toBeCloseTo(2.07);
  });

  it("shows the effective restore batch size while preserving the configured form value", async () => {
    const activeEvent = curtailmentEvent({
      effectiveBatchSize: 10,
      restoreBatchSize: 1,
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.restoreBatchSize).toBe(10);
    expect(result.current.activeEventFormValues?.restoreBatchSize).toBe("1");
  });

  it("maps all-pending events without telemetry to zero observed reduction", async () => {
    const activeEvent = curtailmentEvent({
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
      targets: [
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.PENDING,
          baselinePowerW: 3000,
        }),
        create(CurtailmentTargetSchema, {
          state: CurtailmentTargetState.PENDING,
          baselinePowerW: 3000,
        }),
      ],
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.observedReductionKw).toBe(0);
  });

  it("uses the active display state for the injected active history row", async () => {
    const activeEvent = curtailmentEvent({
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        pending: 1,
        total: 2,
      }),
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        state: "pending",
        displayState: "curtailing",
      }),
    );
  });

  it("keeps server-scrubbed history metadata when injecting active display state", async () => {
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-scrubbed-source",
      externalSource: "webhook-secret",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        pending: 1,
        total: 2,
      }),
    });
    const historyEvent = curtailmentEvent({
      eventUuid: "curt-scrubbed-source",
      externalSource: "",
      state: CurtailmentEventState.PENDING,
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [historyEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-scrubbed-source",
        displayState: "curtailing",
        sourceLabel: "Manual",
      }),
    );
  });

  it("uses a scrubbed source label for injected active history rows without a server history match", async () => {
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-unmatched-source",
      externalSource: "webhook-secret",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-unmatched-source",
        sourceLabel: "Manual",
      }),
    );
  });

  it("loads only the first history page and paginates on demand", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "",
      });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(1);
    expect(mockListCurtailmentEvents.mock.calls[0][0]).toEqual(
      expect.objectContaining({ pageSize: 50, pageToken: "" }),
    );
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-1"]);
    expect(result.current.historyCurrentPage).toBe(0);
    expect(result.current.historyHasPreviousPage).toBe(false);
    expect(result.current.historyHasNextPage).toBe(true);

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(mockListCurtailmentEvents.mock.calls[1][0]).toEqual(
      expect.objectContaining({ pageSize: 50, pageToken: "page-2" }),
    );
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-2"]);
    expect(result.current.historyCurrentPage).toBe(1);
    expect(result.current.historyHasPreviousPage).toBe(true);
    expect(result.current.historyHasNextPage).toBe(false);
  });

  it("stops exposing next history pages when page tokens repeat", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "page-2",
      });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyHasNextPage).toBe(true);

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual(["", "page-2"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-2"]);
    expect(result.current.historyHasNextPage).toBe(false);
  });

  it("sends server history status filters and resets pagination", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "",
      })
      .mockResolvedValueOnce({
        events: [
          curtailmentEvent({
            eventUuid: "curt-completed",
            state: CurtailmentEventState.COMPLETED,
          }),
        ],
        nextPageToken: "",
      });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    await act(async () => {
      await result.current.setHistoryStatusFilters(["completed", "failed"]);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(3);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual(["", "page-2", ""]);
    expect(mockListCurtailmentEvents.mock.calls[2][0]).toEqual(
      expect.objectContaining({
        pageSize: 50,
        stateFilters: [CurtailmentEventState.COMPLETED, CurtailmentEventState.FAILED],
      }),
    );
    expect(result.current.historyCurrentPage).toBe(0);
    expect(result.current.historyStatusFilters).toEqual(["completed", "failed"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-completed"]);
  });

  it("does not prepend a non-matching active event to filtered history", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-active", state: CurtailmentEventState.ACTIVE });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-completed",
      state: CurtailmentEventState.COMPLETED,
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: activeEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["completed"]);
    });

    expect(result.current.activeEventId).toBe("curt-active");
    expect(result.current.historyStatusFilters).toEqual(["completed"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-completed"]);
  });

  it("reconciles terminal active state when current history filters exclude terminal rows", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-filtered-terminal",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-filtered-terminal",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: undefined });
    mockListCurtailmentEvents
      .mockResolvedValueOnce({ events: [], nextPageToken: "" })
      .mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(mockListCurtailmentEvents.mock.calls[0][0].stateFilters).toEqual([CurtailmentEventState.RESTORING]);
    expect(mockListCurtailmentEvents.mock.calls[1][0].stateFilters).toEqual([]);
    expect(result.current.activeEventId).toBe("curt-filtered-terminal");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([]);
  });

  it("refreshes history without refetching active curtailment when requested", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-active" });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-completed",
      state: CurtailmentEventState.COMPLETED,
    });
    applyActiveCurtailmentEvent(activeEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [completedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(mockGetActiveCurtailment).not.toHaveBeenCalled();
    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(1);
    expect(result.current.activeEventId).toBe("curt-active");
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-active", "curt-completed"]);
  });

  it("does not apply active reconciliation from superseded history refreshes", async () => {
    const activeEvent = curtailmentEvent({ eventUuid: "curt-superseded" });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-superseded",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 2,
        total: 2,
      }),
      targets: [],
    });
    let resolveSupersededHistory: (value: { events: CurtailmentEvent[]; nextPageToken: string }) => void = () =>
      undefined;
    mockListCurtailmentEvents
      .mockReturnValueOnce(
        new Promise<{ events: CurtailmentEvent[]; nextPageToken: string }>((resolve) => {
          resolveSupersededHistory = resolve;
        }),
      )
      .mockResolvedValueOnce({ events: [activeEvent], nextPageToken: "" });
    applyActiveCurtailmentEvent(activeEvent);

    const { result } = renderHook(() => useCurtailmentApi());

    let supersededRefresh!: Promise<unknown>;
    act(() => {
      supersededRefresh = result.current.refreshCurtailment({ includeActive: false });
    });
    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEvent?.state).toBe("active");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-superseded",
        state: "active",
      }),
    );

    resolveSupersededHistory({ events: [completedEvent], nextPageToken: "" });
    await act(async () => {
      await supersededRefresh;
    });

    expect(result.current.activeEvent?.state).toBe("active");
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-superseded",
        state: "active",
      }),
    );
  });

  it("uses the shared active curtailment snapshot for active fields and current history", async () => {
    const pendingEvent = curtailmentEvent({
      eventUuid: "curt-shared",
      reason: "Queued dispatch",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        pending: 2,
        total: 2,
      }),
    });
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-shared",
      reason: "Dispatch started",
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: pendingEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [pendingEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.reason).toBe("Queued dispatch");
    expect(result.current.historyEvents[0].reason).toBe("Queued dispatch");

    act(() => {
      applyActiveCurtailmentEvent(activeEvent);
    });

    expect(result.current.activeEvent?.reason).toBe("Dispatch started");
    expect(result.current.historyEvents[0].reason).toBe("Dispatch started");
  });

  it("drops stale injected active history rows when the shared active event changes", async () => {
    const pendingEvent = curtailmentEvent({
      eventUuid: "curt-shared-a",
      reason: "Queued dispatch",
      state: CurtailmentEventState.PENDING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        dispatched: 1,
        pending: 1,
        total: 2,
      }),
    });
    const activeEvent = curtailmentEvent({
      eventUuid: "curt-shared-b",
      reason: "Dispatch started",
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: pendingEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [pendingEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-shared-a",
        displayState: "curtailing",
      }),
    ]);

    act(() => {
      applyActiveCurtailmentEvent(activeEvent);
    });

    expect(result.current.activeEventId).toBe("curt-shared-b");
    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-shared-b",
      }),
    ]);
  });

  it("removes injected active history rows when shared active state stops matching filters", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-filtered-active",
      state: CurtailmentEventState.RESTORING,
    });
    const completedEvent = curtailmentEvent({
      eventUuid: "curt-filtered-active",
      state: CurtailmentEventState.COMPLETED,
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-filtered-active"]);

    act(() => {
      applyActiveCurtailmentEvent(completedEvent);
    });

    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([]);
  });

  it("keeps real history rows when removing stale injected active rows from filtered history", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-injected",
      state: CurtailmentEventState.RESTORING,
    });
    const realHistoryEvent = curtailmentEvent({
      eventUuid: "curt-real-history",
      reason: "Real history row",
      state: CurtailmentEventState.RESTORING,
    });
    const filteredActiveEvent = curtailmentEvent({
      eventUuid: "curt-real-history",
      state: CurtailmentEventState.PENDING,
    });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [realHistoryEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.setHistoryStatusFilters(["restoring"]);
    });

    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-injected", "curt-real-history"]);

    act(() => {
      applyActiveCurtailmentEvent(filteredActiveEvent);
    });

    expect(result.current.historyStatusFilters).toEqual(["restoring"]);
    expect(result.current.historyEvents).toEqual([
      expect.objectContaining({
        id: "curt-real-history",
        reason: "Real history row",
      }),
    ]);
  });

  it("keeps a restored curtailment visible until it is dismissed", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-restored",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const restoredEvent = curtailmentEvent({
      eventUuid: "curt-restored",
      state: CurtailmentEventState.COMPLETED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: undefined,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 2,
        total: 2,
      }),
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [restoredEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBe("curt-restored");
    expect(result.current.activeEvent?.state).toBe("completed");
    expect(result.current.activeEvent?.endedAt).toBe("2026-05-01T13:00:00.000Z");
    expect(result.current.activeEvent?.remainingPowerKw).toBe(1);
    expect(result.current.activeEvent?.rollups).toEqual([{ state: "resolved", count: 2 }]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-restored",
        state: "completed",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("displayState");

    act(() => {
      result.current.dismissTerminalCurtailment();
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
  });

  it("keeps an incomplete restore visible until it is dismissed", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-restore-incomplete",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const restoreIncompleteEvent = curtailmentEvent({
      eventUuid: "curt-restore-incomplete",
      state: CurtailmentEventState.COMPLETED_WITH_FAILURES,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: undefined,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        resolved: 1,
        restoreFailed: 1,
        total: 2,
      }),
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [restoreIncompleteEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBe("curt-restore-incomplete");
    expect(result.current.activeEvent?.state).toBe("completedWithFailures");
    expect(result.current.activeEvent?.remainingPowerKw).toBe(1);
    expect(result.current.activeEvent?.rollups).toEqual([
      { state: "resolved", count: 1 },
      { state: "restoreFailed", count: 1 },
    ]);
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-restore-incomplete",
        state: "completedWithFailures",
      }),
    );
    expect(result.current.historyEvents[0]).not.toHaveProperty("displayState");

    act(() => {
      result.current.dismissTerminalCurtailment();
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
  });

  it("does not preserve stale restoring rollups when terminal history is trimmed", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-trimmed-restore",
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    const trimmedTerminalEvent = curtailmentEvent({
      eventUuid: "curt-trimmed-restore",
      state: CurtailmentEventState.COMPLETED_WITH_FAILURES,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      targetRollup: undefined,
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [trimmedTerminalEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBe("curt-trimmed-restore");
    expect(result.current.activeEvent?.state).toBe("completedWithFailures");
    expect(result.current.activeEvent?.rollups).toEqual([]);
    expect(result.current.activeEvent?.remainingPowerKw).toBeUndefined();
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-trimmed-restore",
        state: "completedWithFailures",
      }),
    );
  });

  it("clears an active snapshot when history reports a failed terminal event", async () => {
    const restoringEvent = curtailmentEvent({
      eventUuid: "curt-failed",
      state: CurtailmentEventState.RESTORING,
    });
    const failedEvent = curtailmentEvent({
      eventUuid: "curt-failed",
      state: CurtailmentEventState.FAILED,
      endedAt: timestamp("2026-05-01T13:00:00Z"),
      decisionSnapshot: undefined,
      targets: [],
    });
    applyActiveCurtailmentEvent(restoringEvent);
    mockListCurtailmentEvents.mockResolvedValueOnce({ events: [failedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ includeActive: false });
    });

    expect(result.current.activeEventId).toBeNull();
    expect(result.current.activeEvent).toBeNull();
    expect(result.current.historyEvents[0]).toEqual(
      expect.objectContaining({
        id: "curt-failed",
        state: "failed",
      }),
    );
  });

  it("keeps non-first history pages stable when mutation refresh fails", async () => {
    mockListCurtailmentEvents
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-1" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        events: [curtailmentEvent({ eventUuid: "curt-page-2" })],
        nextPageToken: "",
      })
      .mockRejectedValueOnce(new Error("refresh failed"));
    mockStartCurtailment.mockResolvedValueOnce({ event: curtailmentEvent({ eventUuid: "curt-new" }) });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    await act(async () => {
      await result.current.goToHistoryPage(1);
    });

    await act(async () => {
      await result.current.startCurtailment(baseSubmitValues);
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(3);
    expect(result.current.activeEventId).toBe("curt-new");
    expect(result.current.historyCurrentPage).toBe(1);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-2"]);
    expect(result.current.loadError).toBe("refresh failed");
  });

  it("passes refresh abort signals to history requests and uses a shared active request signal", async () => {
    const abortController = new AbortController();

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ signal: abortController.signal });
    });

    expect(mockGetActiveCurtailment.mock.calls[0][1]).toEqual({ signal: expect.any(AbortSignal) });
    expect(mockListCurtailmentEvents.mock.calls[0][1]).toEqual({ signal: abortController.signal });
  });

  it("starts and stops curtailment with refresh events", async () => {
    const changedListener = vi.fn();
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
    const startedEvent = curtailmentEvent();
    const restoringEvent = curtailmentEvent({
      state: CurtailmentEventState.RESTORING,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        resolved: 1,
        total: 2,
      }),
    });
    mockStartCurtailment.mockResolvedValueOnce({ event: startedEvent });
    mockStopCurtailment.mockResolvedValueOnce({ event: restoringEvent });
    mockGetActiveCurtailment.mockResolvedValue({ event: startedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [startedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.startCurtailment(baseSubmitValues);
    });

    expect(mockStartCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        reason: "Grid peak",
        mode: CurtailmentMode.FIXED_KW,
      }),
    );
    expect(changedListener).toHaveBeenCalledTimes(1);
    expect(result.current.activeEvent?.state).toBe("active");

    mockGetActiveCurtailment.mockResolvedValue({ event: restoringEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [restoringEvent], nextPageToken: "" });

    await act(async () => {
      await result.current.stopCurtailment("curt-1");
    });

    expect(mockStopCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        force: false,
      }),
    );
    expect(changedListener).toHaveBeenCalledTimes(2);
    expect(result.current.activeEvent?.state).toBe("restoring");

    window.removeEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
  });

  it("updates active curtailment fields and refreshes listeners", async () => {
    const changedListener = vi.fn();
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
    const updatedEvent = curtailmentEvent({ reason: "Updated grid peak", restoreBatchIntervalSec: 120 });
    mockUpdateCurtailment.mockResolvedValueOnce({ event: updatedEvent });
    mockGetActiveCurtailment.mockResolvedValue({ event: updatedEvent });
    mockListCurtailmentEvents.mockResolvedValue({ events: [updatedEvent], nextPageToken: "" });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.updateCurtailment(
        "curt-1",
        {
          ...baseSubmitValues,
          reason: " Updated grid peak ",
          maxDurationSec: "1800",
          restoreBatchSize: "",
          restoreIntervalSec: "120",
        },
        {
          ...baseSubmitValues,
          reason: "Grid peak",
          maxDurationSec: "",
          restoreBatchSize: "",
          restoreIntervalSec: "60",
        },
      );
    });

    const updateRequest = mockUpdateCurtailment.mock.calls[0][0];
    expect(mockUpdateCurtailment).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: "curt-1",
        reason: "Updated grid peak",
        maxDurationSeconds: 1800,
        restoreBatchIntervalSec: 120,
      }),
    );
    expect(updateRequest.restoreBatchSize).toBeUndefined();
    expect(changedListener).toHaveBeenCalledTimes(1);
    expect(result.current.activeEvent?.reason).toBe("Updated grid peak");
    expect(result.current.activeEventFormValues?.restoreIntervalSec).toBe("120");

    window.removeEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
  });

  it("surfaces update failures without refreshing listeners", async () => {
    const changedListener = vi.fn();
    window.addEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
    mockUpdateCurtailment.mockRejectedValueOnce(new Error("rpc failed"));

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(result.current.updateCurtailment("curt-1", baseSubmitValues, baseSubmitValues)).rejects.toThrow(
        "rpc failed",
      );
    });

    expect(result.current.updateError).toBe("rpc failed");
    expect(changedListener).not.toHaveBeenCalled();

    window.removeEventListener(CURTAILMENT_CHANGED_EVENT, changedListener);
  });
});
