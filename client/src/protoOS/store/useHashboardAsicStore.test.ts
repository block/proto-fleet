import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import useHashboardAsicStore from "./useHashboardAsicStore";

describe("useHashboardAsicStore", () => {
  beforeEach(() => {
    // Clear the store before each test
    const { clearAllHashboards } = useHashboardAsicStore.getState();
    clearAllHashboards();
  });

  it("should initialize empty store", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    expect(result.current.hashboards.size).toBe(0);
    expect(result.current.getHashboard("HB123")).toBeUndefined();
  });

  it("should initialize ASIC correctly", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
    });

    const asic = result.current.getAsic("HB123", 0);
    expect(asic).toBeDefined();
    expect(asic?.id).toBe(0);
    expect(asic?.hashboardSerial).toBe("HB123");
    expect(asic?.tempHistory).toEqual([]);
    expect(asic?.hashrateHistory).toEqual([]);
  });

  it("should update current ASIC data", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.updateAsicCurrentData("HB123", 0, {
        temp: 65.5,
        hashrate: 100.2,
      });
    });

    const asic = result.current.getAsic("HB123", 0);
    expect(asic?.temp_c).toBe(65.5);
    expect(asic?.hashrate_ghs).toBe(100.2);
    expect(asic?.lastUpdated).toBeDefined();
  });

  it("should update historical ASIC data", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    const tempHistory = [
      { datetime: 1704067200, value: 65 },
      { datetime: 1704070800, value: 67 },
    ];

    const tempAggregates = {
      min: 65,
      max: 67,
      avg: 66,
    };

    act(() => {
      result.current.updateAsicHistoricalData("HB123", 0, {
        tempHistory,
        tempAggregates,
      });
    });

    const asic = result.current.getAsic("HB123", 0);
    expect(asic?.tempHistory).toEqual(tempHistory);
    expect(asic?.tempAggregates).toEqual(tempAggregates);
    expect(asic?.lastHistoricalUpdate).toBeDefined();
  });

  it("should get all ASICs for a hashboard", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
      result.current.initializeAsic("HB123", 1);
      result.current.initializeAsic("HB123", 2);
    });

    const asics = result.current.getAllAsics("HB123");
    expect(asics).toHaveLength(3);
    expect(asics.map((a) => a.id)).toEqual([0, 1, 2]);
  });

  it("should get ASIC IDs for a hashboard", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 5);
      result.current.initializeAsic("HB123", 10);
      result.current.initializeAsic("HB123", 15);
    });

    const asicIds = result.current.getAsicIds("HB123");
    expect(asicIds).toEqual([5, 10, 15]);
  });

  it("should handle multiple hashboards", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
      result.current.initializeAsic("HB456", 0);
      result.current.initializeAsic("HB456", 1);
    });

    expect(result.current.getAllAsics("HB123")).toHaveLength(1);
    expect(result.current.getAllAsics("HB456")).toHaveLength(2);
    expect(result.current.hashboards.size).toBe(2);
  });

  it("should clear hashboard correctly", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
      result.current.initializeAsic("HB456", 0);
    });

    expect(result.current.hashboards.size).toBe(2);

    act(() => {
      result.current.clearHashboard("HB123");
    });

    expect(result.current.hashboards.size).toBe(1);
    expect(result.current.getHashboard("HB123")).toBeUndefined();
    expect(result.current.getHashboard("HB456")).toBeDefined();
  });

  it("should clear all hashboards", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
      result.current.initializeAsic("HB456", 0);
    });

    expect(result.current.hashboards.size).toBe(2);

    act(() => {
      result.current.clearAllHashboards();
    });

    expect(result.current.hashboards.size).toBe(0);
  });

  // Test new ergonomic methods
  it("should update multiple ASICs in batch", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    const updates = [
      {
        asicId: 0,
        currentData: { temp: 65.5, hashrate: 100.2 },
        historicalData: {
          tempHistory: [{ datetime: 1704067200, value: 65 }],
          tempAggregates: { min: 65, max: 67, avg: 66 },
        },
      },
      {
        asicId: 1,
        currentData: { temp: 68.0, hashrate: 98.5 },
      },
    ];

    act(() => {
      result.current.updateMultipleAsics("HB123", updates);
    });

    const asic0 = result.current.getAsic("HB123", 0);
    const asic1 = result.current.getAsic("HB123", 1);

    expect(asic0?.temp_c).toBe(65.5);
    expect(asic0?.hashrate_ghs).toBe(100.2);
    expect(asic0?.tempHistory).toHaveLength(1);
    expect(asic0?.tempAggregates?.avg).toBe(66);

    expect(asic1?.temp_c).toBe(68.0);
    expect(asic1?.hashrate_ghs).toBe(98.5);
  });

  it("should update single ASIC temperature", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
      result.current.updateAsicTemp("HB123", 0, 72.5);
    });

    const asic = result.current.getAsic("HB123", 0);
    expect(asic?.temp_c).toBe(72.5);
    expect(asic?.lastUpdated).toBeDefined();
  });

  it("should update single ASIC hashrate", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    act(() => {
      result.current.initializeAsic("HB123", 0);
      result.current.updateAsicHashrate("HB123", 0, 105.8);
    });

    const asic = result.current.getAsic("HB123", 0);
    expect(asic?.hashrate_ghs).toBe(105.8);
    expect(asic?.lastUpdated).toBeDefined();
  });

  it("should initialize multiple ASICs in bulk", () => {
    const { result } = renderHook(() => useHashboardAsicStore());

    const initialData = { temp_c: 70 };

    act(() => {
      result.current.initializeHashboardAsics(
        "HB123",
        [0, 1, 2, 3],
        initialData,
      );
    });

    const asics = result.current.getAllAsics("HB123");
    expect(asics).toHaveLength(4);

    asics.forEach((asic, index) => {
      expect(asic.id).toBe(index);
      expect(asic.temp_c).toBe(70);
      expect(asic.hashboardSerial).toBe("HB123");
    });
  });
});
