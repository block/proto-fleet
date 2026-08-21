import { MemoryRouter } from "react-router-dom";
import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { POLL_INTERVAL_MS } from "./constants";
import Fleet from "./Fleet";

const {
  mockMinerList,
  mockRefetchAuthNeededMiners,
  mockRefetchErrors,
  mockListAllBuildings,
  mockUseHasPermission,
  mockNotifyMinersChanged,
  mockFleetOutletContext,
} = vi.hoisted(() => ({
  mockMinerList: vi.fn(() => <div data-testid="miner-list">MinerList</div>),
  mockRefetchAuthNeededMiners: vi.fn(),
  mockRefetchErrors: vi.fn(),
  mockListAllBuildings: vi.fn(),
  mockUseHasPermission: vi.fn((_permission: string) => true),
  mockNotifyMinersChanged: vi.fn(),
  mockFleetOutletContext: {
    sites: [],
    sitesError: null,
    sitesLoaded: true,
    siteCatalogAccessGranted: true,
    refetchSites: vi.fn(),
    notifyPairingCompleted: vi.fn(),
    notifyMinersChanged: vi.fn(),
    minersChangedAt: 0,
    publishViewFilterContext: vi.fn(),
  },
}));

// Mock all dependencies
vi.mock("@/protoFleet/api/useFleet", () => ({
  default: vi.fn(() => ({
    minerIds: [],
    totalMiners: 0,
    availableModels: [],
    availableFirmwareVersions: [],
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
    updateMinerWorkerName: vi.fn(),
    mergeMiners: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({ handleAuthErrors: vi.fn() })),
  useTemperatureUnit: vi.fn(() => "C"),
  useBatchStateVersion: vi.fn(() => 0),
  useStartBatchOperation: vi.fn(() => vi.fn()),
  useCompleteBatchOperation: vi.fn(() => vi.fn()),
  useRemoveDevicesFromBatch: vi.fn(() => vi.fn()),
  useCleanupStaleBatches: vi.fn(() => vi.fn()),
  useHasPermission: mockUseHasPermission,
  getActiveBatches: vi.fn(() => []),
  getAllBatches: vi.fn(() => []),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: vi.fn(() => ({
    listAllBuildings: mockListAllBuildings,
  })),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: vi.fn(() => ({
    listGroups: vi.fn(),
    listRacks: vi.fn(),
  })),
}));

vi.mock("@/protoFleet/api/useAuthNeededMiners", () => ({
  default: vi.fn(() => ({
    totalMiners: 0,
    refetch: mockRefetchAuthNeededMiners,
    hasInitialLoadCompleted: true,
    isLoading: false,
  })),
}));

vi.mock("@/protoFleet/api/useDeviceErrors", () => ({
  useDeviceErrors: vi.fn(() => ({ refetch: mockRefetchErrors })),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerList", () => ({
  default: mockMinerList,
}));

// Fleet now reads pairing/refetch coordination from FleetLayout's outlet
// context; stub the hook so the component can mount without a real layout.
vi.mock("@/protoFleet/features/fleetManagement/components/FleetLayout", () => ({
  useFleetOutletContext: () => mockFleetOutletContext,
}));

vi.mock("@/protoFleet/features/onboarding/components/Miners", () => ({
  default: () => <div data-testid="miners">Miners</div>,
}));

const createFleetMock = (overrides: Record<string, unknown> = {}) => ({
  minerIds: [] as string[],
  miners: {},
  totalMiners: 0,
  hasMore: false,
  hasInitialLoadCompleted: false,
  isLoading: false,
  refetch: vi.fn() as () => void,
  refreshCurrentPage: vi.fn() as () => void,
  loadMore: vi.fn() as () => void,
  availableModels: [] as string[],
  availableFirmwareVersions: [] as string[],
  currentPage: 0,
  hasPreviousPage: false,
  goToNextPage: vi.fn() as () => void,
  goToPrevPage: vi.fn() as () => void,
  updateMinerWorkerName: vi.fn() as (deviceIdentifier: string, workerName: string) => void,
  mergeMiners: vi.fn(),
  ...overrides,
});

// Helper to render Fleet with Router context
const renderFleet = () => {
  return render(
    <MemoryRouter>
      <Fleet />
    </MemoryRouter>,
  );
};

describe("Fleet - Polling", () => {
  let mockRefreshCurrentPage: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    vi.resetModules();
    vi.clearAllMocks();
    vi.useFakeTimers();
    Object.assign(mockFleetOutletContext, {
      sites: [],
      sitesError: null,
      sitesLoaded: true,
      siteCatalogAccessGranted: true,
      notifyMinersChanged: mockNotifyMinersChanged,
      minersChangedAt: 0,
    });

    mockRefreshCurrentPage = vi.fn();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("should setup polling interval after initial load completes", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner1"],
        totalMiners: 1,
        hasInitialLoadCompleted: true,
        refreshCurrentPage: mockRefreshCurrentPage as () => void,
        currentPage: 1,
      }),
    );

    renderFleet();

    // Advance time by poll interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);

    expect(mockRefreshCurrentPage).toHaveBeenCalled();
  });

  it("should not poll before initial load completes", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        refreshCurrentPage: mockRefreshCurrentPage as () => void,
        currentPage: 1,
      }),
    );

    renderFleet();

    // Advance time by poll interval
    vi.advanceTimersByTime(POLL_INTERVAL_MS);

    expect(mockRefreshCurrentPage).not.toHaveBeenCalled();
  });

  it("should poll repeatedly at the configured interval", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner1"],
        totalMiners: 1,
        hasInitialLoadCompleted: true,
        refreshCurrentPage: mockRefreshCurrentPage as () => void,
        currentPage: 1,
      }),
    );

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

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner1"],
        totalMiners: 1,
        hasInitialLoadCompleted: true,
        refreshCurrentPage: mockRefreshCurrentPage as () => void,
        currentPage: 1,
      }),
    );

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
    vi.clearAllMocks();
    mockMinerList.mockClear();
    mockUseHasPermission.mockReturnValue(true);
  });

  it("should render MinerList component", () => {
    const { getByTestId } = renderFleet();
    expect(getByTestId("miner-list")).toBeInTheDocument();
  });

  it("should call useFleet hook with correct parameters", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");
    const useFleet = useFleetModule.default;

    renderFleet();

    expect(useFleet).toHaveBeenCalledWith(
      expect.objectContaining({
        pageSize: 50,
      }),
    );
  });

  it("shows the loading state during sort refetches even when miners are already present", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner-1"],
        totalMiners: 1,
        isLoading: true,
      }),
    );

    renderFleet();

    expect(mockMinerList).toHaveBeenCalledWith(expect.objectContaining({ loading: true }), undefined);
  });

  it("passes a row refresh callback that keeps the current page", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");
    const mockRefetch = vi.fn();
    const mockRefreshCurrentPage = vi.fn();

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner-1"],
        totalMiners: 1,
        refetch: mockRefetch,
        refreshCurrentPage: mockRefreshCurrentPage,
      }),
    );

    renderFleet();

    const minerListCalls = mockMinerList.mock.calls as unknown as Array<[{ onRefreshMinersComplete: () => void }]>;
    const latestMinerListProps = minerListCalls[minerListCalls.length - 1][0];
    latestMinerListProps.onRefreshMinersComplete();

    expect(mockRefreshCurrentPage).toHaveBeenCalledTimes(1);
    expect(mockRefetchErrors).toHaveBeenCalledTimes(1);
    expect(mockRefetchAuthNeededMiners).toHaveBeenCalledTimes(1);
    expect(mockRefetch).not.toHaveBeenCalled();
  });

  it("passes a miner refresh callback that pulses FleetLayout miner changes", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");
    const mockRefetch = vi.fn();

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner-1"],
        totalMiners: 1,
        refetch: mockRefetch,
      }),
    );

    renderFleet();

    const minerListCalls = mockMinerList.mock.calls as unknown as Array<[{ onRefetchMiners: () => void }]>;
    const latestMinerListProps = minerListCalls[minerListCalls.length - 1][0];
    latestMinerListProps.onRefetchMiners();

    expect(mockNotifyMinersChanged).toHaveBeenCalledTimes(1);
    expect(mockRefetch).not.toHaveBeenCalled();
  });

  it("does not request building filter labels until org-scoped site catalog access is confirmed", async () => {
    mockUseHasPermission.mockImplementation(() => true);
    mockFleetOutletContext.siteCatalogAccessGranted = false;

    renderFleet();

    expect(mockListAllBuildings).not.toHaveBeenCalled();
  });
});
