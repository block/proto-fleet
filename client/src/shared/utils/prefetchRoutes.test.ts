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

  it("invokes every importer at idle time", () => {
    const requestIdleCallback = vi.fn<(cb: Parameters<typeof window.requestIdleCallback>[0]) => number>((cb) => {
      cb({ didTimeout: false, timeRemaining: () => 50 });
      return 1;
    });
    setRequestIdleCallback(requestIdleCallback);

    const a = vi.fn(() => Promise.resolve({}));
    const b = vi.fn(() => Promise.resolve({}));

    prefetchRoutes([a, b]);

    expect(requestIdleCallback).toHaveBeenCalledTimes(1);
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
  });

  it("falls back to setTimeout when requestIdleCallback is unavailable", () => {
    setRequestIdleCallback(undefined);

    const a = vi.fn(() => Promise.resolve({}));
    prefetchRoutes([a]);
    expect(a).not.toHaveBeenCalled();

    vi.runAllTimers();
    expect(a).toHaveBeenCalledTimes(1);
  });

  it("swallows importer rejections so prefetch failures don't surface as unhandled rejections", async () => {
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
  });
});
