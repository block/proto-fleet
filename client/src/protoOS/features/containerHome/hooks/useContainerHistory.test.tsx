import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useContainerHistory } from "./useContainerHistory";
import type { ContainerHistoryResult } from "@/protoOS/api/adapters/containerHistory";
import { queryContainerHistory } from "@/protoOS/api/adapters/mdkV2/containerHistory";
import type { MdkV2Client } from "@/protoOS/api/adapters/mdkV2/MdkV2Adapter";
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
    vi.resetAllMocks();
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

  it.each([new TypeError("Failed to fetch"), { status: 503 }])(
    "retains history and its window when the adapter converts %s into unavailable results",
    async (error) => {
      const client = { telemetryServiceQueryTimeSeries: vi.fn().mockRejectedValue(error) } as unknown as MdkV2Client;
      mocks.adapter.getContainerHistory
        .mockResolvedValueOnce(history(10))
        .mockImplementation((request, options) => queryContainerHistory(client, request, options));
      const { result } = renderHook(() => useContainerHistory("1h", [{ metric: "power" }]));
      await act(async () => {});
      const previousWindow = result.current.window;
      await act(() => vi.advanceTimersByTimeAsync(120_000));
      expect(client.telemetryServiceQueryTimeSeries).toHaveBeenCalled();
      expect(result.current.result).toEqual(history(10));
      expect(result.current.window).toEqual(previousWindow);
      expect(result.current.error).toContain("out of date");
      expect(result.current.pending).toBe(false);
    },
  );

  it("keeps the warning and chart during retries until fresh data arrives", async () => {
    const retry = deferred();
    mocks.adapter.getContainerHistory
      .mockResolvedValueOnce(history(10))
      .mockRejectedValueOnce(new Error("offline"))
      .mockReturnValue(retry.promise);
    const { result } = renderHook(() => useContainerHistory("1h", [{ metric: "power" }]));
    await act(async () => {});
    const previousWindow = result.current.window;
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.error).toContain("out of date");
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(result.current.pending).toBe(true);
    expect(result.current.error).toContain("out of date");
    expect(result.current.result).toEqual(history(10));
    expect(result.current.window).toEqual(previousWindow);
    await act(async () => retry.resolve(history(20)));
    expect(result.current.result).toEqual(history(20));
    expect(result.current.error).toBeUndefined();
    expect(result.current.pending).toBe(false);
    expect(result.current.window).not.toEqual(previousWindow);
  });

  it("clears the old warning and history immediately when the query changes", async () => {
    const next = deferred();
    mocks.adapter.getContainerHistory
      .mockResolvedValueOnce(history(10))
      .mockRejectedValueOnce(new Error("offline"))
      .mockReturnValue(next.promise);
    const { result, rerender } = renderHook(
      ({ duration }: { duration: Duration }) => useContainerHistory(duration, [{ metric: "power" }]),
      { initialProps: { duration: "1h" as Duration } },
    );
    await act(async () => {});
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.error).toContain("out of date");
    rerender({ duration: "48h" });
    expect(result.current.error).toBeUndefined();
    expect(result.current.result).toBeUndefined();
    expect(result.current.pending).toBe(true);
    await act(async () => next.resolve(history(48)));
    expect(result.current.result).toEqual(history(48));
  });

  it("accepts partial results and successful empty histories instead of retaining stale data", async () => {
    const partial = { ...history(20), unavailable: ["hashrate:"] };
    const empty: ContainerHistoryResult = {
      series: { "power:": { points: [], units: "W" } },
      unavailable: ["hashrate:"],
    };
    mocks.adapter.getContainerHistory
      .mockResolvedValueOnce(history(10))
      .mockResolvedValueOnce({ series: {}, unavailable: ["power:", "hashrate:"] })
      .mockResolvedValueOnce(partial)
      .mockResolvedValueOnce(empty);
    const { result } = renderHook(() => useContainerHistory("1h", [{ metric: "power" }, { metric: "hashrate" }]));
    await act(async () => {});
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.error).toContain("out of date");
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.result).toEqual(partial);
    expect(result.current.error).toBeUndefined();
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.result).toEqual(empty);
    expect(result.current.error).toBeUndefined();
  });

  it("settles an initial all-unavailable response without leaving history loading", async () => {
    mocks.adapter.getContainerHistory.mockResolvedValue({ series: {}, unavailable: ["power:"] });
    const { result } = renderHook(() => useContainerHistory("1h", [{ metric: "power" }]));
    await act(async () => {});
    expect(result.current.pending).toBe(false);
    expect(result.current.result).toBeUndefined();
    expect(result.current.error).toContain("out of date");
  });
});
