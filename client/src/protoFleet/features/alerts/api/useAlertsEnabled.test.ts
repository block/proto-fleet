import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  _resetAlertsEnabledCache,
  useAlertsEnabled,
  useAlertsEnabledState,
} from "@/protoFleet/features/alerts/api/useAlertsEnabled";

const okResponse = (enabled: boolean) => ({ ok: true, json: async () => ({ enabled }) }) as unknown as Response;

describe("useAlertsEnabled", () => {
  beforeEach(() => {
    _resetAlertsEnabledCache();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("retries an unreachable server rather than reporting alerts disabled", async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(okResponse(true));
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useAlertsEnabled());

    await vi.advanceTimersByTimeAsync(2_000);
    await waitFor(() => expect(result.current).toBe(true));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("gives a probe a deadline, so a connection that never answers still gets retried", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(
        (_url: string, init?: { signal?: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
          }),
      )
      .mockResolvedValueOnce(okResponse(true));
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useAlertsEnabled());

    // The first probe hangs: without a deadline it holds the shared promise, and no later probe is ever made.
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.advanceTimersByTimeAsync(2_000);
    await waitFor(() => expect(result.current).toBe(true));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("retries a failing status, which the endpoint never uses to mean disabled", async () => {
    // Both states come back as a 200 body, so a 503 from a proxy is a failed probe rather than an answer.
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, status: 503 } as unknown as Response)
      .mockResolvedValueOnce(okResponse(false));
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useAlertsEnabled());

    await vi.advanceTimersByTimeAsync(2_000);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(result.current).toBe(false);
  });

  it("keeps probing a sustained outage, since the shell that mounts it is never remounted", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useAlertsEnabled());
    await vi.advanceTimersByTimeAsync(30_000);
    expect(result.current).toBe(false);
    const attemptsWhileDown = fetchMock.mock.calls.length;
    expect(attemptsWhileDown).toBeGreaterThan(1);

    // Recovery reaches the same mount: nothing had to remount or reload for the alerts surface to come back.
    fetchMock.mockResolvedValue(okResponse(true));
    await vi.advanceTimersByTimeAsync(120_000);
    await waitFor(() => expect(result.current).toBe(true));
  });

  it("reports a failing probe within one attempt, then clears it when a retry answers", async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(okResponse(true));
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useAlertsEnabledState());

    // The first failed attempt is enough to flag failing — no unbounded wait before a page can react.
    await waitFor(() => expect(result.current.failing).toBe(true));
    expect(result.current.resolved).toBe(false);

    await vi.advanceTimersByTimeAsync(2_000);
    await waitFor(() => expect(result.current.resolved).toBe(true));
    expect(result.current.enabled).toBe(true);
    expect(result.current.failing).toBe(false);
  });

  it("stops probing once unmounted", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", fetchMock);

    const { unmount } = renderHook(() => useAlertsEnabled());
    await vi.advanceTimersByTimeAsync(10_000);
    unmount();
    const attemptsAtUnmount = fetchMock.mock.calls.length;

    await vi.advanceTimersByTimeAsync(120_000);
    expect(fetchMock).toHaveBeenCalledTimes(attemptsAtUnmount);
  });
});
