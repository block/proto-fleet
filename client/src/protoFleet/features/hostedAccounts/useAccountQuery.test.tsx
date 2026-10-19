import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useAccountQuery } from "./useAccountQuery";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
describe("account query isolation", () => {
  it("clears old data immediately and ignores a late response from the old account", async () => {
    const old = deferred<string>(),
      fresh = deferred<string>();
    let oldSignal!: AbortSignal;
    const loadA = vi.fn((signal: AbortSignal) => {
      oldSignal = signal;
      return old.promise;
    });
    const loadB = vi.fn(() => fresh.promise);
    const { result, rerender } = renderHook(({ load }) => useAccountQuery(load), { initialProps: { load: loadA } });
    rerender({ load: loadB });
    expect(oldSignal.aborted).toBe(true);
    expect(result.current.data).toBeNull();
    await act(async () => {
      old.resolve("private account A");
    });
    expect(result.current.data).toBeNull();
    await act(async () => {
      fresh.resolve("private account B");
    });
    expect(result.current.data).toBe("private account B");
  });
  it("retains same-account forms during background refresh but invalidation clears them", async () => {
    const next = deferred<string>();
    const load = vi.fn().mockResolvedValueOnce("account data").mockReturnValue(next.promise);
    const { result } = renderHook(() => useAccountQuery<string>(load, true, true));
    await waitFor(() => expect(result.current.data).toBe("account data"));
    act(() => result.current.refresh());
    expect(result.current.data).toBe("account data");
    expect(result.current.loading).toBe(false);
    act(() => result.current.invalidate());
    expect(result.current.data).toBeNull();
  });
});
