import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import useAuthNeededMiners from "./useAuthNeededMiners";
import useFleet from "./useFleet";
import {
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

vi.mock("./useFleet");

describe("useAuthNeededMiners", () => {
  const mockUseFleetReturn = {
    minerIds: [],
    miners: {},
    totalMiners: 0,
    hasMore: false,
    isLoading: false,
    hasInitialLoadCompleted: true,
    loadMore: vi.fn(),
    currentPage: 0,
    hasPreviousPage: false,
    goToNextPage: vi.fn(),
    goToPrevPage: vi.fn(),
    refetch: vi.fn(),
    refreshCurrentPage: vi.fn(),
    updateMinerWorkerName: vi.fn(),
    mergeMiners: vi.fn(),
    availableModels: [],
    availableFirmwareVersions: [],
  };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useFleet).mockReturnValue(mockUseFleetReturn);
  });

  it("calls useFleet with default options", () => {
    renderHook(() => useAuthNeededMiners());

    expect(useFleet).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,

        pageSize: 100,
        pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
      }),
    );
  });

  it("calls useFleet with custom pageSize", () => {
    renderHook(() => useAuthNeededMiners({ pageSize: 50 }));

    expect(useFleet).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,

        pageSize: 50,
        pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
      }),
    );
  });

  it("returns the same result as useFleet", () => {
    const fleetResult = {
      ...mockUseFleetReturn,
      hasMore: true,
      isLoading: true,
      totalMiners: 42,
      miners: {
        "miner-1": create(MinerStateSnapshotSchema, { deviceIdentifier: "miner-1" }),
        "miner-2": create(MinerStateSnapshotSchema, { deviceIdentifier: "miner-2" }),
      },
    };
    vi.mocked(useFleet).mockReturnValue(fleetResult);

    const { result } = renderHook(() => useAuthNeededMiners());

    expect(result.current).toBe(fleetResult);
  });
});
