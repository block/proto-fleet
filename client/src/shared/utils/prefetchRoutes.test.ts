import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { prefetchRoutes } from "./prefetchRoutes";

const setRequestIdleCallback = (
  impl: ((cb: Parameters<typeof window.requestIdleCallback>[0]) => number) | undefined,
) => {
  if (impl === undefined) {
    vi.stubGlobal("requestIdleCallback", undefined);
  } else {
    vi.stubGlobal("requestIdleCallback", impl);
  }
};

describe("prefetchRoutes", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("invokes every importer at idle time with the 2000ms timeout option", () => {
    const requestIdleCallback = vi.fn<(cb: Parameters<typeof window.requestIdleCallback>[0]) => number>((cb) => {
      cb({ didTimeout: false, timeRemaining: () => 50 });
      return 1;
    });
    setRequestIdleCallback(requestIdleCallback);

    const a = vi.fn(() => Promise.resolve({}));
    const b = vi.fn(() => Promise.resolve({}));

    prefetchRoutes([a, b]);

    expect(requestIdleCallback).toHaveBeenCalledTimes(1);
    expect(requestIdleCallback).toHaveBeenCalledWith(expect.any(Function), { timeout: 2000 });
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
  });

  it("falls back to setTimeout(cb, 500) when requestIdleCallback is unavailable", () => {
    setRequestIdleCallback(undefined);
    const setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");

    const a = vi.fn(() => Promise.resolve({}));
    prefetchRoutes([a]);
    expect(a).not.toHaveBeenCalled();
    expect(setTimeoutSpy).toHaveBeenCalledWith(expect.any(Function), 500);

    vi.runAllTimers();
    expect(a).toHaveBeenCalledTimes(1);
  });

  it("no-ops on an empty importer list without throwing", () => {
    setRequestIdleCallback((cb) => {
      cb({ didTimeout: false, timeRemaining: () => 50 });
      return 1;
    });

    expect(() => prefetchRoutes([])).not.toThrow();
  });

  it("returns a cancel handle that prevents queued importers from firing (idle path)", () => {
    const cancelIdleCallback = vi.fn();
    vi.stubGlobal("cancelIdleCallback", cancelIdleCallback);
    setRequestIdleCallback(() => 42);

    const a = vi.fn(() => Promise.resolve({}));
    const cancel = prefetchRoutes([a]);
    cancel();

    expect(cancelIdleCallback).toHaveBeenCalledWith(42);
    expect(a).not.toHaveBeenCalled();
  });

  it("returns a cancel handle that prevents queued importers from firing (setTimeout fallback)", () => {
    setRequestIdleCallback(undefined);

    const a = vi.fn(() => Promise.resolve({}));
    const cancel = prefetchRoutes([a]);
    cancel();

    vi.runAllTimers();
    expect(a).not.toHaveBeenCalled();
  });

  it("logs swallowed rejections via console.error so prefetch failures stay observable", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const unhandled = vi.fn();
    window.addEventListener("unhandledrejection", unhandled);
    try {
      setRequestIdleCallback((cb) => {
        cb({ didTimeout: false, timeRemaining: () => 50 });
        return 1;
      });

      const a = vi.fn(() => Promise.reject(new Error("boom")));
      const b = vi.fn(() => Promise.resolve({}));

      expect(() => prefetchRoutes([a, b])).not.toThrow();

      // Drain the microtask queue so the rejected promise resolves through the
      // .catch() chain instead of leaking out of the test.
      await vi.runAllTimersAsync();
      expect(a).toHaveBeenCalledTimes(1);
      expect(b).toHaveBeenCalledTimes(1);
      expect(unhandled).not.toHaveBeenCalled();
      expect(consoleErrorSpy).toHaveBeenCalledWith(
        "[prefetchRoutes] chunk prefetch failed:",
        expect.objectContaining({ message: "boom" }),
      );
    } finally {
      window.removeEventListener("unhandledrejection", unhandled);
      consoleErrorSpy.mockRestore();
    }
  });
});
