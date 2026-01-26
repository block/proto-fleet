import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import useVisibleMiners from "./useVisibleMiners";
import type { UISlice } from "@/protoFleet/store/slices/uiSlice";
import { createUISlice } from "@/protoFleet/store/slices/uiSlice";

type TestStore = { ui: UISlice };

// Mock the store
vi.mock("@/protoFleet/store", () => ({
  useFleetStore: {
    getState: vi.fn(),
  },
}));

describe("useVisibleMiners store integration", () => {
  let store: any;
  let observerCallback: any;
  let mockObserver: any;

  beforeEach(async () => {
    vi.clearAllMocks();
    vi.useFakeTimers();

    // Create a fresh store for each test
    store = create<TestStore>()(
      immer((set, get, api) => ({
        ui: createUISlice(set as any, get as any, api as any),
      })),
    );

    // Setup mock implementation
    const { useFleetStore } = vi.mocked(await import("@/protoFleet/store"));
    useFleetStore.getState = vi.fn(() => store.getState());

    // Setup IntersectionObserver mock
    mockObserver = {
      observe: vi.fn(),
      unobserve: vi.fn(),
      disconnect: vi.fn(),
    };

    globalThis.IntersectionObserver = class IntersectionObserver {
      constructor(callback: any) {
        observerCallback = callback;
        return mockObserver;
      }
    } as any;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("should update store when visible miners change", async () => {
    const { result, unmount } = renderHook(() => useVisibleMiners({ debounceMs: 100 }));

    // Create mock elements
    const element1 = document.createElement("div");
    const element2 = document.createElement("div");

    // Register miners
    act(() => {
      result.current.registerMiner("device-1", element1);
      result.current.registerMiner("device-2", element2);
    });

    // Simulate IntersectionObserver entries
    act(() => {
      observerCallback([
        { target: element1, isIntersecting: true },
        { target: element2, isIntersecting: true },
      ]);
    });

    // Fast-forward past debounce delay
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });

    // Store should be updated with visible miner IDs
    const storeVisibleIds = store.getState().ui.visibleMinerIds;
    expect(storeVisibleIds).toEqual(new Set(["device-1", "device-2"]));

    unmount();
  });

  it("should update store when visibility changes", async () => {
    const element1 = document.createElement("div");
    const element2 = document.createElement("div");

    const { result, unmount } = renderHook(() => useVisibleMiners({ debounceMs: 100 }));

    // Register miners
    act(() => {
      result.current.registerMiner("device-1", element1);
      result.current.registerMiner("device-2", element2);
    });

    // Initially both visible
    act(() => {
      observerCallback([
        { target: element1, isIntersecting: true },
        { target: element2, isIntersecting: true },
      ]);
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });

    expect(store.getState().ui.visibleMinerIds).toEqual(new Set(["device-1", "device-2"]));

    // Device-2 scrolls out of view
    act(() => {
      observerCallback([{ target: element2, isIntersecting: false }]);
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });

    // Store should only have device-1 now
    expect(store.getState().ui.visibleMinerIds).toEqual(new Set(["device-1"]));

    unmount();
  });

  it("should debounce store updates during rapid visibility changes", async () => {
    const element1 = document.createElement("div");

    const { result, unmount } = renderHook(() => useVisibleMiners({ debounceMs: 300 }));

    act(() => {
      result.current.registerMiner("device-1", element1);
    });

    // Rapid visibility changes
    act(() => {
      observerCallback([{ target: element1, isIntersecting: true }]);
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    act(() => {
      observerCallback([{ target: element1, isIntersecting: false }]);
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    act(() => {
      observerCallback([{ target: element1, isIntersecting: true }]);
    });

    // Store should not be updated yet (still within debounce window)
    expect(store.getState().ui.visibleMinerIds.size).toBe(0);

    // Fast-forward past debounce delay from the LAST change (300ms)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    // Now store should be updated with final state
    expect(store.getState().ui.visibleMinerIds).toEqual(new Set(["device-1"]));

    unmount();
  });

  it("should clear store when all miners become invisible", async () => {
    const element1 = document.createElement("div");
    const element2 = document.createElement("div");

    const { result, unmount } = renderHook(() => useVisibleMiners({ debounceMs: 100 }));

    // Register and make visible
    act(() => {
      result.current.registerMiner("device-1", element1);
      result.current.registerMiner("device-2", element2);
    });

    act(() => {
      observerCallback([
        { target: element1, isIntersecting: true },
        { target: element2, isIntersecting: true },
      ]);
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });

    expect(store.getState().ui.visibleMinerIds.size).toBe(2);

    // Make all invisible
    act(() => {
      observerCallback([
        { target: element1, isIntersecting: false },
        { target: element2, isIntersecting: false },
      ]);
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });

    // Store should be empty
    expect(store.getState().ui.visibleMinerIds).toEqual(new Set());

    unmount();
  });
});
