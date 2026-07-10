import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { MODAL_IDLE_CEILING_MS, MODAL_REFRESH_INTERVAL_MS, useModalLiveRefresh } from "./useModalLiveRefresh";

const setVisibility = (state: "visible" | "hidden") => {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  });
  document.dispatchEvent(new Event("visibilitychange"));
};

describe("useModalLiveRefresh", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setVisibility("visible");
  });

  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
    setVisibility("visible");
  });

  it("fires an immediate tick on open", () => {
    const onTick = vi.fn();
    renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));

    expect(onTick).toHaveBeenCalledTimes(1);
  });

  it("ticks again after the interval elapses", () => {
    const onTick = vi.fn();
    renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));

    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS));
    expect(onTick).toHaveBeenCalledTimes(2);

    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS));
    expect(onTick).toHaveBeenCalledTimes(3);
  });

  it("does not tick while enabled is false", () => {
    const onTick = vi.fn();
    renderHook(() => useModalLiveRefresh({ enabled: false, onTick }));

    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS * 3));
    expect(onTick).not.toHaveBeenCalled();
  });

  it("suspends ticks while the tab is hidden and catches up on return", () => {
    const onTick = vi.fn();
    renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));
    expect(onTick).toHaveBeenCalledTimes(1); // immediate open tick

    act(() => setVisibility("hidden"));
    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS * 3));
    expect(onTick).toHaveBeenCalledTimes(1); // no ticks while hidden

    act(() => setVisibility("visible"));
    expect(onTick).toHaveBeenCalledTimes(2); // immediate catch-up fetch

    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS));
    expect(onTick).toHaveBeenCalledTimes(3); // cadence resumes
  });

  it("pauses after the idle ceiling and resumes on interaction", () => {
    const onTick = vi.fn();
    const { result } = renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));

    // Advance past the idle ceiling without any interaction.
    act(() => vi.advanceTimersByTime(MODAL_IDLE_CEILING_MS + MODAL_REFRESH_INTERVAL_MS));
    expect(result.current.isPaused).toBe(true);

    const callsAtPause = onTick.mock.calls.length;
    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS * 3));
    expect(onTick).toHaveBeenCalledTimes(callsAtPause); // no ticks while paused

    // Any interaction resumes with an immediate tick.
    act(() => document.dispatchEvent(new Event("mousemove")));
    expect(result.current.isPaused).toBe(false);
    expect(onTick).toHaveBeenCalledTimes(callsAtPause + 1);

    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS));
    expect(onTick).toHaveBeenCalledTimes(callsAtPause + 2);
  });

  it("keeps ticking as long as the operator interacts within the ceiling", () => {
    const onTick = vi.fn();
    const { result } = renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));

    // Interact just before each ceiling boundary so it never pauses.
    for (let i = 0; i < 3; i++) {
      act(() => vi.advanceTimersByTime(MODAL_IDLE_CEILING_MS - MODAL_REFRESH_INTERVAL_MS));
      act(() => document.dispatchEvent(new Event("keydown")));
    }
    expect(result.current.isPaused).toBe(false);
  });

  it("resumes a paused loop via the returned resume()", () => {
    const onTick = vi.fn();
    const { result } = renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));

    act(() => vi.advanceTimersByTime(MODAL_IDLE_CEILING_MS + MODAL_REFRESH_INTERVAL_MS));
    expect(result.current.isPaused).toBe(true);

    const callsAtPause = onTick.mock.calls.length;
    act(() => result.current.resume());
    expect(result.current.isPaused).toBe(false);
    expect(onTick).toHaveBeenCalledTimes(callsAtPause + 1);
  });

  it("clears the interval on unmount", () => {
    const onTick = vi.fn();
    const { unmount } = renderHook(() => useModalLiveRefresh({ enabled: true, onTick }));

    unmount();
    const callsAtUnmount = onTick.mock.calls.length;
    act(() => vi.advanceTimersByTime(MODAL_REFRESH_INTERVAL_MS * 3));
    expect(onTick).toHaveBeenCalledTimes(callsAtUnmount);
  });

  it("restarts with an immediate tick when restartKey changes", () => {
    const onTick = vi.fn();
    const { rerender } = renderHook(({ key }) => useModalLiveRefresh({ enabled: true, onTick, restartKey: key }), {
      initialProps: { key: "miner-1" },
    });
    expect(onTick).toHaveBeenCalledTimes(1);

    rerender({ key: "miner-2" });
    expect(onTick).toHaveBeenCalledTimes(2);
  });
});
