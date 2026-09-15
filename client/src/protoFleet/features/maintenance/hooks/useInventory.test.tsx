import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

const listParts = vi.fn();
const getInsights = vi.fn();
const createPart = vi.fn();
const updatePart = vi.fn();
const deletePart = vi.fn();
const importCsv = vi.fn();
const confirmImport = vi.fn();
vi.mock("@/protoFleet/api/inventory", () => ({
  useInventoryApi: () => ({ listParts, getInsights, createPart, updatePart, deletePart, importCsv, confirmImport }),
}));
vi.mock("../mappers", () => ({
  toInventoryPart: (part: unknown) => part,
  toInventoryInsights: (value: unknown) => value,
}));
const { useInventory } = await import("./useInventory");

beforeEach(() => {
  vi.clearAllMocks();
  listParts.mockImplementation(async ({ pageToken, onSuccess }) =>
    onSuccess({
      parts: [{ id: pageToken === "next" ? "2" : "1" }],
      nextPageToken: pageToken ? "" : "next",
      totalCount: 2,
    }),
  );
  getInsights.mockImplementation(async ({ onSuccess }) =>
    onSuccess({
      totalOnHand: 3,
      totalAllocated: 1,
      lowStockCount: 1,
      sitesCount: 1,
      partTypes: ["Cooling"],
    }),
  );
});
it("loads parts and insights", async () => {
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.data).toEqual([{ id: "1" }]);
  expect(result.current.nextPageToken).toBe("next");
  expect(result.current.total).toBe(2);
  expect(result.current.insights?.partTypes).toEqual(["Cooling"]);
});

it("keeps visible inventory when organization-wide insights are unavailable", async () => {
  getInsights.mockImplementation(async ({ onError }) => onError("inventory insights require organization-wide access"));

  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(result.current.data).toEqual([{ id: "1" }]);
  expect(result.current.total).toBe(2);
  expect(result.current.insights).toBeNull();
  expect(result.current.error).toBeNull();
});

it("loads inventory and insights once until explicitly refreshed", async () => {
  vi.useFakeTimers();
  try {
    const { result } = renderHook(() => useInventory());
    await act(async () => undefined);
    expect(listParts).toHaveBeenCalledTimes(1);
    expect(getInsights).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(listParts).toHaveBeenCalledTimes(1);
    expect(getInsights).toHaveBeenCalledTimes(1);
    await act(() => result.current.refresh());

    expect(listParts).toHaveBeenCalledTimes(2);
    expect(getInsights).toHaveBeenCalledTimes(2);
  } finally {
    vi.useRealTimers();
  }
});

it("replaces inventory rows when navigating cursor pages", async () => {
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(() => result.current.nextPage());
  expect(result.current.data).toEqual([{ id: "2" }]);
  expect(result.current.currentPage).toBe(1);
  await act(() => result.current.previousPage());
  expect(result.current.data).toEqual([{ id: "1" }]);
  expect(result.current.currentPage).toBe(0);
});

it("returns to the previous page after deleting the final row on a later page", async () => {
  let deleted = false;
  listParts.mockImplementation(async ({ pageToken, onSuccess }) =>
    onSuccess({
      parts: pageToken === "next" ? (deleted ? [] : [{ id: "2" }]) : [{ id: "1" }],
      nextPageToken: pageToken || deleted ? "" : "next",
      totalCount: deleted ? 1 : 2,
    }),
  );
  deletePart.mockImplementation(async ({ onSuccess }) => {
    deleted = true;
    onSuccess();
  });
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(() => result.current.nextPage());
  expect(result.current.currentPage).toBe(1);

  const expectedUpdatedAt = {
    $typeName: "google.protobuf.Timestamp" as const,
    seconds: 1788609600n,
    nanos: 123456000,
  };
  await act(() => result.current.remove("2", expectedUpdatedAt));

  expect(deletePart).toHaveBeenCalledWith(expect.objectContaining({ id: 2n, expectedUpdatedAt }));
  expect(result.current.currentPage).toBe(0);
  expect(result.current.data).toEqual([{ id: "1" }]);
  expect(listParts).toHaveBeenCalledTimes(3);
});

it("returns to the previous page when refresh finds an empty later page", async () => {
  vi.useFakeTimers();
  let externallyRemoved = false;
  try {
    listParts.mockImplementation(async ({ pageToken, onSuccess }) =>
      onSuccess({
        parts: pageToken === "next" ? (externallyRemoved ? [] : [{ id: "2" }]) : [{ id: "1" }],
        nextPageToken: pageToken || externallyRemoved ? "" : "next",
        totalCount: externallyRemoved ? 1 : 2,
      }),
    );
    const { result } = renderHook(() => useInventory());
    await act(async () => undefined);
    await act(() => result.current.nextPage());
    expect(result.current.currentPage).toBe(1);

    externallyRemoved = true;
    await act(() => result.current.refresh());

    expect(result.current.currentPage).toBe(0);
    expect(result.current.data).toEqual([{ id: "1" }]);
  } finally {
    vi.useRealTimers();
  }
});

it("submits combined site, type, and low-stock filters", async () => {
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  act(() => result.current.setFilter({ siteIds: [2n], types: ["Cooling"], lowStockOnly: true }));
  await waitFor(() =>
    expect(listParts).toHaveBeenLastCalledWith(
      expect.objectContaining({ filter: { siteIds: [2n], types: ["Cooling"], lowStockOnly: true } }),
    ),
  );
});
it("ignores errors from an older filtered request", async () => {
  const requests: Array<{ onError: (message: string) => void }> = [];
  listParts.mockImplementation(({ onError }) => {
    requests.push({ onError });
    return new Promise(() => undefined);
  });
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(requests).toHaveLength(1));

  act(() => result.current.setFilter({ lowStockOnly: true }));
  await waitFor(() => expect(requests).toHaveLength(2));
  act(() => requests[0].onError("stale inventory failure"));

  expect(result.current.error).toBeNull();
});

it("does not refresh an obsolete inventory view after an adjustment completes", async () => {
  let finishAdjustment!: () => void;
  updatePart.mockImplementation(
    ({ onSuccess }) =>
      new Promise<void>((resolve) => {
        finishAdjustment = () => {
          onSuccess();
          resolve();
        };
      }),
  );
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));

  let adjustment!: Promise<boolean>;
  act(() => {
    adjustment = result.current.adjust({ id: 1n, onHand: 4, expectedOnHand: 5 });
  });
  await waitFor(() => expect(updatePart).toHaveBeenCalledOnce());
  act(() => result.current.setFilter({ siteIds: [2n] }));
  await waitFor(() => expect(listParts).toHaveBeenCalledTimes(2));

  await act(async () => {
    finishAdjustment();
    await adjustment;
  });

  expect(listParts).toHaveBeenCalledTimes(2);
  expect(listParts).toHaveBeenLastCalledWith(expect.objectContaining({ filter: { siteIds: [2n] } }));
});

it("passes exact CSV bytes to preview", async () => {
  importCsv.mockImplementation(async ({ onSuccess }) => onSuccess({ rows: [], validCount: 1, errorCount: 0 }));
  const bytes = new Uint8Array([3]);
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await result.current.previewCsv(bytes);
  expect(importCsv).toHaveBeenCalledWith(expect.objectContaining({ csvData: bytes }));
});
