import { act, renderHook, waitFor as waitForDefault } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient, fleetInventoryClient, fleetManagementClient } from "./clients";
import {
  EntityKind,
  EntityRefSchema,
  ListInventoryResponseSchema,
} from "./generated/fleetinventory/v1/fleetinventory_pb";
import { useDeviceErrors } from "./useDeviceErrors";
import useFleet from "./useFleet";
import {
  ListMinerStateSnapshotsResponseSchema,
  MinerListFilterSchema,
  MinerStateSnapshotSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// The default poll interval is 50 ms. That interval is most of the run time of this file.
const waitFor: typeof waitForDefault = (callback, options) => waitForDefault(callback, { interval: 1, ...options });

vi.mock("./clients", () => ({
  errorQueryClient: { query: vi.fn() },
  fleetInventoryClient: { listInventory: vi.fn() },
  fleetManagementClient: {
    listMinerStateSnapshots: vi.fn(),
  },
}));

const mockHandleAuthErrors = vi.fn(({ onError }) => onError?.(new Error("auth error")));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: {
    error: "error",
  },
}));

const makeMiner = (deviceIdentifier: string, workerName = "") =>
  create(MinerStateSnapshotSchema, {
    deviceIdentifier,
    workerName,
  });

const makeTimedMiner = (deviceIdentifier: string, workerName: string, seconds: number) =>
  create(MinerStateSnapshotSchema, {
    deviceIdentifier,
    workerName,
    timestamp: { seconds: BigInt(seconds), nanos: 0 },
  });

const makeListResponse = (miners: ReturnType<typeof makeMiner>[]) =>
  create(ListMinerStateSnapshotsResponseSchema, {
    miners,
    cursor: "",
    totalMiners: miners.length,
    models: [],
  });

