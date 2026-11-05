import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useCurtailmentPolling } from "./useCurtailmentPolling";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("curtailment polling ownership", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("uses one Energy refresh and returns polling to the header when Energy unmounts", async () => {
    const headerRefresh = vi.fn().mockResolvedValue(undefined);
    const energyRefresh = vi.fn().mockResolvedValue(undefined);
    const header = renderHook(() => useCurtailmentPolling({ priority: 0, intervalMs: 3000, refresh: headerRefresh }));
    const energy = renderHook(() => useCurtailmentPolling({ priority: 1, intervalMs: 3000, refresh: energyRefresh }));
    await act(() => vi.advanceTimersByTimeAsync(3000));
    expect(energyRefresh).toHaveBeenCalledTimes(1);
    expect(headerRefresh).not.toHaveBeenCalled();
    energy.unmount();
    await act(() => vi.advanceTimersByTimeAsync(3000));
    expect(headerRefresh).toHaveBeenCalledTimes(1);
    header.unmount();
    await act(() => vi.advanceTimersByTimeAsync(30000));
    expect(headerRefresh).toHaveBeenCalledTimes(1);
  });

  it("does not overlap slow refreshes and aborts the active request on unmount", async () => {
    const pending = deferred();
    const refresh = vi.fn().mockReturnValue(pending.promise);
    const hook = renderHook(() => useCurtailmentPolling({ priority: 0, intervalMs: 3000, refresh }));
    await act(() => vi.advanceTimersByTimeAsync(30000));
    expect(refresh).toHaveBeenCalledTimes(1);
    const signal = refresh.mock.calls[0][0] as AbortSignal;
    hook.unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve());
  });
});
