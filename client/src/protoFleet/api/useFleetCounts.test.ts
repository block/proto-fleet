import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fleetManagementClient } from "./clients";
import useFleetCounts from "./useFleetCounts";

vi.mock("./clients", () => ({
  fleetManagementClient: {
    getMinerStateCounts: vi.fn(),
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

const mockGetMinerStateCounts = vi.mocked(fleetManagementClient.getMinerStateCounts);

const okResponse = {
  totalMiners: 5,
  stateCounts: { hashingCount: 5, brokenCount: 0, offlineCount: 0, sleepingCount: 0 },
} as never;

describe("useFleetCounts request site scope", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetMinerStateCounts.mockResolvedValue(okResponse);
  });

  it("sends no site filter for all-sites scope", async () => {
    const { result } = renderHook(() => useFleetCounts());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const req = mockGetMinerStateCounts.mock.calls[0][0];
    expect(req.siteIds).toEqual([]);
    expect(req.includeUnassigned).toBe(false);
  });

  it("does not fetch while disabled", () => {
    const { result } = renderHook(() => useFleetCounts({ enabled: false, siteIds: [7n] }));

    expect(result.current.hasLoaded).toBe(false);
    expect(result.current.isLoading).toBe(false);
    expect(mockGetMinerStateCounts).not.toHaveBeenCalled();
  });

  it("sends site_ids for a specific-site scope", async () => {
    const { result } = renderHook(() => useFleetCounts({ siteIds: [7n] }));
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const req = mockGetMinerStateCounts.mock.calls[0][0];
    expect(req.siteIds).toEqual([7n]);
    expect(req.includeUnassigned).toBe(false);
  });

  it("sends include_unassigned for the unassigned scope", async () => {
    const { result } = renderHook(() => useFleetCounts({ includeUnassigned: true }));
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const req = mockGetMinerStateCounts.mock.calls[0][0];
    expect(req.siteIds).toEqual([]);
    expect(req.includeUnassigned).toBe(true);
  });

  it("re-fetches with the new scope when the active site changes", async () => {
    const { result, rerender } = renderHook(({ siteIds }) => useFleetCounts({ siteIds }), {
      initialProps: { siteIds: [7n] as bigint[] },
    });
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(mockGetMinerStateCounts.mock.calls[0][0].siteIds).toEqual([7n]);

    // Switching site resets loaded state and issues a fresh request.
    rerender({ siteIds: [9n] });
    expect(result.current.hasLoaded).toBe(false);

    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const calls = mockGetMinerStateCounts.mock.calls;
    const lastReq = calls[calls.length - 1][0];
    expect(lastReq.siteIds).toEqual([9n]);
    expect(mockGetMinerStateCounts.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("does not mark failed first fetches as loaded", async () => {
    mockGetMinerStateCounts.mockRejectedValueOnce(new Error("database unavailable"));

    const { result } = renderHook(() => useFleetCounts({ siteIds: [7n] }));

    await waitFor(() => expect(mockHandleAuthErrors).toHaveBeenCalled());
    expect(result.current.hasLoaded).toBe(false);
    expect(result.current.stateCounts).toBeUndefined();
    expect(result.current.totalMiners).toBe(0);
  });
});
