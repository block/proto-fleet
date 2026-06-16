import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useDefaultPasswordMiners from "./useDefaultPasswordMiners";
import useFleet from "./useFleet";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

vi.mock("./useFleet");

describe("useDefaultPasswordMiners", () => {
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

  it("calls useFleet with default-password pairing status", () => {
    renderHook(() => useDefaultPasswordMiners());

    expect(useFleet).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,
        pageSize: 100,
        pairingStatuses: [PairingStatus.DEFAULT_PASSWORD],
      }),
    );
  });

  it("passes through custom options", () => {
    renderHook(() => useDefaultPasswordMiners({ pageSize: 50, enabled: false }));

    expect(useFleet).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: false,
        pageSize: 50,
        pairingStatuses: [PairingStatus.DEFAULT_PASSWORD],
      }),
    );
  });

  it("returns the same result as useFleet", () => {
    const { result } = renderHook(() => useDefaultPasswordMiners());

    expect(result.current).toEqual(mockUseFleetReturn);
  });
});
