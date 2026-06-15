import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { applyActiveCurtailmentEvent, resetActiveCurtailmentData } from "@/protoFleet/api/activeCurtailmentData";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { useCurtailmentPillData } from "@/protoFleet/components/PageHeader/useCurtailmentPillData";

const { mockListActiveCurtailments, mockHandleAuthErrors, mockUseHasPermission } = vi.hoisted(() => ({
  mockListActiveCurtailments: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockUseHasPermission: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: (() => {
    let activeEvents: CurtailmentEvent[] = [];

    return {
      listActiveCurtailments: async (...args: unknown[]) => {
        const response = (await mockListActiveCurtailments(...args)) as {
          event?: CurtailmentEvent;
          events?: CurtailmentEvent[];
        };
        activeEvents = response.events ?? (response.event ? [response.event] : []);
        return { events: activeEvents };
      },
      getCurtailmentEvent: async (request: { eventUuid: string }) => ({
        event: activeEvents.find((event) => event.eventUuid === request.eventUuid),
      }),
    };
  })(),
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
  useHasPermission: mockUseHasPermission,
}));

function curtailmentEvent(
  eventUuid = "curt-1",
  state = CurtailmentEventState.ACTIVE,
  reason = "Grid peak call",
): CurtailmentEvent {
  return create(CurtailmentEventSchema, {
    eventUuid,
    reason,
    state,
  });
}

describe("useCurtailmentPillData", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetActiveCurtailmentData();
    vi.clearAllMocks();
    mockUseHasPermission.mockReturnValue(true);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not start overlapping polling requests", async () => {
    let resolveRequest: (value: { event?: CurtailmentEvent }) => void = () => {};
    mockListActiveCurtailments.mockImplementation(
      () =>
        new Promise<{ event?: CurtailmentEvent }>((resolve) => {
          resolveRequest = resolve;
        }),
    );

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveRequest({ event: undefined });
    });

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(2);
  });

  it("does not poll or surface cached events without curtailment read permission", async () => {
    applyActiveCurtailmentEvent(curtailmentEvent());
    mockUseHasPermission.mockReturnValue(false);

    const { result } = renderHook(() => useCurtailmentPillData());

    expect(result.current.activeEvent).toBeNull();

    act(() => {
      vi.advanceTimersByTime(30_000);
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
    });

    expect(mockUseHasPermission).toHaveBeenCalledWith("curtailment:read");
    expect(mockListActiveCurtailments).not.toHaveBeenCalled();
  });

  it("polls active curtailments more frequently", async () => {
    mockListActiveCurtailments.mockResolvedValue({ event: curtailmentEvent() });

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    await act(async () => {});

    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(2_999);
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(1);
    });

    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(2);
  });

  it("uses listed active events when the selected event is terminal", async () => {
    const listedActiveEvent = curtailmentEvent("curt-active", CurtailmentEventState.ACTIVE, "Active grid call");
    const restoredSelectedEvent = curtailmentEvent(
      "curt-restored",
      CurtailmentEventState.COMPLETED,
      "Restored grid call",
    );
    act(() => {
      applyActiveCurtailmentEvent(listedActiveEvent, { mergeActiveEvents: true });
      applyActiveCurtailmentEvent(restoredSelectedEvent, { mergeActiveEvents: true });
    });

    const { result } = renderHook(() => useCurtailmentPillData());

    expect(result.current.activeEvent?.reason).toBe("Active grid call");
    expect(result.current.activeEvent?.targetMetricsAvailable).toBe(false);

    act(() => {
      vi.advanceTimersByTime(0);
    });
    await act(async () => {});
    expect(mockListActiveCurtailments).toHaveBeenCalledOnce();
    mockListActiveCurtailments.mockClear();

    act(() => {
      vi.advanceTimersByTime(2_999);
    });
    expect(mockListActiveCurtailments).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledOnce();
  });

  it("aborts the active request when the hook unmounts", () => {
    mockListActiveCurtailments.mockReturnValue(new Promise(() => {}));

    const { unmount } = renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });

    const requestOptions = mockListActiveCurtailments.mock.calls[0][1] as { signal: AbortSignal };
    expect(requestOptions.signal.aborted).toBe(false);

    unmount();

    expect(requestOptions.signal.aborted).toBe(true);
  });

  it("refreshes immediately when curtailment changes", async () => {
    mockListActiveCurtailments.mockResolvedValue({ event: curtailmentEvent() });

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    await act(async () => {});

    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    act(() => {
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
    });
    await act(async () => {});

    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(2);
  });

  it("preserves the cached active event when a refresh fails", async () => {
    mockListActiveCurtailments
      .mockResolvedValueOnce({ event: curtailmentEvent() })
      .mockRejectedValueOnce(new Error("load failed"));

    const { result } = renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    await act(async () => {});

    expect(result.current.activeEvent?.reason).toBe("Grid peak call");

    await act(async () => {
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
      await Promise.resolve();
    });

    expect(result.current.activeEvent?.reason).toBe("Grid peak call");
    expect(mockHandleAuthErrors).toHaveBeenCalledOnce();
  });

  it("queues a fresh refresh when curtailment changes during an in-flight poll", async () => {
    let resolveFirstRequest: (value: { event: CurtailmentEvent }) => void = () => {};
    let resolveSecondRequest: (value: { event: CurtailmentEvent }) => void = () => {};
    mockListActiveCurtailments
      .mockImplementationOnce(
        () =>
          new Promise<{ event: CurtailmentEvent }>((resolve) => {
            resolveFirstRequest = resolve;
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise<{ event: CurtailmentEvent }>((resolve) => {
            resolveSecondRequest = resolve;
          }),
      );

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    act(() => {
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveFirstRequest({ event: curtailmentEvent() });
      await Promise.resolve();
    });
    expect(mockListActiveCurtailments).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveSecondRequest({ event: curtailmentEvent() });
    });
  });
});