describe("useFleet", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("settles with device errors while inventory is pending, failed, or disabled", async () => {
    let rejectInventory!: (error: Error) => void;
    vi.mocked(fleetInventoryClient.listInventory).mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectInventory = reject;
        }),
    );
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    let renders = 0;
    const { result, rerender } = renderHook(
      ({ enabled }) => {
        // Fail promptly if an unstable ID array makes the error hook render endlessly.
        if (++renders > 20) throw new Error("Inventory and device errors did not settle");
        const fleet = useFleet({ inventory: true, enabled });
        const errors = useDeviceErrors(fleet.minerIds);
        return { fleet, errors };
      },
      { initialProps: { enabled: true } },
    );
    expect(result.current.fleet.isLoading).toBe(true);
    const { minerIds, miners, inventoryItems } = result.current.fleet;
    rerender({ enabled: true });
    expect(result.current.fleet.minerIds).toBe(minerIds);
    expect(result.current.fleet.miners).toBe(miners);
    expect(result.current.fleet.inventoryItems).toBe(inventoryItems);

    await act(async () => rejectInventory(new Error("Inventory unavailable")));
    expect(result.current.fleet.isLoading).toBe(false);
    expect(result.current.fleet.minerIds).toBe(minerIds);
    expect(result.current.fleet.inventoryItems).toBe(inventoryItems);

    rerender({ enabled: false });
    expect(result.current.fleet.minerIds).toBe(minerIds);
    expect(result.current.fleet.miners).toBe(miners);
    expect(result.current.errors.errorsByDevice).toEqual({});
    expect(errorQueryClient.query).not.toHaveBeenCalled();
    consoleError.mockRestore();
  });

  it("clears the inventory miner count and ignores in-flight results when disabled", async () => {
    const response = create(ListInventoryResponseSchema, {
      items: [{ ref: { kind: EntityKind.MINER, id: 1n }, details: { case: "miner", value: makeMiner("miner-1") } }],
      totalCount: 12n,
      minerCount: 9n,
    });
    vi.mocked(fleetInventoryClient.listInventory).mockResolvedValueOnce(response);
    const { result, rerender } = renderHook(({ enabled }) => useFleet({ inventory: true, enabled }), {
      initialProps: { enabled: true },
    });
    await waitFor(() => expect(result.current.inventoryMinerCount).toBe(9));
    let resolve!: (value: typeof response) => void;
    vi.mocked(fleetInventoryClient.listInventory).mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    act(() => result.current.refreshCurrentPage());
    rerender({ enabled: false });
    expect(result.current.inventoryMinerCount).toBe(0);
    await act(async () => resolve(response));
    expect(result.current.inventoryMinerCount).toBe(0);
    expect(result.current.inventoryItems).toBeUndefined();
    vi.mocked(fleetInventoryClient.listInventory).mockResolvedValueOnce(
      create(ListInventoryResponseSchema, { minerCount: 2n }),
    );
    rerender({ enabled: true });
    await waitFor(() => expect(result.current.inventoryMinerCount).toBe(2));
  });

  it.each([EntityKind.CONTAINER, EntityKind.TANK])(
    "keeps ancestor scope %s across pages and hides previous rows on a contradictory scope",
    async (kind) => {
      const response = create(ListInventoryResponseSchema, {
        items: [{ ref: { kind: EntityKind.MODULE, id: 1n }, name: "North module" }],
        totalCount: 2n,
        nextPageToken: "next",
      });
      vi.mocked(fleetInventoryClient.listInventory)
        .mockResolvedValueOnce(response)
        .mockResolvedValueOnce({ ...response, nextPageToken: "" });
      const { result, rerender } = renderHook(
        ({ container }) =>
          useFleet({
            inventory: true,
            ancestors: [create(EntityRefSchema, { kind, id: container })],
            filter: create(MinerListFilterSchema, {
              siteIds: [1n],
              buildingIds: [7n],
              rackIds: [7n],
              includeNoRack: true,
            }),
            pageSize: 1,
          }),
        { initialProps: { container: 7n } },
      );
      await waitFor(() => expect(result.current.inventoryItems?.[0]?.name).toBe("North module"));
      act(() => result.current.goToNextPage());
      await waitFor(() => expect(result.current.currentPage).toBe(1));
      expect(vi.mocked(fleetInventoryClient.listInventory).mock.calls[1][0]).toMatchObject({
        ancestors: [
          { kind: EntityKind.BUILDING, id: 7n },
          { kind: EntityKind.RACK, id: 7n },
          { kind, id: 7n },
        ],
        includeNoGroup: true,
        siteIds: [1n],
        pageToken: "next",
      });
      let resolve!: (value: typeof response) => void;
      vi.mocked(fleetInventoryClient.listInventory).mockImplementationOnce(
        () =>
          new Promise((done) => {
            resolve = done;
          }),
      );
      rerender({ container: 8n });
      expect(result.current.inventoryItems).toEqual([]);
      expect(result.current.totalMiners).toBe(0);
      await act(async () => resolve(create(ListInventoryResponseSchema)));
      expect(result.current.inventoryItems).toEqual([]);
      expect(result.current.currentPage).toBe(0);
      expect(vi.mocked(fleetInventoryClient.listInventory).mock.calls[2][0].pageToken).toBe("");
    },
  );

  it("restores the native export/count placement filter when the last CT1 scope is cleared", async () => {
    const response = create(ListInventoryResponseSchema, { totalCount: 2n, nextPageToken: "next" });
    vi.mocked(fleetInventoryClient.listInventory).mockResolvedValue(response);
    const filter = create(MinerListFilterSchema, { buildingIds: [7n], includeNoRack: true, siteIds: [1n] });
    const { result, rerender } = renderHook(
      ({ ancestors }) => useFleet({ inventory: true, filter, ancestors, pageSize: 1 }),
      { initialProps: { ancestors: [create(EntityRefSchema, { kind: EntityKind.CONTAINER, id: 7n })] } },
    );
    await waitFor(() => expect(result.current.hasInitialLoadCompleted).toBe(true));
    expect(vi.mocked(fleetInventoryClient.listInventory).mock.calls[0][0]).toMatchObject({
      ancestors: [
        { kind: EntityKind.BUILDING, id: 7n },
        { kind: EntityKind.CONTAINER, id: 7n },
      ],
      includeNoGroup: true,
    });
    expect(vi.mocked(fleetInventoryClient.listInventory).mock.calls[0][0].minerFilter).toBeUndefined();

    rerender({ ancestors: [] });
    await waitFor(() => expect(fleetInventoryClient.listInventory).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    act(() => result.current.goToNextPage());
    await waitFor(() => expect(result.current.currentPage).toBe(1));
    for (const [request] of vi.mocked(fleetInventoryClient.listInventory).mock.calls.slice(1)) {
      expect(request).toMatchObject({
        ancestors: [],
        includeNoFacility: false,
        includeNoGroup: false,
        minerFilter: { buildingIds: [7n], includeNoRack: true, siteIds: [1n] },
      });
    }
    expect(vi.mocked(fleetInventoryClient.listInventory).mock.calls[2][0].pageToken).toBe("next");
    expect(filter.buildingIds).toEqual([7n]);
    expect(filter.includeNoRack).toBe(true);
  });

  it("queues a refetch requested while a fetch is already in flight", async () => {
    let resolveFirst: (value: ReturnType<typeof makeListResponse>) => void;

    const firstPromise = new Promise<ReturnType<typeof makeListResponse>>((resolve) => {
      resolveFirst = resolve;
    });

    vi.mocked(fleetManagementClient.listMinerStateSnapshots)
      .mockReturnValueOnce(firstPromise as Promise<any>)
      .mockResolvedValueOnce(makeListResponse([makeMiner("miner-2", "worker-new")]));

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await act(async () => {
      result.current.refetch();
    });

    expect(fleetManagementClient.listMinerStateSnapshots).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveFirst!(makeListResponse([makeMiner("miner-1", "worker-old")]));
    });

    await waitFor(() => {
      expect(fleetManagementClient.listMinerStateSnapshots).toHaveBeenCalledTimes(2);
      expect(result.current.minerIds).toEqual(["miner-2"]);
      expect(result.current.miners["miner-2"]?.workerName).toBe("worker-new");
    });
  });

  it("ignores stale responses when a newer request starts", async () => {
    let resolveFirst: (value: ReturnType<typeof makeListResponse>) => void;

    const firstPromise = new Promise<ReturnType<typeof makeListResponse>>((resolve) => {
      resolveFirst = resolve;
    });

    vi.mocked(fleetManagementClient.listMinerStateSnapshots)
      .mockReturnValueOnce(firstPromise as Promise<any>)
      .mockResolvedValueOnce(makeListResponse([makeMiner("fresh-miner", "fresh-worker")]));

    const initialFilter = create(MinerListFilterSchema, { models: ["initial-model"] });
    const updatedFilter = create(MinerListFilterSchema, { models: ["updated-model"] });

    const { result, rerender } = renderHook(({ filter }) => useFleet({ pageSize: 10, filter }), {
      initialProps: { filter: initialFilter },
    });

    rerender({ filter: updatedFilter });

    await waitFor(() => {
      expect(fleetManagementClient.listMinerStateSnapshots).toHaveBeenCalledTimes(2);
    });

    await waitFor(() => {
      expect(result.current.minerIds).toEqual(["fresh-miner"]);
      expect(result.current.miners["fresh-miner"]?.workerName).toBe("fresh-worker");
    });

    await act(async () => {
      resolveFirst!(makeListResponse([makeMiner("stale-miner", "stale-worker")]));
    });

    await waitFor(() => {
      expect(result.current.minerIds).toEqual(["fresh-miner"]);
      expect(result.current.miners["fresh-miner"]?.workerName).toBe("fresh-worker");
    });
  });

  it("updates a visible miner worker name locally before refetch reconciliation", async () => {
    vi.mocked(fleetManagementClient.listMinerStateSnapshots).mockResolvedValue(
      makeListResponse([makeMiner("miner-1", "worker-old")]),
    );

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-old");
    });

    act(() => {
      result.current.updateMinerWorkerName("miner-1", "worker-new");
    });

    expect(result.current.miners["miner-1"]?.workerName).toBe("worker-new");
  });

  it("merges refreshed miner snapshots by device identifier", async () => {
    vi.mocked(fleetManagementClient.listMinerStateSnapshots).mockResolvedValue(
      makeListResponse([makeMiner("miner-1", "worker-old")]),
    );

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-old");
    });

    act(() => {
      result.current.mergeMiners([makeMiner("miner-1", "worker-new"), makeMiner("miner-2", "worker-added")]);
    });

    expect(result.current.miners["miner-1"]?.workerName).toBe("worker-new");
    expect(result.current.miners["miner-2"]?.workerName).toBe("worker-added");
  });

  it("does not update miner state when refreshed snapshots are unchanged", async () => {
    vi.mocked(fleetManagementClient.listMinerStateSnapshots).mockResolvedValue(
      makeListResponse([makeMiner("miner-1", "worker-old")]),
    );

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-old");
    });

    const before = result.current.miners;

    act(() => {
      result.current.mergeMiners([makeMiner("miner-1", "worker-old")]);
    });

    expect(result.current.miners).toBe(before);
  });

  it("keeps a newer merged snapshot when a slower page response resolves afterward", async () => {
    // Initial page load and the later refetch both return the same older
    // snapshot (ts=100); a live-modal merge in between carries a newer one.
    vi.mocked(fleetManagementClient.listMinerStateSnapshots).mockResolvedValue(
      makeListResponse([makeTimedMiner("miner-1", "worker-old", 100)]),
    );

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-old");
    });

    act(() => {
      result.current.mergeMiners([makeTimedMiner("miner-1", "worker-fresh", 200)]);
    });
    expect(result.current.miners["miner-1"]?.workerName).toBe("worker-fresh");

    // A page poll that resolves after the merge carries the older snapshot; it
    // must not regress the device back to stale state.
    await act(async () => {
      result.current.refetch();
    });

    await waitFor(() => {
      expect(fleetManagementClient.listMinerStateSnapshots).toHaveBeenCalledTimes(2);
    });
    expect(result.current.miners["miner-1"]?.workerName).toBe("worker-fresh");
  });

  it("lets a newer page response win over an older existing snapshot", async () => {
    vi.mocked(fleetManagementClient.listMinerStateSnapshots)
      .mockResolvedValueOnce(makeListResponse([makeTimedMiner("miner-1", "worker-old", 100)]))
      .mockResolvedValueOnce(makeListResponse([makeTimedMiner("miner-1", "worker-newer", 300)]));

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-old");
    });

    await act(async () => {
      result.current.refetch();
    });

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-newer");
    });
  });

  it("does not let an older snapshot merge over a newer existing one", async () => {
    vi.mocked(fleetManagementClient.listMinerStateSnapshots).mockResolvedValue(
      makeListResponse([makeTimedMiner("miner-1", "worker-fresh", 200)]),
    );

    const { result } = renderHook(() => useFleet({ pageSize: 10 }));

    await waitFor(() => {
      expect(result.current.miners["miner-1"]?.workerName).toBe("worker-fresh");
    });

    const before = result.current.miners;

    act(() => {
      result.current.mergeMiners([makeTimedMiner("miner-1", "worker-stale", 100)]);
    });

    expect(result.current.miners).toBe(before);
    expect(result.current.miners["miner-1"]?.workerName).toBe("worker-fresh");
  });
});
