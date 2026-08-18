import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import { buildAlertHistoryEntry } from "@/protoFleet/features/alerts/alertHistory.fixtures";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import { usePagedAlerts } from "@/protoFleet/features/alerts/api/usePagedAlerts";

vi.mock("@/protoFleet/features/alerts/api/alertsApi", () => ({
  listHistory: vi.fn(),
}));

const listMock = vi.mocked(api.listHistory);

const page = (ids: string[], nextCursor = "") => ({
  alerts: ids.map((id) => buildAlertHistoryEntry({ id })),
  next_cursor: nextCursor,
});

describe("usePagedAlerts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reports a permission denial as both denied and an error", async () => {
    listMock.mockRejectedValue(new ConnectError("forbidden", Code.PermissionDenied));

    const { result } = renderHook(() => usePagedAlerts({}, "Failed to load alert history"));

    await waitFor(() => expect(result.current.denied).toBe(true));
    expect(result.current.error).toBeTruthy();
    expect(result.current.items).toEqual([]);
    expect(result.current.hasMore).toBe(false);
  });

  it("clears rows and the cursor when a denial lands mid-pagination", async () => {
    listMock.mockResolvedValueOnce(page(["1"], "1"));

    const { result } = renderHook(() => usePagedAlerts({}, "Failed to load alert history"));

    await waitFor(() => expect(result.current.items).toHaveLength(1));
    expect(result.current.hasMore).toBe(true);

    listMock.mockRejectedValueOnce(new ConnectError("forbidden", Code.PermissionDenied));
    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => expect(result.current.denied).toBe(true));
    expect(result.current.error).toBeTruthy();
    expect(result.current.items).toEqual([]);
    expect(result.current.hasMore).toBe(false);
  });

  it("drops a response that lands after the hook is disabled", async () => {
    let resolvePage!: (value: ReturnType<typeof page>) => void;
    listMock.mockReturnValueOnce(
      new Promise((resolve) => {
        resolvePage = resolve;
      }),
    );

    const { result, rerender } = renderHook(
      ({ enabled }) => usePagedAlerts({}, "Failed to load alert history", { enabled }),
      { initialProps: { enabled: true } },
    );
    expect(result.current.loading).toBe(true);

    rerender({ enabled: false });
    expect(result.current.loading).toBe(false);

    await act(async () => {
      resolvePage(page(["1"], "1"));
    });

    expect(result.current.items).toEqual([]);
    expect(result.current.hasMore).toBe(false);
  });

  it("ignores a stale response that lands after a newer request settles", async () => {
    let resolveStale!: (value: ReturnType<typeof page>) => void;
    listMock
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveStale = resolve;
        }),
      )
      .mockResolvedValueOnce(page(["fresh"]));

    const { result, rerender } = renderHook(
      ({ enabled }) => usePagedAlerts({}, "Failed to load alert history", { enabled }),
      { initialProps: { enabled: true } },
    );

    rerender({ enabled: false });
    rerender({ enabled: true });

    await waitFor(() => expect(result.current.items).toHaveLength(1));
    expect(result.current.items[0].id).toBe("fresh");

    await act(async () => {
      resolveStale(page(["stale-a", "stale-b"], "stale-cursor"));
    });

    expect(result.current.items.map((item) => item.id)).toEqual(["fresh"]);
    expect(result.current.hasMore).toBe(false);
  });

  it("keeps loaded rows on a non-permission failure and recovers on retry", async () => {
    listMock.mockResolvedValueOnce(page(["1"], "1")).mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => usePagedAlerts({}, "Failed to load alert history"));

    await waitFor(() => expect(result.current.items).toHaveLength(1));
    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => expect(result.current.error).toBe("boom"));
    expect(result.current.denied).toBe(false);
    expect(result.current.items).toHaveLength(1);

    listMock.mockResolvedValueOnce(page(["0"]));
    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => expect(result.current.items).toHaveLength(2));
    expect(result.current.error).toBeNull();
    expect(result.current.hasMore).toBe(false);
  });
});
