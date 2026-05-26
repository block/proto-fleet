import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentLevel,
  CurtailmentMode,
  CurtailmentPriority,
  CurtailmentStrategy,
  FixedKwParamsSchema,
  GetActiveCurtailmentResponseSchema,
  ListCurtailmentEventsResponseSchema,
  ScopeWholeOrgSchema,
  StartCurtailmentRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useCurtailmentApi from "@/protoFleet/api/useCurtailmentApi";

const { mockHandleAuthErrors } = vi.hoisted(() => ({
  mockHandleAuthErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    getActiveCurtailment: vi.fn(),
    listCurtailmentEvents: vi.fn(),
    startCurtailment: vi.fn(),
    stopCurtailment: vi.fn(),
    updateCurtailmentEvent: vi.fn(),
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

interface CurtailmentEventOverrides {
  eventUuid?: string;
  state?: CurtailmentEventState;
  reason?: string;
  restoreBatchSize?: number;
  effectiveBatchSize?: number;
  startedAt?: CurtailmentEvent["startedAt"];
  scheduledStartAt?: CurtailmentEvent["scheduledStartAt"];
  createdAt?: CurtailmentEvent["createdAt"];
}

function createTimestamp(value: string): CurtailmentEvent["createdAt"] {
  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(new Date(value).getTime() / 1000)),
    nanos: 0,
  });
}

function createCurtailmentEvent(overrides: CurtailmentEventOverrides = {}): CurtailmentEvent {
  return create(CurtailmentEventSchema, {
    eventUuid: "curt-live",
    state: CurtailmentEventState.ACTIVE,
    mode: CurtailmentMode.FIXED_KW,
    strategy: CurtailmentStrategy.LEAST_EFFICIENT_FIRST,
    level: CurtailmentLevel.FULL,
    priority: CurtailmentPriority.NORMAL,
    scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) },
    modeParams: { case: "fixedKw", value: create(FixedKwParamsSchema, { targetKw: 25 }) },
    reason: "Live curtailment",
    restoreBatchSize: 5,
    restoreBatchIntervalSec: 60,
    ...overrides,
  });
}

