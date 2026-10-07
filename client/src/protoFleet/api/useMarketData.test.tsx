import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { MARKET_DATA_POLL_MS, useMarketData } from "./useMarketData";
import { GetMarketDataResponseSchema } from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";

const mocks = vi.hoisted(() => ({ get: vi.fn(), auth: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ marketDataClient: { getMarketData: mocks.get } }));
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors: mocks.auth }) }));

const response = () => create(GetMarketDataResponseSchema, { enabled: true });
const settle = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

beforeEach(() => {
  vi.useFakeTimers();
  mocks.get.mockReset().mockResolvedValue(response());
  mocks.auth.mockReset();
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("useMarketData", () => {
  it("treats permission denial as a hidden terminal state without polling or visibility refreshes", async () => {
    mocks.get.mockRejectedValue(new ConnectError("not permitted", Code.PermissionDenied));
    const { result } = renderHook(() => useMarketData());
    await settle();
    expect(result.current.data).toBeUndefined();
    expect(result.current.error).toBe(false);
    expect(mocks.auth).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(mocks.get).toHaveBeenCalledOnce();
  });

  it("clears previously displayed data and stops polling when access is revoked", async () => {
    const { result } = renderHook(() => useMarketData());
    await settle();
    expect(result.current.data?.enabled).toBe(true);
    mocks.get.mockRejectedValue(new ConnectError("not permitted", Code.PermissionDenied));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(result.current.data).toBeUndefined();
    expect(result.current.error).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });

  it("stops polling and visibility refreshes when the server disables market data", async () => {
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema));
    const { result } = renderHook(() => useMarketData());
    await settle();
    expect(result.current.data?.enabled).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(mocks.get).toHaveBeenCalledOnce();
  });

  it("stops polling after an enabled instance becomes disabled", async () => {
    const { result } = renderHook(() => useMarketData());
    await settle();
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(result.current.data?.enabled).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
    });
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });

  it("rechecks the flag on remount after a deployment enables market data", async () => {
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema));
    const first = renderHook(() => useMarketData());
    await settle();
    first.unmount();
    mocks.get.mockResolvedValue(response());
    const { result } = renderHook(() => useMarketData());
    await settle();
    expect(result.current.data?.enabled).toBe(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(mocks.get).toHaveBeenCalledTimes(3);
  });

  it("fetches immediately and retains the data while polling once a minute", async () => {
    const { result } = renderHook(() => useMarketData());
    expect(result.current.data).toBeUndefined();
    await settle();
    expect(result.current.data?.enabled).toBe(true);
    const previous = result.current.data;
    mocks.get.mockImplementationOnce(() => new Promise(() => {}));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(mocks.get).toHaveBeenCalledTimes(2);
    expect(result.current.data).toBe(previous);
  });

  it("retains data during errors and recovers on retry", async () => {
    const { result } = renderHook(() => useMarketData());
    await settle();
    const previous = result.current.data;
    mocks.get.mockRejectedValueOnce(new Error("offline"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(result.current.error).toBe(true);
    expect(result.current.data).toBe(previous);
    expect(mocks.auth).toHaveBeenCalledOnce();
    act(() => result.current.refetch());
    await settle();
    expect(result.current.error).toBe(false);
  });

  it("skips hidden-tab polls and refreshes when visible again", async () => {
    const visibility = vi.spyOn(document, "visibilityState", "get");
    const { unmount } = renderHook(() => useMarketData());
    await settle();
    visibility.mockReturnValue("hidden");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
    });
    expect(mocks.get).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
    visibility.mockReturnValue("visible");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await settle();
    expect(mocks.get).toHaveBeenCalledTimes(2);
    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 2);
    });
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });

  it("waits for visibility before making the initial request", async () => {
    const visibility = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    renderHook(() => useMarketData());
    await settle();
    expect(mocks.get).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
    visibility.mockReturnValue("visible");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await settle();
    expect(mocks.get).toHaveBeenCalledOnce();
  });

  it("does not schedule another poll when an in-flight request finishes in a hidden tab", async () => {
    let resolve!: (value: ReturnType<typeof response>) => void;
    mocks.get.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const visibility = vi.spyOn(document, "visibilityState", "get");
    renderHook(() => useMarketData());
    visibility.mockReturnValue("hidden");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await act(async () => resolve(response()));
    expect(vi.getTimerCount()).toBe(0);
    visibility.mockReturnValue("visible");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await settle();
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });

  it("doesn't overlap requests and aborts on unmount", async () => {
    mocks.get.mockImplementation(() => new Promise(() => {}));
    const { unmount } = renderHook(() => useMarketData());
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
    });
    expect(mocks.get).toHaveBeenCalledOnce();
    const signal = mocks.get.mock.calls[0][1].signal as AbortSignal;
    unmount();
    expect(signal.aborted).toBe(true);
  });

  it("ignores responses from an aborted request after retry", async () => {
    let resolveOld!: (value: ReturnType<typeof response>) => void;
    mocks.get.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    );
    const { result } = renderHook(() => useMarketData());
    act(() => result.current.refetch());
    await settle();
    expect(result.current.data?.enabled).toBe(true);
    await act(async () => {
      resolveOld(create(GetMarketDataResponseSchema, { enabled: false }));
    });
    expect(result.current.data?.enabled).toBe(true);
  });
});
