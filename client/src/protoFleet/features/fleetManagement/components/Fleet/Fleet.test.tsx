import { MemoryRouter } from "react-router-dom";
import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Fleet from "./Fleet";

// Mock all dependencies
vi.mock("@/protoFleet/api/useFleet", () => ({
  default: vi.fn(() => ({
    isInitialLoad: false,
    hasMore: false,
    isLoadingMiners: false,
    isFetching: false,
    loadMore: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/store", () => {
  const useFleetStore = Object.assign(
    vi.fn(() => ({
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
      },
    })),
    {
      getState: vi.fn(() => ({
        fleet: { setCurrentFilter: vi.fn() },
      })),
    },
  );
  return {
    useFleetStore,
    useFleetMiners: vi.fn(() => []),
    useIsLoading: vi.fn(() => false),
    useIsStreaming: vi.fn(() => false),
    useMinerIds: vi.fn(() => []),
    useTotalMiners: vi.fn(() => 0),
    useDeviceStatusCounts: vi.fn(() => ({})),
    useSetRefetchCallback: vi.fn(() => vi.fn()),
    useCleanupStaleBatches: vi.fn(() => vi.fn()),
    useLastPairingCompletedAt: vi.fn(() => 0),
    useNotifyPairingCompleted: vi.fn(() => vi.fn()),
    useBatchOperationCount: vi.fn(() => 0),
  };
});

vi.mock("@/protoFleet/api/useAuthNeededMiners", () => ({
  default: vi.fn(() => ({ totalMiners: 0 })),
}));

vi.mock("@/protoFleet/api/useBatchTelemetry", () => ({
  default: vi.fn(() => ({
    fetchBatchTelemetry: vi.fn(),
    resetFetchedIds: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/api/useDeviceErrors", () => ({
  useDeviceErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/useStreamDeviceErrors", () => ({
  useStreamDeviceErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/useStreamMinerListUpdates", () => ({
  default: vi.fn(),
}));

vi.mock("@/protoFleet/hooks", () => ({
  useVisibleMiners: vi.fn(() => ({
    visibleMinerIds: new Set(),
    registerMiner: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerList", () => ({
  default: () => <div data-testid="miner-list">MinerList</div>,
}));

vi.mock("@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup", () => ({
  default: () => <div data-testid="complete-setup">CompleteSetup</div>,
}));

vi.mock("@/protoFleet/features/onboarding/components/Miners", () => ({
  default: () => <div data-testid="miners">Miners</div>,
}));

// Helper to render Fleet with Router context
const renderFleet = () => {
  return render(
    <MemoryRouter>
      <Fleet />
    </MemoryRouter>,
  );
};

describe("Fleet - Stale Batch Cleanup", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("should setup cleanup interval on mount", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches = vi.fn();
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches);

    renderFleet();

    // Advance time by 60 seconds
    vi.advanceTimersByTime(60000);

    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1);
  });

  it("should call cleanup every 60 seconds", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches = vi.fn();
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches);

    renderFleet();

    // First interval
    vi.advanceTimersByTime(60000);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1);

    // Second interval
    vi.advanceTimersByTime(60000);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(2);

    // Third interval
    vi.advanceTimersByTime(60000);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(3);
  });

  it("should cleanup interval on unmount", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches = vi.fn();
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches);

    const { unmount } = renderFleet();

    // Advance time
    vi.advanceTimersByTime(60000);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1);

    // Unmount component
    unmount();

    // Advance time again - should not call cleanup after unmount
    vi.advanceTimersByTime(60000);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1); // Still 1, not 2
  });

  it("should handle cleanup function changes", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches1 = vi.fn();
    const cleanupStaleBatches2 = vi.fn();

    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches1);

    const { rerender } = renderFleet();

    vi.advanceTimersByTime(60000);
    expect(cleanupStaleBatches1).toHaveBeenCalledTimes(1);

    // Update the mock to return a new function
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches2);
    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    vi.advanceTimersByTime(60000);
    // After rerender, the new cleanup function should be called
    expect(cleanupStaleBatches2).toHaveBeenCalled();
  });
});

