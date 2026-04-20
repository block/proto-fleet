import { beforeEach, describe, expect, it, vi } from "vitest";
import { fetchAllMinerSnapshots } from "./fetchAllMinerSnapshots";

const mockListMinerStateSnapshots = vi.fn();

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    listMinerStateSnapshots: (...args: unknown[]) => mockListMinerStateSnapshots(...args),
  },
}));

function minerSnapshot(deviceIdentifier: string) {
  return { deviceIdentifier } as { deviceIdentifier: string };
}

describe("fetchAllMinerSnapshots", () => {
  beforeEach(() => {
    mockListMinerStateSnapshots.mockReset();
  });

  it("returns a map from a single page", async () => {
    mockListMinerStateSnapshots.mockResolvedValueOnce({
      miners: [minerSnapshot("d1"), minerSnapshot("d2")],
      cursor: "",
    });

    const result = await fetchAllMinerSnapshots({ groupIds: [1n] });

    expect(result).toEqual({ d1: minerSnapshot("d1"), d2: minerSnapshot("d2") });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
    expect(mockListMinerStateSnapshots).toHaveBeenCalledWith(
      { pageSize: 1000, cursor: "", filter: { groupIds: [1n] } },
      { signal: undefined },
    );
  });

  it("accumulates results across multiple pages", async () => {
    mockListMinerStateSnapshots
      .mockResolvedValueOnce({
        miners: [minerSnapshot("d1"), minerSnapshot("d2")],
        cursor: "page2",
      })
      .mockResolvedValueOnce({
        miners: [minerSnapshot("d3")],
        cursor: "",
      });

    const result = await fetchAllMinerSnapshots({ rackIds: [5n] });

    expect(result).toEqual({
      d1: minerSnapshot("d1"),
      d2: minerSnapshot("d2"),
      d3: minerSnapshot("d3"),
    });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    expect(mockListMinerStateSnapshots).toHaveBeenNthCalledWith(
      2,
      { pageSize: 1000, cursor: "page2", filter: { rackIds: [5n] } },
      expect.anything(),
    );
  });

  it("throws AbortError when signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();

    await expect(fetchAllMinerSnapshots({}, controller.signal)).rejects.toThrow(
      expect.objectContaining({ name: "AbortError" }),
    );
    expect(mockListMinerStateSnapshots).not.toHaveBeenCalled();
  });

  it("throws when signal is aborted between pages", async () => {
    const controller = new AbortController();

    mockListMinerStateSnapshots.mockImplementationOnce(async () => {
      controller.abort();
      return { miners: [minerSnapshot("d1")], cursor: "page2" };
    });

    await expect(fetchAllMinerSnapshots({}, controller.signal)).rejects.toThrow(
      expect.objectContaining({ name: "AbortError" }),
    );
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
  });

  it("propagates RPC errors without returning partial data", async () => {
    const rpcError = new Error("server unavailable");

    mockListMinerStateSnapshots
      .mockResolvedValueOnce({
        miners: [minerSnapshot("d1")],
        cursor: "page2",
      })
      .mockRejectedValueOnce(rpcError);

    await expect(fetchAllMinerSnapshots({})).rejects.toThrow("server unavailable");
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
  });
});
