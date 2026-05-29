import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { resetActiveCurtailmentData } from "@/protoFleet/api/activeCurtailmentData";
import { curtailmentClient } from "@/protoFleet/api/clients";
import { CURTAILMENT_CHANGED_EVENT } from "@/protoFleet/api/curtailmentEvents";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentMode,
  CurtailmentTargetRollupSchema,
  CurtailmentTargetSchema,
  CurtailmentTargetState,
  FixedKwParamsSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { useCurtailmentPillData } from "@/protoFleet/components/PageHeader/useCurtailmentPillData";

const { mockGetActiveCurtailment, mockHandleAuthErrors } = vi.hoisted(() => ({
  mockGetActiveCurtailment: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
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

function curtailmentEvent(overrides: Partial<CurtailmentEvent> = {}): CurtailmentEvent {
  const event = create(CurtailmentEventSchema, {
    eventUuid: "curt-1",
    reason: "Grid peak call",
    state: CurtailmentEventState.ACTIVE,
    mode: CurtailmentMode.FIXED_KW,
    scope: {
      case: "wholeOrg",
      value: create(ScopeWholeOrgSchema, {}),
    },
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 126.4 }),
    },
    targetRollup: create(CurtailmentTargetRollupSchema, {
      dispatched: 48,
      total: 48,
    }),
    targets: [
      create(CurtailmentTargetSchema, {
        state: CurtailmentTargetState.DISPATCHED,
        baselinePowerW: 126_400,
      }),
    ],
  });

  return Object.assign(event, overrides);
}

describe("useCurtailmentPillData", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetActiveCurtailmentData();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not start overlapping polling requests", async () => {
    let resolveRequest: (value: { event?: CurtailmentEvent }) => void = () => {};
    mockGetActiveCurtailment.mockImplementation(
      () =>
        new Promise<{ event?: CurtailmentEvent }>((resolve) => {
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
      resolveRequest({ event: undefined });
    });

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(2);
  });

  it("polls active curtailments more frequently", async () => {
    mockGetActiveCurtailment.mockResolvedValue({ event: curtailmentEvent() });

    renderHook(() => useCurtailmentPillData());

    act(() => {
      vi.advanceTimersByTime(0);
    });
    await act(async () => {});

    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(2_999);
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(1);
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
    mockGetActiveCurtailment.mockResolvedValue({ event: curtailmentEvent() });

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

  it("queues a fresh refresh when curtailment changes during an in-flight poll", async () => {
    let resolveFirstRequest: (value: { event: CurtailmentEvent }) => void = () => {};
    let resolveSecondRequest: (value: { event: CurtailmentEvent }) => void = () => {};
    mockGetActiveCurtailment
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
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    act(() => {
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
      window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveFirstRequest({ event: curtailmentEvent() });
      await Promise.resolve();
    });
    expect(curtailmentClient.getActiveCurtailment).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveSecondRequest({ event: curtailmentEvent() });
    });
  });
});
