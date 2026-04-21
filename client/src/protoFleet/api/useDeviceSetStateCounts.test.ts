import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { deviceSetClient } from "./clients";
import { useDeviceSetStateCounts } from "./useDeviceSetStateCounts";

vi.mock("./clients", () => ({
  deviceSetClient: {
    getDeviceSetStats: vi.fn(),
  },
}));

const { mockHandleAuthErrors } = vi.hoisted(() => ({
  mockHandleAuthErrors: vi.fn(({ onError }: { onError: (err: unknown) => void }) => onError),
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

const mockGetDeviceSetStats = vi.mocked(deviceSetClient.getDeviceSetStats);

function createMockResponse(
  deviceCount: number,
  counts: { hashing?: number; broken?: number; offline?: number; sleeping?: number },
) {
  return {
    stats: [
      {
        deviceCount,
        hashingCount: counts.hashing ?? 0,
        brokenCount: counts.broken ?? 0,
        offlineCount: counts.offline ?? 0,
        sleepingCount: counts.sleeping ?? 0,
        slotStatuses: [],
      },
    ],
  };
}

describe("useDeviceSetStateCounts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns initial state when deviceSetId is undefined", () => {
    const { result } = renderHook(() => useDeviceSetStateCounts({ deviceSetId: undefined }));

    expect(result.current.totalMiners).toBe(0);
    expect(result.current.stateCounts).toBeUndefined();
    expect(result.current.hasLoaded).toBe(false);
    expect(mockGetDeviceSetStats).not.toHaveBeenCalled();
  });

  it("fetches counts when deviceSetId is provided", async () => {
    mockGetDeviceSetStats.mockResolvedValue(
      createMockResponse(42, { hashing: 30, broken: 5, offline: 4, sleeping: 3 }) as any,
    );

    const { result } = renderHook(() => useDeviceSetStateCounts({ deviceSetId: 1n }));

    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });

    expect(result.current.totalMiners).toBe(42);
    expect(result.current.stateCounts?.hashingCount).toBe(30);
    expect(result.current.stateCounts?.brokenCount).toBe(5);
    expect(result.current.stateCounts?.offlineCount).toBe(4);
    expect(result.current.stateCounts?.sleepingCount).toBe(3);
  });

  it("resets state when deviceSetId changes", async () => {
    mockGetDeviceSetStats.mockResolvedValue(createMockResponse(10, { hashing: 10 }) as any);

    const { result, rerender } = renderHook(({ deviceSetId }) => useDeviceSetStateCounts({ deviceSetId }), {
      initialProps: { deviceSetId: 1n as bigint | undefined },
    });

    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });
    expect(result.current.totalMiners).toBe(10);

    // Change deviceSetId — state should reset
    mockGetDeviceSetStats.mockResolvedValue(createMockResponse(20, { hashing: 15, offline: 5 }) as any);
    rerender({ deviceSetId: 2n });

    // Before new fetch resolves, state should be cleared
    expect(result.current.stats).toBeUndefined();
    expect(result.current.hasLoaded).toBe(false);

    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });
    expect(result.current.totalMiners).toBe(20);
    expect(result.current.stateCounts?.offlineCount).toBe(5);
  });

  it("sets hasLoaded on error so consumers are not stuck loading", async () => {
    mockGetDeviceSetStats.mockRejectedValue(new Error("network error"));

    const { result } = renderHook(() => useDeviceSetStateCounts({ deviceSetId: 1n }));

    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });

    // hasLoaded is true even on error — page can render with empty stats
    expect(result.current.totalMiners).toBe(0);
    expect(result.current.stateCounts).toBeUndefined();
    expect(result.current.isLoading).toBe(false);
  });

  it("does not fetch when deviceSetId transitions to undefined", async () => {
    mockGetDeviceSetStats.mockResolvedValue(createMockResponse(10, { hashing: 10 }) as any);

    const { result, rerender } = renderHook(({ deviceSetId }) => useDeviceSetStateCounts({ deviceSetId }), {
      initialProps: { deviceSetId: 1n as bigint | undefined },
    });

    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });

    rerender({ deviceSetId: undefined });

    // Should not have made a second call
    expect(mockGetDeviceSetStats).toHaveBeenCalledTimes(1);
  });
});
