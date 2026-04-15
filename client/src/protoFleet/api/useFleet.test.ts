import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "./clients";
import useFleet from "./useFleet";
import {
  ListMinerStateSnapshotsResponseSchema,
  MinerListFilterSchema,
  MinerStateSnapshotSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

vi.mock("./clients", () => ({
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
});
