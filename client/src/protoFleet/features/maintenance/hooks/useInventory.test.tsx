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

  await act(() => result.current.remove("2"));

  expect(result.current.currentPage).toBe(0);
  expect(result.current.data).toEqual([{ id: "1" }]);
  expect(listParts).toHaveBeenCalledTimes(4);
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
it("passes exact CSV bytes to preview", async () => {
  importCsv.mockImplementation(async ({ onSuccess }) => onSuccess({ rows: [], validCount: 1, errorCount: 0 }));
  const bytes = new Uint8Array([3]);
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await result.current.previewCsv(bytes);
  expect(importCsv).toHaveBeenCalledWith(expect.objectContaining({ csvData: bytes }));
});
