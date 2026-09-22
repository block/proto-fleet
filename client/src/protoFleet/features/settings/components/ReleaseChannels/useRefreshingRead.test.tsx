import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred } from "./__tests__/helpers";
import { useRefreshingRead } from "./useRefreshingRead";

const auth = vi.hoisted(() => ({ username: "operator", sessionGeneration: 1, isAuthenticated: true }));
vi.mock("@/protoFleet/store", () => ({
  useUsername: () => auth.username,
  useSessionGeneration: () => auth.sessionGeneration,
  useIsAuthenticated: () => auth.isAuthenticated,
  useFleetStore: { getState: () => ({ auth }) },
}));

beforeEach(() => {
  Object.assign(auth, { username: "operator", sessionGeneration: 1, isAuthenticated: true });
});
afterEach(() => vi.useRealTimers());

describe("refreshing complete reads", () => {
  it.each([false, true])("coalesces repeated summary changes with trailing refresh %s", async (trailingRefresh) => {
    const first = deferred<string[]>();
    const second = deferred<string[]>();
    const read = vi
      .fn<(signal: AbortSignal) => Promise<string[]>>()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const { result, rerender } = renderHook(
      ({ refreshKey }) =>
        useRefreshingRead({
          read,
          refreshKey,
          trailingRefresh,
          errorMessage: "Read failed",
        }),
      { initialProps: { refreshKey: 0 } },
    );
    for (let refreshKey = 1; refreshKey < 5; refreshKey += 1) rerender({ refreshKey });
    act(() => result.current.refresh());
    expect(read).toHaveBeenCalledOnce();
    expect(read.mock.calls[0][0].aborted).toBe(false);
    await act(async () => first.resolve(["complete"]));
    expect(read).toHaveBeenCalledTimes(trailingRefresh ? 2 : 1);
    expect(result.current.data).toEqual(["complete"]);
    expect(result.current.isLoading).toBe(trailingRefresh);
    if (trailingRefresh) {
      await act(async () => second.resolve(["new complete"]));
      expect(result.current.data).toEqual(["new complete"]);
      expect(result.current.isLoading).toBe(false);
    }
  });

  it.each(["success", "failure"])("ignores stale %s before the new login rerenders", async (outcome) => {
    const obsolete = deferred<string[]>();
    const replacement = deferred<string[]>();
    const read = vi
      .fn<(signal: AbortSignal) => Promise<string[]>>()
      .mockResolvedValueOnce(["old complete"])
      .mockReturnValueOnce(obsolete.promise)
      .mockReturnValueOnce(replacement.promise);
    const { result, rerender } = renderHook(() => useRefreshingRead({ read, errorMessage: "Read failed" }));
    await act(async () => {});
    act(() => result.current.refresh());
    auth.sessionGeneration += 1;
    await act(async () => {
      if (outcome === "success") obsolete.resolve(["obsolete"]);
      else obsolete.reject(new Error("obsolete failure"));
    });
    expect(result.current.data).toEqual(["old complete"]);
    expect(result.current.error).toBeNull();
    rerender();
    expect(result.current.data).toBeNull();
    expect(result.current.isLoading).toBe(true);
    await act(async () => replacement.resolve(["new login"]));
    expect(result.current.data).toEqual(["new login"]);
  });

  it("does not publish a late success after its deadline or overlap a transport that has not drained", async () => {
    vi.useFakeTimers();
    const stalled = deferred<string[]>();
    const read = vi
      .fn<(signal: AbortSignal) => Promise<string[]>>()
      .mockResolvedValueOnce(["last complete"])
      .mockReturnValueOnce(stalled.promise)
      .mockResolvedValueOnce(["recovered"]);
    const { result } = renderHook(() =>
      useRefreshingRead({
        read,
        errorMessage: "Read failed",
        timeoutMs: 30_000,
        timeoutMessage: "Request timed out",
      }),
    );
    await act(async () => {});
    act(() => result.current.refresh());
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(read.mock.calls[1][0].reason).toMatchObject({ message: "Request timed out" });
    act(() => result.current.refresh());
    expect(read).toHaveBeenCalledTimes(2);
    await act(async () => stalled.resolve(["arrived too late"]));
    expect(result.current.data).toEqual(["last complete"]);
    expect(result.current.error).toBe("Request timed out");
    expect(result.current.isLoading).toBe(false);
    await act(async () => result.current.refresh());
    expect(result.current.data).toEqual(["recovered"]);
    expect(result.current.error).toBeNull();
  });

  it("aborts old reader contexts and prevents their queued work from reaching the replacement", async () => {
    const obsolete = deferred<string[]>();
    const oldRead = vi.fn<(signal: AbortSignal) => Promise<string[]>>().mockReturnValueOnce(obsolete.promise);
    const newRead = vi.fn<(signal: AbortSignal) => Promise<string[]>>().mockResolvedValue(["replacement"]);
    const { result, rerender } = renderHook(
      ({ read }) => useRefreshingRead({ read, trailingRefresh: true, errorMessage: "Read failed" }),
      {
        initialProps: { read: oldRead },
      },
    );
    act(() => result.current.refresh());
    rerender({ read: newRead });
    expect(oldRead.mock.calls[0][0].aborted).toBe(true);
    expect(result.current.data).toBeNull();
    await act(async () => obsolete.resolve(["obsolete"]));
    expect(result.current.data).toEqual(["replacement"]);
    expect(oldRead).toHaveBeenCalledOnce();
    expect(newRead).toHaveBeenCalledOnce();
  });
});
