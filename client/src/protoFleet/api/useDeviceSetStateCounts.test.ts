import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "./clients";
import useDeviceSetStateCounts from "./useDeviceSetStateCounts";
import { ListMinerStateSnapshotsResponseSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerStateCountsSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

vi.mock("./clients", () => ({
  fleetManagementClient: {
    listMinerStateSnapshots: vi.fn(),
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

const mockListMinerStateSnapshots = vi.mocked(fleetManagementClient.listMinerStateSnapshots);

function createMockResponse(
  totalMiners: number,
  counts: { hashing?: number; broken?: number; offline?: number; sleeping?: number },
) {
  return create(ListMinerStateSnapshotsResponseSchema, {
    totalMiners,
    totalStateCounts: create(MinerStateCountsSchema, {
      hashingCount: counts.hashing ?? 0,
      brokenCount: counts.broken ?? 0,
      offlineCount: counts.offline ?? 0,
      sleepingCount: counts.sleeping ?? 0,
    }),
  });
}

describe("useDeviceSetStateCounts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns initial state when filter is null", () => {
    const { result } = renderHook(() => useDeviceSetStateCounts(null));

    expect(result.current.totalMiners).toBe(0);
    expect(result.current.stateCounts).toBeUndefined();
    expect(result.current.hasInitialLoadCompleted).toBe(false);
    expect(mockListMinerStateSnapshots).not.toHaveBeenCalled();
  });

  it("fetches counts when filter is provided", async () => {
    mockListMinerStateSnapshots.mockResolvedValue(
      createMockResponse(42, { hashing: 30, broken: 5, offline: 4, sleeping: 3 }),
    );

    const { result } = renderHook(() => useDeviceSetStateCounts({ groupIds: [1n] }));

    await waitFor(() => {
      expect(result.current.hasInitialLoadCompleted).toBe(true);
    });

    expect(result.current.totalMiners).toBe(42);
    expect(result.current.stateCounts?.hashingCount).toBe(30);
    expect(result.current.stateCounts?.brokenCount).toBe(5);
    expect(result.current.stateCounts?.offlineCount).toBe(4);
    expect(result.current.stateCounts?.sleepingCount).toBe(3);
  });

  it("clears stale counts and refetches when filter changes", async () => {
    mockListMinerStateSnapshots.mockResolvedValue(createMockResponse(10, { hashing: 10 }));

    const { result, rerender } = renderHook(({ filter }) => useDeviceSetStateCounts(filter), {
      initialProps: { filter: { groupIds: [1n] } as { groupIds?: bigint[]; rackIds?: bigint[] } | null },
    });

    await waitFor(() => {
      expect(result.current.hasInitialLoadCompleted).toBe(true);
    });
    expect(result.current.totalMiners).toBe(10);

    // Change filter — stale counts should be cleared immediately
    mockListMinerStateSnapshots.mockResolvedValue(createMockResponse(20, { hashing: 15, offline: 5 }));

    rerender({ filter: { groupIds: [2n] } });

    // Before the new fetch resolves, state should be reset
    expect(result.current.totalMiners).toBe(0);
    expect(result.current.stateCounts).toBeUndefined();
    expect(result.current.hasInitialLoadCompleted).toBe(false);

    await waitFor(() => {
      expect(result.current.hasInitialLoadCompleted).toBe(true);
    });
    expect(result.current.totalMiners).toBe(20);
    expect(result.current.stateCounts?.offlineCount).toBe(5);
  });

  it("does not refetch when filter is the same reference", async () => {
    mockListMinerStateSnapshots.mockResolvedValue(createMockResponse(5, { hashing: 5 }));

    const stableFilter = { groupIds: [1n] };
    const { rerender } = renderHook(({ filter }) => useDeviceSetStateCounts(filter), {
      initialProps: { filter: stableFilter as { groupIds?: bigint[]; rackIds?: bigint[] } | null },
    });

    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
    });

    // Re-render with the same object reference (simulates useMemo stability)
    rerender({ filter: stableFilter });

    // Should not trigger another fetch
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
  });

  it("does not mark hasInitialLoadCompleted on error so consumers fall back to groupSize", async () => {
    mockListMinerStateSnapshots.mockRejectedValue(new Error("network error"));

    const { result } = renderHook(() => useDeviceSetStateCounts({ rackIds: [1n] }));

    // Wait for the fetch to settle
    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
    });

    // hasInitialLoadCompleted stays false so consumers use groupSize fallback
    expect(result.current.hasInitialLoadCompleted).toBe(false);
    expect(result.current.totalMiners).toBe(0);
    expect(result.current.stateCounts).toBeUndefined();
  });

  it("does not fetch when filter transitions to null", async () => {
    mockListMinerStateSnapshots.mockResolvedValue(createMockResponse(10, { hashing: 10 }));

    const { result, rerender } = renderHook(({ filter }) => useDeviceSetStateCounts(filter), {
      initialProps: { filter: { groupIds: [1n] } as { groupIds?: bigint[]; rackIds?: bigint[] } | null },
    });

    await waitFor(() => {
      expect(result.current.hasInitialLoadCompleted).toBe(true);
    });

    rerender({ filter: null });

    // Should not have made a second call
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
  });
});
