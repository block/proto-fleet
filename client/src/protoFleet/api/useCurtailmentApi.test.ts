import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, TimestampSchema } from "@bufbuild/protobuf/wkt";

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
} = vi.hoisted(() => ({
  mockGetActiveCurtailment: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListCurtailmentEvents: vi.fn(),
  mockStartCurtailment: vi.fn(),
  mockStopCurtailment: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    getActiveCurtailment: mockGetActiveCurtailment,
    listCurtailmentEvents: mockListCurtailmentEvents,
    startCurtailment: mockStartCurtailment,
    stopCurtailment: mockStopCurtailment,
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

  it("caps history refresh pagination", async () => {
    mockListCurtailmentEvents.mockImplementation(() => {
      const pageNumber = mockListCurtailmentEvents.mock.calls.length;

      return Promise.resolve({
        events: [curtailmentEvent({ eventUuid: `curt-page-${pageNumber}` })],
        nextPageToken: `page-${pageNumber + 1}`,
      });
    });

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(5);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual([
      "curt-page-1",
      "curt-page-2",
      "curt-page-3",
      "curt-page-4",
      "curt-page-5",
    ]);
  });

  it("stops history pagination when page tokens repeat", async () => {
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

    expect(mockListCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(mockListCurtailmentEvents.mock.calls.map(([request]) => request.pageToken)).toEqual(["", "page-2"]);
    expect(result.current.historyEvents.map((event) => event.id)).toEqual(["curt-page-1", "curt-page-2"]);
  });

  it("passes refresh abort signals to curtailment requests", async () => {
    const abortController = new AbortController();

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment({ signal: abortController.signal });
    });

    expect(mockGetActiveCurtailment.mock.calls[0][1]).toEqual({ signal: abortController.signal });
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
});
