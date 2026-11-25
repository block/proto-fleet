import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { usePoll } from "./usePoll";

describe("usePoll", () => {
  test("hands fetchData a live (non-aborted) AbortSignal", async () => {
    const fetchData = vi.fn(async (_signal: AbortSignal) => {});

    renderHook(() => usePoll({ fetchData, poll: false }));

    await waitFor(() => expect(fetchData).toHaveBeenCalledTimes(1));
    const signal = fetchData.mock.calls[0][0];
    expect(signal).toBeInstanceOf(AbortSignal);
    expect(signal.aborted).toBe(false);
  });

  test("aborts the in-flight signal on unmount", async () => {
    let capturedSignal: AbortSignal | undefined;
    // Never resolve, so the request is still 'in flight' when we unmount.
    const fetchData = vi.fn((signal: AbortSignal) => {
      capturedSignal = signal;
      return new Promise<void>(() => {});
    });

    const { unmount } = renderHook(() => usePoll({ fetchData, poll: true }));

    await waitFor(() => expect(fetchData).toHaveBeenCalledTimes(1));
    expect(capturedSignal?.aborted).toBe(false);

    unmount();
    expect(capturedSignal?.aborted).toBe(true);
  });

  test("aborts the previous run's signal when params change", async () => {
    const signals: AbortSignal[] = [];
    const fetchData = vi.fn((signal: AbortSignal) => {
      signals.push(signal);
      return new Promise<void>(() => {});
    });

    const { rerender } = renderHook(({ params }) => usePoll({ fetchData, params, poll: true }), {
      initialProps: { params: "a" },
    });

    await waitFor(() => expect(fetchData).toHaveBeenCalledTimes(1));

    rerender({ params: "b" });
    await waitFor(() => expect(fetchData).toHaveBeenCalledTimes(2));

    // The first run's signal is aborted by its cleanup; the new run's is live.
    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);
  });
});
