import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type PoolDuration, usePoolStatistics } from "./usePoolStatistics";
import type { PoolHistoryResult } from "@/protoOS/api/adapters/poolHistory";
import { asPoolId } from "@/protoOS/store/contract";

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
    mocks.adapter.getPools.mockResolvedValue([{ id: asPoolId("pool:1"), name: "Primary", priority: 0 }]);
  });
  afterEach(() => vi.useRealTimers());

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
