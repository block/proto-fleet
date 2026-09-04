import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AdjustmentReason } from "./generated/inventory/v1/inventory_pb";

const clients = {
  listInventoryParts: vi.fn(),
  getInventoryPart: vi.fn(),
  createInventoryPart: vi.fn(),
  updateInventoryPart: vi.fn(),
  deleteInventoryPart: vi.fn(),
  getInventoryInsights: vi.fn(),
  listPartsBySite: vi.fn(),
  importInventoryCsv: vi.fn(),
  confirmInventoryImport: vi.fn(),
};
vi.mock("./clients", () => ({ inventoryClient: clients }));
const handleAuthErrors = vi.fn();
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors }) }));
const { useInventoryApi } = await import("./inventory");

describe("useInventoryApi", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    handleAuthErrors.mockImplementation(({ onError, error }) => onError?.(error));
  });
  it("maps filters and pagination", async () => {
    clients.listInventoryParts.mockResolvedValue({ parts: [], nextPageToken: "next", totalCount: 42 });
    const onSuccess = vi.fn();
    const { result } = renderHook(() => useInventoryApi());
    await act(() =>
      result.current.listParts({
        filter: { siteIds: [8n], types: ["fan"], lowStockOnly: true },
        pageSize: 20,
        pageToken: "cursor",
        onSuccess,
      }),
    );
    expect(clients.listInventoryParts).toHaveBeenCalledWith(
      { filter: { siteIds: [8n], types: ["fan"], lowStockOnly: true }, pageSize: 20, pageToken: "cursor" },
      expect.anything(),
    );
    expect(onSuccess).toHaveBeenCalledWith({ parts: [], nextPageToken: "next", totalCount: 42 });
  });
  it("maps adjustment values and CSV bytes", async () => {
    clients.updateInventoryPart.mockResolvedValue({});
    clients.importInventoryCsv.mockResolvedValue({ rows: [], validCount: 2, errorCount: 0 });
    const bytes = new Uint8Array([1, 2]);
    const { result } = renderHook(() => useInventoryApi());
    await act(() =>
      result.current.updatePart({ id: 2n, onHand: 10, siteId: 8n, reason: AdjustmentReason.CYCLE_COUNT }),
    );
    await act(() => result.current.importCsv({ csvData: bytes }));
    expect(clients.updateInventoryPart).toHaveBeenCalledWith(
      expect.objectContaining({ id: 2n, onHand: 10, siteId: 8n, reason: AdjustmentReason.CYCLE_COUNT }),
      expect.anything(),
    );
    expect(clients.importInventoryCsv).toHaveBeenCalledWith({ csvData: bytes }, expect.anything());
  });
  it("routes failures through auth handling and finalizes once", async () => {
    const error = new Error("nope");
    clients.getInventoryInsights.mockRejectedValue(error);
    const onError = vi.fn();
    const onFinally = vi.fn();
    const { result } = renderHook(() => useInventoryApi());
    await act(() => result.current.getInsights({ onError, onFinally }));
    expect(handleAuthErrors).toHaveBeenCalledWith(expect.objectContaining({ error }));
    expect(onError).toHaveBeenCalledWith("nope");
    expect(onFinally).toHaveBeenCalledOnce();
  });
});
