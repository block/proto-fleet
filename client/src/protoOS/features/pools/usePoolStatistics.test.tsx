import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type PoolDuration, usePoolStatistics } from "./usePoolStatistics";
import type { PoolHistoryResult } from "@/protoOS/api/adapters/poolHistory";
import { usePoolsInfo as useFetchPoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { asPoolId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({ adapter: { getPools: vi.fn(), getPoolHistory: vi.fn() } }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: true }),
}));
const history = (accepted: number): PoolHistoryResult => ({
  byPool: { [asPoolId("pool:1")]: { accepted, rejected: 0, acceptanceRate: 1, partial: false } },
  unavailable: [],
});
const deferred = () => {
  let resolve!: (value: PoolHistoryResult) => void;
  const promise = new Promise<PoolHistoryResult>((accept) => {
    resolve = accept;
  });
  return { promise, resolve };
};

describe("pool statistics lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-16T12:00:00Z"));
    vi.resetAllMocks();
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().pools.setPoolsInfo([{ id: asPoolId("pool:1"), name: "Primary", priority: 0 }]);
  });
  afterEach(() => vi.useRealTimers());

  it("exposes initial shared pool-fetch failure and recovers on the shell poll", async () => {
    useMinerStore.getState().resetDeviceData();
    mocks.adapter.getPools.mockRejectedValue({ status: 503, error: { message: "Unavailable" } });
    mocks.adapter.getPoolHistory.mockResolvedValue(history(10));
    const { result } = renderHook(() => {
      useFetchPoolsInfo({ poll: true, pollIntervalMs: 15_000 });
      return usePoolStatistics("1h");
    });
    await act(async () => {});
    expect(result.current.pools).toBeUndefined();
    expect(result.current.pending).toBe(false);
    expect(result.current.error).toBe("Could not load pool information. Retrying automatically.");
    expect(mocks.adapter.getPoolHistory).not.toHaveBeenCalled();

    let recover!: (pools: { id: ReturnType<typeof asPoolId>; priority: number }[]) => void;
    mocks.adapter.getPools.mockReturnValue(
      new Promise((resolve) => {
        recover = resolve;
      }),
    );
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(result.current.error).toContain("Could not load pool information");
    // The shell poll waits for the in-flight request; the page doesn't add another fetch.
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(mocks.adapter.getPools).toHaveBeenCalledTimes(2);
    await act(async () => recover([{ id: asPoolId("pool:1"), priority: 0 }]));
    expect(result.current.error).toBeUndefined();
    expect(result.current.history).toEqual(history(10));
    expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(1);
  });

  it("retains pool data and warns if the shared pool refresh fails", async () => {
    const pools = [{ id: asPoolId("pool:1"), priority: 0 }];
    mocks.adapter.getPools.mockResolvedValueOnce(pools).mockRejectedValue({ status: 503 });
    mocks.adapter.getPoolHistory.mockResolvedValue(history(10));
    const { result } = renderHook(() => {
      useFetchPoolsInfo({ poll: true, pollIntervalMs: 15_000 });
      return usePoolStatistics("1h");
    });
    await act(async () => {});
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(result.current.pools).toEqual(pools);
    expect(result.current.history).toEqual(history(10));
    expect(result.current.error).toContain("Displayed pool details may be out of date");
    mocks.adapter.getPools.mockResolvedValue(pools);
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(result.current.error).toBeUndefined();
  });

  it("does not apply a shared pool-fetch error after switching miners", async () => {
    useMinerStore.getState().resetDeviceData();
    let reject!: (error: unknown) => void;
    mocks.adapter.getPools.mockReturnValue(
      new Promise((_, no) => {
        reject = no;
      }),
    );
    const { result } = renderHook(() => {
      useFetchPoolsInfo({ poll: true, pollIntervalMs: 15_000 });
      return usePoolStatistics("1h");
    });
    await act(async () => {});
    act(() => useMinerStore.getState().resetDeviceData());
    await act(async () => reject({ status: 503 }));
    expect(result.current.error).toBeUndefined();
    expect(result.current.pools).toBeUndefined();
    expect(result.current.pending).toBe(true);
  });

  it.each([
    ["1h", 1],
    ["12h", 12],
    ["24h", 24],
    ["48h", 48],
    ["5d", 120],
  ] as const)("requests the %s window", async (duration, hours) => {
    mocks.adapter.getPoolHistory.mockResolvedValue(history(10));
    renderHook(() => usePoolStatistics(duration));
    await act(async () => {});
    const query = mocks.adapter.getPoolHistory.mock.calls[0][0];
    expect(Date.parse(query.endTime) - Date.parse(query.startTime)).toBe(hours * 3_600_000);
    expect(query.intervalMs).toBe(3_600_000);
    expect(Date.parse(query.startTime) % 3_600_000).toBe(0);
    expect(Date.parse(query.endTime) % 3_600_000).toBe(0);
    expect(mocks.adapter.getPools).not.toHaveBeenCalled();
  });

  it("waits for shared pools and updates live diagnostics without restarting history", async () => {
    useMinerStore.getState().pools.setPoolsInfo(undefined);
    const pending = deferred();
    mocks.adapter.getPoolHistory.mockReturnValue(pending.promise);
    const { result } = renderHook(() => usePoolStatistics("1h"));
    await act(async () => {});
    expect(result.current.pending).toBe(true);
    expect(mocks.adapter.getPoolHistory).not.toHaveBeenCalled();
    const pool = { id: asPoolId("pool:1"), name: "Primary", priority: 0, status: "Active" };
    await act(async () => useMinerStore.getState().pools.setPoolsInfo([pool]));
    const signal = mocks.adapter.getPoolHistory.mock.calls[0][1].signal;
    await act(async () => useMinerStore.getState().pools.setPoolsInfo([{ ...pool, status: "Idle" }]));
    expect(result.current.pools?.[0].status).toBe("Idle");
    expect(signal.aborted).toBe(false);
    expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve(history(10)));
    expect(result.current.history).toEqual(history(10));
    expect(mocks.adapter.getPools).not.toHaveBeenCalled();
  });

  it("cancels old pool selections and miner snapshots", async () => {
    const pending = deferred();
    mocks.adapter.getPoolHistory.mockReturnValue(pending.promise);
    const { result, rerender } = renderHook(() => usePoolStatistics("1h"));
    await act(async () => {});
    const firstSignal = mocks.adapter.getPoolHistory.mock.calls[0][1].signal;
    await act(async () => useMinerStore.getState().pools.setPoolsInfo([{ id: asPoolId("pool:2"), priority: 0 }]));
    expect(firstSignal.aborted).toBe(true);
    expect(mocks.adapter.getPoolHistory.mock.calls[1][0].poolIds).toEqual(["pool:2"]);
    const secondSignal = mocks.adapter.getPoolHistory.mock.calls[1][1].signal;
    mocks.adapter = { getPools: vi.fn(), getPoolHistory: vi.fn().mockResolvedValue(history(99)) };
    act(() => useMinerStore.getState().resetDeviceData());
    rerender();
    expect(secondSignal.aborted).toBe(true);
    expect(result.current.pools).toBeUndefined();
    await act(async () => pending.resolve(history(10)));
    expect(result.current.history).toBeUndefined();
    expect(mocks.adapter.getPoolHistory).not.toHaveBeenCalled();
    await act(async () => useMinerStore.getState().pools.setPoolsInfo([{ id: asPoolId("pool:1"), priority: 0 }]));
    expect(result.current.history).toEqual(history(99));
  });

  it("aborts old ranges, ignores late responses, and stops polling on unmount", async () => {
    const old = deferred(),
      current = deferred();
    mocks.adapter.getPoolHistory.mockReturnValueOnce(old.promise).mockReturnValue(current.promise);
    const { result, rerender, unmount } = renderHook(
      ({ duration }: { duration: PoolDuration }) => usePoolStatistics(duration),
      { initialProps: { duration: "1h" as PoolDuration } },
    );
    await act(async () => {});
    const oldSignal = mocks.adapter.getPoolHistory.mock.calls[0][1].signal;
    rerender({ duration: "48h" });
    expect(oldSignal.aborted).toBe(true);
    expect(result.current.history).toBeUndefined();
    await act(async () => {});
    await act(async () => current.resolve(history(48)));
    await act(async () => old.resolve(history(1)));
    expect(result.current.history).toEqual(history(48));
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(3);
    unmount();
    expect(mocks.adapter.getPoolHistory.mock.calls[2][1].signal.aborted).toBe(true);
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(3);
  });

  it("retains the current period with an explicit error when refreshing fails", async () => {
    const recovery = deferred();
    mocks.adapter.getPoolHistory
      .mockResolvedValueOnce(history(10))
      .mockRejectedValueOnce(new Error("offline"))
      .mockReturnValue(recovery.promise);
    const { result } = renderHook(() => usePoolStatistics("24h"));
    await act(async () => {});
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.history).toEqual(history(10));
    expect(result.current.error).toContain("out of date");
    expect(result.current.pending).toBe(false);
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(result.current.pending).toBe(true);
    expect(result.current.history).toEqual(history(10));
    expect(result.current.error).toContain("out of date");
    await act(async () => recovery.resolve(history(20)));
    expect(result.current.history).toEqual(history(20));
    expect(result.current.error).toBeUndefined();
  });
});
