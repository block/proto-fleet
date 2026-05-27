import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import { curtailmentClient } from "@/protoFleet/api/clients";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import { useCurtailmentPillData } from "@/protoFleet/components/PageHeader/useCurtailmentPillData";

const { mockGetActiveCurtailment, mockHandleAuthErrors, mockMapCurtailmentPillEvent } = vi.hoisted(() => ({
  mockGetActiveCurtailment: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockMapCurtailmentPillEvent: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    getActiveCurtailment: mockGetActiveCurtailment,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

vi.mock("./curtailmentPillMapper", () => ({
  mapCurtailmentPillEvent: mockMapCurtailmentPillEvent,
}));

const activeCurtailmentEvent: CurtailmentPillEvent = {
  reason: "Grid peak call",
  state: "active",
  scopeLabel: "Whole fleet",
  selectedMiners: 48,
  estimatedReductionKw: 126.4,
};

describe("useCurtailmentPillData", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    mockMapCurtailmentPillEvent.mockReturnValue(activeCurtailmentEvent);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not start overlapping polling requests", async () => {
    let resolveRequest: (value: { event: unknown }) => void = () => {};
    mockGetActiveCurtailment.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve;
        }),
    );

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveRequest({ event: {} });
    });

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(2);
  });

  it("aborts the active request when the hook unmounts", () => {
    mockGetActiveCurtailment.mockReturnValue(new Promise(() => {}));

    const { unmount } = renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });

    const requestOptions = mockGetActiveCurtailment.mock.calls[0][1] as { signal: AbortSignal };
    expect(requestOptions.signal.aborted).toBe(false);

    unmount();

    expect(requestOptions.signal.aborted).toBe(true);
  });

  it("refreshes immediately when curtailment changes", async () => {
    mockGetActiveCurtailment.mockResolvedValue({ event: {} });

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    await act(async () => {});

    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    act(() => {
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
    });
    await act(async () => {});

    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(2);
  });
});
