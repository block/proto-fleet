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
    setFilter: vi.fn(),
    loadMore: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useFleet).mockReturnValue(mockUseFleetReturn);
  });

  it("calls useFleet with default options", () => {
    renderHook(() => useAuthNeededMiners());

    expect(useFleet).toHaveBeenCalledWith({
      scope: "local",
      pageSize: 100,
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
      mode: "metadata",
    });
  });

  it("calls useFleet with custom pageSize", () => {
    renderHook(() => useAuthNeededMiners({ pageSize: 50 }));

    expect(useFleet).toHaveBeenCalledWith({
      scope: "local",
      pageSize: 50,
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
      mode: "metadata",
    });
  });

  it("returns the same result as useFleet", () => {
    const { result } = renderHook(() => useAuthNeededMiners());

    expect(result.current).toEqual(mockUseFleetReturn);
  });

  it("uses local scope to avoid conflicting with global fleet view", () => {
    renderHook(() => useAuthNeededMiners());

    const callArgs = vi.mocked(useFleet).mock.calls[0]?.[0];
    expect(callArgs).toBeDefined();
    expect(callArgs?.scope).toBe("local");
  });

  it("filters for AUTHENTICATION_NEEDED pairing status only", () => {
    renderHook(() => useAuthNeededMiners());

    const callArgs = vi.mocked(useFleet).mock.calls[0]?.[0];
    expect(callArgs).toBeDefined();
    expect(callArgs?.pairingStatuses).toEqual([
      PairingStatus.AUTHENTICATION_NEEDED,
    ]);
  });

  it("uses metadata mode for minimal data transfer", () => {
    renderHook(() => useAuthNeededMiners());

    const callArgs = vi.mocked(useFleet).mock.calls[0]?.[0];
    expect(callArgs).toBeDefined();
    expect(callArgs?.mode).toBe("metadata");
  });

  describe("pagination", () => {
    it("exposes hasMore flag for pagination", () => {
      vi.mocked(useFleet).mockReturnValue({
        ...mockUseFleetReturn,
        hasMore: true,
      });

      const { result } = renderHook(() => useAuthNeededMiners());

      expect(result.current.hasMore).toBe(true);
    });

    it("exposes isLoading flag", () => {
      vi.mocked(useFleet).mockReturnValue({
        ...mockUseFleetReturn,
        isLoading: true,
      });

      const { result } = renderHook(() => useAuthNeededMiners());

      expect(result.current.isLoading).toBe(true);
    });

    it("exposes loadMore function for pagination", () => {
      const mockLoadMore = vi.fn();
      vi.mocked(useFleet).mockReturnValue({
        ...mockUseFleetReturn,
        loadMore: mockLoadMore,
      });

      const { result } = renderHook(() => useAuthNeededMiners());

      expect(result.current.loadMore).toBe(mockLoadMore);
      expect(typeof result.current.loadMore).toBe("function");
    });

    it("exposes totalMiners count", () => {
      vi.mocked(useFleet).mockReturnValue({
        ...mockUseFleetReturn,
        totalMiners: 42,
      });

      const { result } = renderHook(() => useAuthNeededMiners());

      expect(result.current.totalMiners).toBe(42);
    });

    it("exposes miners map for local scope", () => {
      const mockMiners = {
        "miner-1": create(MinerStateSnapshotSchema, {
          deviceIdentifier: "miner-1",
        }),
        "miner-2": create(MinerStateSnapshotSchema, {
          deviceIdentifier: "miner-2",
        }),
      };
      vi.mocked(useFleet).mockReturnValue({
        ...mockUseFleetReturn,
        miners: mockMiners,
      });

      const { result } = renderHook(() => useAuthNeededMiners());

      expect(result.current.miners).toEqual(mockMiners);
    });
  });
});
