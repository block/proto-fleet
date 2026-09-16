import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useContainerHistory } from "./useContainerHistory";
import type { ContainerHistoryResult } from "@/protoOS/api/adapters/containerHistory";
import useMinerStore from "@/protoOS/store/useMinerStore";
import type { Duration } from "@/shared/components/DurationSelector";

const mocks = vi.hoisted(() => ({ adapter: { getContainerHistory: vi.fn() } }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: true }),
}));

const history = (value: number): ContainerHistoryResult => ({
  series: { "power:": { points: [{ timestampMs: 1, value }], units: "W" } },
  unavailable: [],
});
const deferred = () => {
  let resolve!: (value: ContainerHistoryResult) => void;
  const promise = new Promise<ContainerHistoryResult>((accept) => (resolve = accept));
  return { promise, resolve };
};

describe("container history lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    useMinerStore.getState().resetDeviceData();
  });
  afterEach(() => vi.useRealTimers());

  it("aborts old periods and ignores late responses without adding another poll loop", async () => {
    const old = deferred();
    const current = deferred();
    mocks.adapter.getContainerHistory.mockReturnValueOnce(old.promise).mockReturnValue(current.promise);
    const { result, rerender, unmount } = renderHook(
      ({ duration }: { duration: Duration }) => useContainerHistory(duration, [{ metric: "power" }]),
      { initialProps: { duration: "1h" as Duration } },
    );
    const oldSignal = mocks.adapter.getContainerHistory.mock.calls[0][1].signal;
    rerender({ duration: "48h" });
    expect(oldSignal.aborted).toBe(true);
    expect(result.current.result).toBeUndefined();
    await act(async () => current.resolve(history(48)));
    await act(async () => old.resolve(history(1)));
    expect(result.current.result).toEqual(history(48));
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(3);
    unmount();
    expect(mocks.adapter.getContainerHistory.mock.calls[2][1].signal.aborted).toBe(true);
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(3);
  });

  it("rejects a response after device reset and fetches nothing for an unavailable mode", async () => {
    const pending = deferred();
    mocks.adapter.getContainerHistory.mockReturnValue(pending.promise);
    const { result, rerender } = renderHook(
      ({ enabled }) => useContainerHistory("12h", enabled ? [{ metric: "power" }] : []),
      { initialProps: { enabled: true } },
    );
    act(() => useMinerStore.getState().resetDeviceData());
    await act(async () => pending.resolve(history(10)));
    expect(result.current.result).toBeUndefined();
    rerender({ enabled: false });
    expect(result.current.pending).toBe(false);
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1);
  });

  it("retains same-period readings with an error on refresh failure", async () => {
    mocks.adapter.getContainerHistory.mockResolvedValueOnce(history(10)).mockRejectedValue(new Error("offline"));
    const { result } = renderHook(() => useContainerHistory("12h", [{ metric: "power" }]));
    await act(async () => {});
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.result).toEqual(history(10));
    expect(result.current.error).toContain("out of date");
    expect(result.current.pending).toBe(false);
  });
});
