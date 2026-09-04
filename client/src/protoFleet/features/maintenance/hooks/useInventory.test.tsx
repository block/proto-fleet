import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

const listParts = vi.fn();
const getInsights = vi.fn();
const updatePart = vi.fn();
const importCsv = vi.fn();
const confirmImport = vi.fn();
vi.mock("@/protoFleet/api/inventory", () => ({
  useInventoryApi: () => ({ listParts, getInsights, updatePart, importCsv, confirmImport }),
}));
vi.mock("../mappers", () => ({
  toInventoryPart: (part: unknown) => part,
  toInventoryInsights: (value: unknown) => value,
}));
const { useInventory } = await import("./useInventory");

beforeEach(() => {
  vi.clearAllMocks();
  listParts.mockImplementation(async ({ onSuccess }) => onSuccess({ parts: [{ id: "1" }], nextPageToken: "next" }));
  getInsights.mockImplementation(async ({ onSuccess }) => onSuccess({ totalOnHand: 1 }));
});
it("loads parts and insights", async () => {
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.data).toEqual([{ id: "1" }]);
  expect(result.current.nextPageToken).toBe("next");
  expect(result.current.insights).toEqual({ totalOnHand: 1 });
});
it("passes exact CSV bytes to preview", async () => {
  importCsv.mockImplementation(async ({ onSuccess }) => onSuccess({ rows: [], validCount: 1, errorCount: 0 }));
  const bytes = new Uint8Array([3]);
  const { result } = renderHook(() => useInventory());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await result.current.previewCsv(bytes);
  expect(importCsv).toHaveBeenCalledWith(expect.objectContaining({ csvData: bytes }));
});
