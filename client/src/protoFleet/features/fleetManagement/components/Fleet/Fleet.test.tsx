import { MemoryRouter } from "react-router-dom";
import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { POLL_INTERVAL_MS } from "./constants";
import Fleet from "./Fleet";

const { mockMinerList } = vi.hoisted(() => ({
  mockMinerList: vi.fn(() => <div data-testid="miner-list">MinerList</div>),
}));

// Mock all dependencies
vi.mock("@/protoFleet/api/useFleet", () => ({
  default: vi.fn(() => ({
    minerIds: [],
    totalMiners: 0,
    availableModels: [],
    currentPage: 0,
    hasPreviousPage: false,
    isInitialLoad: false,
    hasMore: false,
    hasInitialLoadCompleted: false,
    isLoading: false,
    loadMore: vi.fn(),
    goToNextPage: vi.fn(),
    goToPrevPage: vi.fn(),
    refetch: vi.fn(),
    refreshCurrentPage: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/store", () => {
  const useFleetStore = Object.assign(
    vi.fn(() => ({
      fleet: {
        isLoading: false,
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
    useMinerIds: vi.fn(() => []),
    useTotalMiners: vi.fn(() => 0),
    useDeviceStatusCounts: vi.fn(() => ({})),
    useSetRefetchCallback: vi.fn(() => vi.fn()),
    useCleanupStaleBatches: vi.fn(() => vi.fn()),
    useNotifyPairingCompleted: vi.fn(() => vi.fn()),
    useAuthErrors: vi.fn(() => ({ handleAuthErrors: vi.fn() })),
    useTemperatureUnit: vi.fn(() => "C"),
  };
});

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: vi.fn(() => ({
    listGroups: vi.fn(),
    listRacks: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/api/useAuthNeededMiners", () => ({
  default: vi.fn(() => ({ totalMiners: 0 })),
}));

vi.mock("@/protoFleet/api/useDeviceErrors", () => ({
  useDeviceErrors: vi.fn(() => ({ refetch: vi.fn() })),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerList", () => ({
  default: mockMinerList,
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

    // Advance time by poll interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);

    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1);
  });

  it("should call cleanup at poll interval", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches = vi.fn();
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches);

    renderFleet();

    // First interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1);

    // Second interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(2);

    // Third interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(3);
  });

  it("should cleanup interval on unmount", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches = vi.fn();
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches);

    const { unmount } = renderFleet();

    // Advance time
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1);

    // Unmount component
    unmount();

    // Advance time again - should not call cleanup after unmount
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(cleanupStaleBatches).toHaveBeenCalledTimes(1); // Still 1, not 2
  });

  it("should handle cleanup function changes", async () => {
    const { useCleanupStaleBatches } = await import("@/protoFleet/store");
    const cleanupStaleBatches1 = vi.fn();
    const cleanupStaleBatches2 = vi.fn();

    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches1);

    const { rerender } = renderFleet();

    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(cleanupStaleBatches1).toHaveBeenCalledTimes(1);

    // Update the mock to return a new function
    vi.mocked(useCleanupStaleBatches).mockReturnValue(cleanupStaleBatches2);
    rerender(
      <MemoryRouter>
        <Fleet />
      </MemoryRouter>,
    );

    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    // After rerender, the new cleanup function should be called
    expect(cleanupStaleBatches2).toHaveBeenCalled();
  });
});

describe("Fleet - Polling", () => {
  let mockRefreshCurrentPage: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    vi.resetModules();
    vi.clearAllMocks();
    vi.useFakeTimers();

    mockRefreshCurrentPage = vi.fn();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("should setup polling interval after initial load completes", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue({
      minerIds: ["miner1"],
      totalMiners: 1,
      hasMore: false,
      hasInitialLoadCompleted: true,
      isLoading: false,
      refetch: vi.fn() as () => void,
      refreshCurrentPage: mockRefreshCurrentPage as () => void,
      loadMore: vi.fn() as () => void,
      availableModels: [],
      currentPage: 1,
      hasPreviousPage: false,
      goToNextPage: vi.fn() as () => void,
      goToPrevPage: vi.fn() as () => void,
    });

    renderFleet();

    // Advance time by poll interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);

    expect(mockRefreshCurrentPage).toHaveBeenCalled();
  });

  it("should not poll before initial load completes", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue({
      minerIds: [],
      totalMiners: 0,
      hasMore: false,
      hasInitialLoadCompleted: false,
      isLoading: false,
      refetch: vi.fn() as () => void,
      refreshCurrentPage: mockRefreshCurrentPage as () => void,
      loadMore: vi.fn() as () => void,
      availableModels: [],
      currentPage: 1,
      hasPreviousPage: false,
      goToNextPage: vi.fn() as () => void,
      goToPrevPage: vi.fn() as () => void,
    });

    renderFleet();

    // Advance time by poll interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);

    expect(mockRefreshCurrentPage).not.toHaveBeenCalled();
  });

  it("should poll repeatedly at the configured interval", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue({
      minerIds: ["miner1"],
      totalMiners: 1,
      hasMore: false,
      hasInitialLoadCompleted: true,
      isLoading: false,
      refetch: vi.fn() as () => void,
      refreshCurrentPage: mockRefreshCurrentPage as () => void,
      loadMore: vi.fn() as () => void,
      availableModels: [],
      currentPage: 1,
      hasPreviousPage: false,
      goToNextPage: vi.fn() as () => void,
      goToPrevPage: vi.fn() as () => void,
    });

    renderFleet();

    // First poll
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    const callsAfterFirst = mockRefreshCurrentPage.mock.calls.length;
    expect(callsAfterFirst).toBeGreaterThan(0);

    // Second poll
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(mockRefreshCurrentPage.mock.calls.length).toBeGreaterThan(callsAfterFirst);
  });

  it("should cleanup polling interval on unmount", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue({
      minerIds: ["miner1"],
      totalMiners: 1,
      hasMore: false,
      hasInitialLoadCompleted: true,
      isLoading: false,
      refetch: vi.fn() as () => void,
      refreshCurrentPage: mockRefreshCurrentPage as () => void,
      loadMore: vi.fn() as () => void,
      availableModels: [],
      currentPage: 1,
      hasPreviousPage: false,
      goToNextPage: vi.fn() as () => void,
      goToPrevPage: vi.fn() as () => void,
    });

    const { unmount } = renderFleet();

    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    const callsBeforeUnmount = mockRefreshCurrentPage.mock.calls.length;
    expect(callsBeforeUnmount).toBeGreaterThan(0);

    unmount();

    // Advance time again - should not poll after unmount
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(mockRefreshCurrentPage.mock.calls.length).toBe(callsBeforeUnmount);
  });
});

describe("Fleet - Component Integration", () => {
  beforeEach(() => {
    mockMinerList.mockClear();
  });

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

  it("shows the loading state during sort refetches even when miners are already present", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue({
      minerIds: ["miner-1"],
      totalMiners: 1,
      hasMore: false,
      hasInitialLoadCompleted: false,
      isLoading: true,
      refetch: vi.fn() as () => void,
      refreshCurrentPage: vi.fn() as () => void,
      loadMore: vi.fn() as () => void,
      availableModels: [],
      currentPage: 0,
      hasPreviousPage: false,
      goToNextPage: vi.fn() as () => void,
      goToPrevPage: vi.fn() as () => void,
    });

    renderFleet();

    expect(mockMinerList).toHaveBeenCalledWith(expect.objectContaining({ loading: true }), undefined);
  });
});