describe("useCurtailmentApi", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockHandleAuthErrors.mockImplementation(
      ({ onError, error }: { onError?: (error: unknown) => void; error: unknown }) => {
        onError?.(error);
      },
    );
  });

  it("uses a generic load error while the backend returns unimplemented", async () => {
    vi.mocked(curtailmentClient.getActiveCurtailment).mockRejectedValue(
      new ConnectError("not implemented", Code.Unimplemented),
    );
    vi.mocked(curtailmentClient.listCurtailmentEvents).mockRejectedValue(
      new ConnectError("not implemented", Code.Unimplemented),
    );

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(result.current.refreshCurtailment()).rejects.toThrow("Failed to load curtailment events.");
    });

    expect(result.current.activeEvent).toBeUndefined();
    expect(result.current.events).toHaveLength(0);
  });

  it("does not mix fixture kW values into live backend events", async () => {
    const event = createCurtailmentEvent();
    vi.mocked(curtailmentClient.getActiveCurtailment).mockResolvedValue(
      create(GetActiveCurtailmentResponseSchema, { event }),
    );
    vi.mocked(curtailmentClient.listCurtailmentEvents).mockResolvedValue(
      create(ListCurtailmentEventsResponseSchema, { events: [event] }),
    );

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.estimatedReductionKw).toBe(0);
    expect(result.current.activeEvent?.remainingPowerKw).toBeUndefined();
    expect(result.current.activeEvent?.selectedMiners).toBe(0);
    expect(result.current.events[0]?.estimatedReductionKw).toBe(0);
  });

  it("uses the server effective restore batch size for active display", async () => {
    const event = createCurtailmentEvent({ restoreBatchSize: 5, effectiveBatchSize: 8 });
    vi.mocked(curtailmentClient.getActiveCurtailment).mockResolvedValue(
      create(GetActiveCurtailmentResponseSchema, { event }),
    );
    vi.mocked(curtailmentClient.listCurtailmentEvents).mockResolvedValue(
      create(ListCurtailmentEventsResponseSchema, { events: [event] }),
    );

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.activeEvent?.restoreBatchSize).toBe(8);
    expect(result.current.activeEvent?.rawEvent?.restoreBatchSize).toBe(5);
  });

  it("loads all event history pages", async () => {
    const firstEvent = createCurtailmentEvent({
      eventUuid: "curt-page-1",
      state: CurtailmentEventState.COMPLETED,
      reason: "First page",
    });
    const secondEvent = createCurtailmentEvent({
      eventUuid: "curt-page-2",
      state: CurtailmentEventState.COMPLETED,
      reason: "Second page",
    });
    vi.mocked(curtailmentClient.getActiveCurtailment).mockResolvedValue(create(GetActiveCurtailmentResponseSchema, {}));
    vi.mocked(curtailmentClient.listCurtailmentEvents)
      .mockResolvedValueOnce(
        create(ListCurtailmentEventsResponseSchema, { events: [firstEvent], nextPageToken: "next" }),
      )
      .mockResolvedValueOnce(create(ListCurtailmentEventsResponseSchema, { events: [secondEvent] }));

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(curtailmentClient.listCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(result.current.events.map((event) => event.id)).toEqual(["curt-page-1", "curt-page-2"]);
  });

  it("refetches history when active history rows remain after active curtailment clears", async () => {
    const staleActiveEvent = createCurtailmentEvent({
      eventUuid: "curt-refetch",
      state: CurtailmentEventState.ACTIVE,
      reason: "Stale active event",
    });
    const completedEvent = createCurtailmentEvent({
      eventUuid: "curt-refetch",
      state: CurtailmentEventState.COMPLETED,
      reason: "Stale active event",
    });
    vi.mocked(curtailmentClient.getActiveCurtailment).mockResolvedValue(create(GetActiveCurtailmentResponseSchema, {}));
    vi.mocked(curtailmentClient.listCurtailmentEvents)
      .mockResolvedValueOnce(create(ListCurtailmentEventsResponseSchema, { events: [staleActiveEvent] }))
      .mockResolvedValueOnce(create(ListCurtailmentEventsResponseSchema, { events: [completedEvent] }));

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(curtailmentClient.listCurtailmentEvents).toHaveBeenCalledTimes(2);
    expect(result.current.activeEvent).toBeUndefined();
    expect(result.current.events[0]?.id).toBe("curt-refetch");
    expect(result.current.events[0]?.state).toBe("completed");
  });

  it("passes selected event states to the history list requests", async () => {
    const event = createCurtailmentEvent({
      eventUuid: "curt-completed",
      state: CurtailmentEventState.COMPLETED,
      reason: "Completed event",
    });
    const cancelledEvent = createCurtailmentEvent({
      eventUuid: "curt-cancelled",
      state: CurtailmentEventState.CANCELLED,
      reason: "Cancelled event",
    });
    vi.mocked(curtailmentClient.getActiveCurtailment).mockResolvedValue(create(GetActiveCurtailmentResponseSchema, {}));
    vi.mocked(curtailmentClient.listCurtailmentEvents)
      .mockResolvedValueOnce(create(ListCurtailmentEventsResponseSchema, { events: [event] }))
      .mockResolvedValueOnce(create(ListCurtailmentEventsResponseSchema, { events: [cancelledEvent] }));

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment(["completed", "cancelled"]);
    });

    expect(curtailmentClient.listCurtailmentEvents).toHaveBeenCalledWith(
      expect.objectContaining({
        pageSize: 100,
        pageToken: "",
        stateFilter: CurtailmentEventState.COMPLETED,
      }),
    );
    expect(curtailmentClient.listCurtailmentEvents).toHaveBeenCalledWith(
      expect.objectContaining({
        pageSize: 100,
        pageToken: "",
        stateFilter: CurtailmentEventState.CANCELLED,
      }),
    );
    expect(result.current.events.map((historyEvent) => historyEvent.id)).toEqual(["curt-completed", "curt-cancelled"]);
  });

  it("keeps pending history timestamps distinct from started timestamps", async () => {
    const event = createCurtailmentEvent({
      eventUuid: "curt-scheduled",
      state: CurtailmentEventState.PENDING,
      reason: "Scheduled event",
      scheduledStartAt: createTimestamp("2026-05-01T10:00:00.000Z"),
      createdAt: createTimestamp("2026-04-30T09:00:00.000Z"),
    });
    vi.mocked(curtailmentClient.getActiveCurtailment).mockResolvedValue(create(GetActiveCurtailmentResponseSchema, {}));
    vi.mocked(curtailmentClient.listCurtailmentEvents).mockResolvedValue(
      create(ListCurtailmentEventsResponseSchema, { events: [event] }),
    );

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await result.current.refreshCurtailment();
    });

    expect(result.current.events[0]).toEqual(
      expect.objectContaining({
        id: "curt-scheduled",
        startedAt: undefined,
        scheduledAt: "2026-05-01T10:00:00.000Z",
        createdAt: "2026-04-30T09:00:00.000Z",
      }),
    );
  });

  it("does not mock successful actions when curtailment RPCs are unimplemented", async () => {
    vi.mocked(curtailmentClient.startCurtailment).mockRejectedValue(
      new ConnectError("not implemented", Code.Unimplemented),
    );

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(
        result.current.startCurtailment(create(StartCurtailmentRequestSchema, { reason: "Grid peak call" })),
      ).rejects.toThrow("Failed to start curtailment.");
    });

    expect(result.current.activeEvent).toBeUndefined();
  });

  it("routes mutation auth errors through the auth error handler", async () => {
    const authError = new ConnectError("session expired", Code.Unauthenticated);
    vi.mocked(curtailmentClient.startCurtailment).mockRejectedValue(authError);

    const { result } = renderHook(() => useCurtailmentApi());

    await act(async () => {
      await expect(
        result.current.startCurtailment(create(StartCurtailmentRequestSchema, { reason: "Grid peak call" })),
      ).rejects.toThrow("session expired");
    });

    expect(mockHandleAuthErrors).toHaveBeenCalledWith(expect.objectContaining({ error: authError }));
  });
});
