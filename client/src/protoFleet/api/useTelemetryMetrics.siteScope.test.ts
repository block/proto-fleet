import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { telemetryClient } from "./clients";
import { useTelemetryMetrics } from "./useTelemetryMetrics";

vi.mock("./clients", () => ({
  telemetryClient: {
    getCombinedMetrics: vi.fn(),
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: vi.fn(({ onError }: { onError: (err: unknown) => void }) => onError),
  })),
}));

const mockGetCombinedMetrics = vi.mocked(telemetryClient.getCombinedMetrics);

describe("useTelemetryMetrics site scope request construction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetCombinedMetrics.mockResolvedValue({ metrics: [] } as never);
  });

  it("sends no site filter by default", async () => {
    const { result } = renderHook(() => useTelemetryMetrics({ duration: "24h", enabled: true }));
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const req = mockGetCombinedMetrics.mock.calls[0][0];
    expect(req.siteIds).toEqual([]);
    expect(req.includeUnassigned).toBe(false);
    // Dashboard still selects all devices; site scope is an independent filter.
    expect(req.deviceSelector?.selectorValue?.case).toBe("allDevices");
  });

  it("sends site_ids for a specific-site scope", async () => {
    const { result } = renderHook(() => useTelemetryMetrics({ duration: "24h", enabled: true, siteIds: [7n] }));
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const req = mockGetCombinedMetrics.mock.calls[0][0];
    expect(req.siteIds).toEqual([7n]);
    expect(req.includeUnassigned).toBe(false);
  });

  it("sends include_unassigned for the unassigned scope", async () => {
    const { result } = renderHook(() =>
      useTelemetryMetrics({ duration: "24h", enabled: true, includeUnassigned: true }),
    );
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const req = mockGetCombinedMetrics.mock.calls[0][0];
    expect(req.includeUnassigned).toBe(true);
  });

  it("re-fetches when the active site changes", async () => {
    const { result, rerender } = renderHook(
      ({ siteIds }) => useTelemetryMetrics({ duration: "24h", enabled: true, siteIds }),
      {
        initialProps: { siteIds: [7n] as bigint[] },
      },
    );
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    rerender({ siteIds: [9n] });
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));

    const calls = mockGetCombinedMetrics.mock.calls;
    expect(calls[calls.length - 1][0].siteIds).toEqual([9n]);
    expect(calls.length).toBeGreaterThanOrEqual(2);
  });
});