describe("Fleet - Telemetry Cache Reset", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should call resetFetchedIds when batch count decreases", async () => {
    const useBatchTelemetryModule = await import("@/protoFleet/api/useBatchTelemetry");
    const { useFleetStore, useBatchOperationCount } = await import("@/protoFleet/store");
    const resetFetchedIds = vi.fn();
    const fetchBatchTelemetry = vi.fn();

    vi.mocked(useBatchTelemetryModule.default).mockReturnValue({
      fetchBatchTelemetry,
      resetFetchedIds,
    });

    // Start with 2 batch operations
    const initialState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
            batch2: { id: "batch2" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(initialState);
      }
      return initialState;
    });

    // Mock batch count to return 2 initially
    vi.mocked(useBatchOperationCount).mockReturnValue(2);

    const { rerender } = renderFleet();

    // Clear mocks after initial render to only track calls from the batch count change
    vi.clearAllMocks();

    // Batch count decreases to 1 (a batch completed)
    const updatedState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(updatedState);
      }
      return updatedState;
    });

    // Mock batch count to return 1 after batch completion
    vi.mocked(useBatchOperationCount).mockReturnValue(1);

    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    expect(resetFetchedIds).toHaveBeenCalledTimes(1);
  });

  it("should refetch telemetry for visible miners when batch completes", async () => {
    const useBatchTelemetryModule = await import("@/protoFleet/api/useBatchTelemetry");
    const { useFleetStore, useBatchOperationCount } = await import("@/protoFleet/store");
    const { useVisibleMiners } = await import("@/protoFleet/hooks");
    const resetFetchedIds = vi.fn();
    const fetchBatchTelemetry = vi.fn();

    vi.mocked(useBatchTelemetryModule.default).mockReturnValue({
      fetchBatchTelemetry,
      resetFetchedIds,
    });

    const visibleMinerIds = new Set(["miner1", "miner2", "miner3"]);
    vi.mocked(useVisibleMiners).mockReturnValue({
      visibleMinerIds,
      registerMiner: vi.fn(),
    });

    // Start with 2 batch operations
    const initialState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
            batch2: { id: "batch2" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(initialState);
      }
      return initialState;
    });

    // Mock batch count to return 2 initially
    vi.mocked(useBatchOperationCount).mockReturnValue(2);

    const { rerender } = renderFleet();

    // Clear mocks after initial render to only track calls from the batch count change
    vi.clearAllMocks();

    // Batch count decreases to 1
    const updatedState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(updatedState);
      }
      return updatedState;
    });

    // Mock batch count to return 1 after batch completion
    vi.mocked(useBatchOperationCount).mockReturnValue(1);

    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    expect(resetFetchedIds).toHaveBeenCalledTimes(1);
    expect(fetchBatchTelemetry).toHaveBeenCalledWith(visibleMinerIds);
  });

  it("should not refetch when there are no visible miners", async () => {
    const useBatchTelemetryModule = await import("@/protoFleet/api/useBatchTelemetry");
    const { useFleetStore, useBatchOperationCount } = await import("@/protoFleet/store");
    const { useVisibleMiners } = await import("@/protoFleet/hooks");
    const resetFetchedIds = vi.fn();
    const fetchBatchTelemetry = vi.fn();

    vi.mocked(useBatchTelemetryModule.default).mockReturnValue({
      fetchBatchTelemetry,
      resetFetchedIds,
    });

    // No visible miners
    vi.mocked(useVisibleMiners).mockReturnValue({
      visibleMinerIds: new Set(),
      registerMiner: vi.fn(),
    });

    // Start with 2 batch operations
    const initialState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
            batch2: { id: "batch2" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(initialState);
      }
      return initialState;
    });

    // Mock batch count to return 2 initially
    vi.mocked(useBatchOperationCount).mockReturnValue(2);

    const { rerender } = renderFleet();

    // Clear mocks after initial render to only track calls from the batch count change
    vi.clearAllMocks();

    // Batch count decreases to 1
    const updatedState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(updatedState);
      }
      return updatedState;
    });

    // Mock batch count to return 1 after batch completion
    vi.mocked(useBatchOperationCount).mockReturnValue(1);

    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    expect(resetFetchedIds).toHaveBeenCalledTimes(1);
    expect(fetchBatchTelemetry).not.toHaveBeenCalled();
  });

  it("should not reset cache when batch count increases", async () => {
    const useBatchTelemetryModule = await import("@/protoFleet/api/useBatchTelemetry");
    const { useFleetStore, useBatchOperationCount } = await import("@/protoFleet/store");
    const resetFetchedIds = vi.fn();
    const fetchBatchTelemetry = vi.fn();

    vi.mocked(useBatchTelemetryModule.default).mockReturnValue({
      fetchBatchTelemetry,
      resetFetchedIds,
    });

    // Start with 1 batch operation
    const initialState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(initialState);
      }
      return initialState;
    });

    // Mock batch count to return 1 initially
    vi.mocked(useBatchOperationCount).mockReturnValue(1);

    const { rerender } = renderFleet();

    // Clear mocks after initial render to only track calls from the batch count change
    vi.clearAllMocks();

    // Batch count increases to 2 (new batch started)
    const updatedState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
            batch2: { id: "batch2" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(updatedState);
      }
      return updatedState;
    });

    // Mock batch count to return 2 after new batch starts
    vi.mocked(useBatchOperationCount).mockReturnValue(2);

    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    expect(resetFetchedIds).not.toHaveBeenCalled();
    expect(fetchBatchTelemetry).not.toHaveBeenCalled();
  });

  it("should not reset cache when batch count stays the same", async () => {
    const useBatchTelemetryModule = await import("@/protoFleet/api/useBatchTelemetry");
    const { useFleetStore, useBatchOperationCount } = await import("@/protoFleet/store");
    const resetFetchedIds = vi.fn();
    const fetchBatchTelemetry = vi.fn();

    vi.mocked(useBatchTelemetryModule.default).mockReturnValue({
      fetchBatchTelemetry,
      resetFetchedIds,
    });

    // Start with 2 batch operations
    const initialState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
            batch2: { id: "batch2" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(initialState);
      }
      return initialState;
    });

    // Mock batch count to return 2 initially
    vi.mocked(useBatchOperationCount).mockReturnValue(2);

    const { rerender } = renderFleet();

    // Clear mocks after initial render to only track calls from the batch count change
    vi.clearAllMocks();

    // Batch count stays at 2 (no change)
    const updatedState = {
      fleet: {
        isLoading: false,
        isStreaming: false,
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: {},
        setRefetchCallback: vi.fn(),
        batchOperations: {
          byBatchId: {
            batch1: { id: "batch1" },
            batch2: { id: "batch2" },
          },
        },
      },
    };

    vi.mocked(useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(updatedState);
      }
      return updatedState;
    });

    // Mock batch count to still return 2 (no change)
    vi.mocked(useBatchOperationCount).mockReturnValue(2);

    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    expect(resetFetchedIds).not.toHaveBeenCalled();
    expect(fetchBatchTelemetry).not.toHaveBeenCalled();
  });
});

describe("Fleet - Component Integration", () => {
  it("should render MinerList component", () => {
    const { getByTestId } = renderFleet();
    expect(getByTestId("miner-list")).toBeInTheDocument();
  });

  it("should render CompleteSetup component", () => {
    const { getByTestId } = renderFleet();
    expect(getByTestId("complete-setup")).toBeInTheDocument();
  });

  it("should call useFleet hook with correct parameters", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");
    const useFleet = useFleetModule.default;

    renderFleet();

    expect(useFleet).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: "global",
        pageSize: 50,
      }),
    );
  });
});
