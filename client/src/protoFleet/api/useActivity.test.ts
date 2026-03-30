import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { activityClient } from "./clients";
import { useActivity } from "./useActivity";
import {
  ActivityEntrySchema,
  type ActivityFilter,
  ActivityFilterSchema,
  ListActivitiesResponseSchema,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";

vi.mock("./clients", () => ({
  activityClient: {
    listActivities: vi.fn(),
  },
}));

const mockHandleAuthErrors = vi.fn(({ onError }) => onError?.(new Error("auth error")));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

function makeEntry(id: string) {
  return create(ActivityEntrySchema, {
    eventId: id,
    eventCategory: "auth",
    eventType: "login",
    description: `Entry ${id}`,
    result: "success",
    actorType: "user",
  });
}

function mockListResponse(entries: ReturnType<typeof makeEntry>[], nextPageToken = "", totalCount = 0) {
  return create(ListActivitiesResponseSchema, { activities: entries, nextPageToken, totalCount });
}

describe("useActivity", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("fetches activities on mount with correct params", async () => {
    const entries = [makeEntry("1"), makeEntry("2")];
    vi.mocked(activityClient.listActivities).mockResolvedValue(mockListResponse(entries, "", 2));

    const { result } = renderHook(() => useActivity({ pageSize: 25 }));

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(activityClient.listActivities).toHaveBeenCalledWith(
      expect.objectContaining({ pageSize: 25, pageToken: "" }),
    );
    expect(result.current.activities).toHaveLength(2);
    expect(result.current.totalCount).toBe(2);
    expect(result.current.hasMore).toBe(false);
  });

  it("loadMore appends next page of results", async () => {
    const page1 = [makeEntry("1")];
    const page2 = [makeEntry("2")];

    vi.mocked(activityClient.listActivities)
      .mockResolvedValueOnce(mockListResponse(page1, "token-2", 2))
      .mockResolvedValueOnce(mockListResponse(page2, "", 2));

    const { result } = renderHook(() => useActivity({ pageSize: 1 }));

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });
    expect(result.current.activities).toHaveLength(1);
    expect(result.current.hasMore).toBe(true);

    await act(async () => {
      result.current.loadMore();
    });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(activityClient.listActivities).toHaveBeenCalledTimes(2);
    expect(activityClient.listActivities).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "token-2" }));
    expect(result.current.activities).toHaveLength(2);
    expect(result.current.hasMore).toBe(false);
  });

  it("discards stale responses when a newer request starts", async () => {
    let resolveFirst: (value: ReturnType<typeof mockListResponse>) => void;
    const firstPromise = new Promise<ReturnType<typeof mockListResponse>>((r) => {
      resolveFirst = r;
    });

    const staleEntries = [makeEntry("stale")];
    const freshEntries = [makeEntry("fresh")];

    vi.mocked(activityClient.listActivities)
      .mockReturnValueOnce(firstPromise as Promise<any>)
      .mockResolvedValueOnce(mockListResponse(freshEntries, "", 1));

    const filter1 = create(ActivityFilterSchema, { searchText: "old" });
    const filter2 = create(ActivityFilterSchema, { searchText: "new" });

    const { result, rerender } = renderHook(({ filter }) => useActivity({ filter }), {
      initialProps: { filter: filter1 as ActivityFilter },
    });

    // Trigger second fetch via filter change before first resolves
    rerender({ filter: filter2 as ActivityFilter });

    await waitFor(() => {
      expect(activityClient.listActivities).toHaveBeenCalledTimes(2);
    });

    // Now resolve the stale first request
    resolveFirst!(mockListResponse(staleEntries, "", 99));

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    // Fresh result wins, stale result discarded
    expect(result.current.activities[0].eventId).toBe("fresh");
    expect(result.current.totalCount).toBe(1);
  });

  it("refresh resets state and re-fetches from page 1", async () => {
    const initialEntries = [makeEntry("1")];
    const refreshedEntries = [makeEntry("refreshed")];

    vi.mocked(activityClient.listActivities)
      .mockResolvedValueOnce(mockListResponse(initialEntries, "tok", 10))
      .mockResolvedValueOnce(mockListResponse(refreshedEntries, "", 5));

    const { result } = renderHook(() => useActivity({}));

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });
    expect(result.current.totalCount).toBe(10);
    expect(result.current.hasMore).toBe(true);

    await act(async () => {
      result.current.refresh();
    });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(activityClient.listActivities).toHaveBeenCalledTimes(2);
    expect(activityClient.listActivities).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "" }));
    expect(result.current.activities).toHaveLength(1);
    expect(result.current.activities[0].eventId).toBe("refreshed");
    expect(result.current.totalCount).toBe(5);
    expect(result.current.hasMore).toBe(false);
  });

  it("re-fetches when filter changes", async () => {
    vi.mocked(activityClient.listActivities).mockResolvedValue(mockListResponse([], "", 0));

    const filter1 = create(ActivityFilterSchema, { searchText: "alpha" });
    const filter2 = create(ActivityFilterSchema, { searchText: "beta" });

    const { rerender } = renderHook(({ filter }) => useActivity({ filter }), {
      initialProps: { filter: filter1 as ActivityFilter },
    });

    await waitFor(() => {
      expect(activityClient.listActivities).toHaveBeenCalledTimes(1);
    });

    rerender({ filter: filter2 as ActivityFilter });

    await waitFor(() => {
      expect(activityClient.listActivities).toHaveBeenCalledTimes(2);
    });
  });

  it("does not re-fetch when filter content is identical (deep equality)", async () => {
    vi.mocked(activityClient.listActivities).mockResolvedValue(mockListResponse([], "", 0));

    const filter1 = create(ActivityFilterSchema, { searchText: "same" });
    const filter2 = create(ActivityFilterSchema, { searchText: "same" });

    const { rerender } = renderHook(({ filter }) => useActivity({ filter }), {
      initialProps: { filter: filter1 as ActivityFilter },
    });

    await waitFor(() => {
      expect(activityClient.listActivities).toHaveBeenCalledTimes(1);
    });

    rerender({ filter: filter2 as ActivityFilter });

    // Should still be 1 call -- no re-fetch for identical content
    await waitFor(() => {
      expect(activityClient.listActivities).toHaveBeenCalledTimes(1);
    });
  });

  it("handles auth errors and sets error state", async () => {
    const testError = new Error("network failure");
    vi.mocked(activityClient.listActivities).mockRejectedValue(testError);

    const { result } = renderHook(() => useActivity({}));

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(mockHandleAuthErrors).toHaveBeenCalledWith(expect.objectContaining({ error: testError }));
    expect(result.current.error).toBeTruthy();
    expect(result.current.activities).toHaveLength(0);
  });
});
