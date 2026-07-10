import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MAX_SNAPSHOT_PAGES, resolveAllModeIds } from "./resolveAllModeMiners";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const mockListMinerStateSnapshots = vi.fn();

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    listMinerStateSnapshots: (...args: unknown[]) => mockListMinerStateSnapshots(...args),
  },
}));

const page = (ids: string[], cursor: string) => ({
  miners: ids.map((deviceIdentifier) => ({ deviceIdentifier })),
  cursor,
});

describe("resolveAllModeIds", () => {
  beforeEach(() => {
    mockListMinerStateSnapshots.mockReset();
  });

  it("aggregates ids and snapshots across pages until the cursor is empty", async () => {
    mockListMinerStateSnapshots
      .mockResolvedValueOnce(page(["a", "b"], "next"))
      .mockResolvedValueOnce(page(["c"], ""));

    const { ids, snapshots } = await resolveAllModeIds(create(MinerListFilterSchema, { rackIds: [7n] }));

    expect(ids).toEqual(["a", "b", "c"]);
    expect(Object.keys(snapshots).sort()).toEqual(["a", "b", "c"]);
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    // Second page forwards the cursor from the first response.
    expect(mockListMinerStateSnapshots.mock.calls[1][0].cursor).toBe("next");
  });

  it("throws when the result never exhausts (exceeds the page cap)", async () => {
    // Always return a cursor so pagination never completes.
    mockListMinerStateSnapshots.mockResolvedValue(page(["x"], "more"));

    await expect(resolveAllModeIds(create(MinerListFilterSchema, {}))).rejects.toThrow(/Too many miners/);
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(MAX_SNAPSHOT_PAGES);
  });

  it("returns the partial accumulation instead of throwing when aborted", async () => {
    const controller = new AbortController();
    mockListMinerStateSnapshots
      .mockResolvedValueOnce(page(["a", "b"], "next"))
      .mockImplementationOnce(() => {
        controller.abort();
        return Promise.resolve(page(["c"], "next"));
      });

    const { ids } = await resolveAllModeIds(create(MinerListFilterSchema, {}), controller.signal);

    // Pages accumulated before the abort are returned; the aborted page is dropped
    // and no error is thrown.
    expect(ids).toEqual(["a", "b"]);
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
  });
});
