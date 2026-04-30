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
  getActiveBatches: vi.fn(() => []),
  getAllBatches: vi.fn(() => []),
}));

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
  });

  it("should render MinerList component", () => {
    const { getByTestId } = renderFleet();
    expect(getByTestId("miner-list")).toBeInTheDocument();
  });

  it("should render CompleteSetup component when miners exist", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue(
      createFleetMock({
        minerIds: ["miner1"],
        totalMiners: 1,
        hasInitialLoadCompleted: true,
      }),
    );

    const { getByTestId } = renderFleet();
    expect(getByTestId("complete-setup")).toBeInTheDocument();
  });

  it("should not render CompleteSetup when there are no miners", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockReturnValue(createFleetMock({ hasInitialLoadCompleted: true }));

    const { queryByTestId } = renderFleet();
    expect(queryByTestId("complete-setup")).not.toBeInTheDocument();
  });

  it("should render CompleteSetup when filters yield 0 results but miners exist", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockImplementation((options: any) => {
      if (options.pageSize === 1) {
        return createFleetMock({ totalMiners: 5, hasInitialLoadCompleted: true });
      }
      return createFleetMock({ totalMiners: 0, hasInitialLoadCompleted: true });
    });

    const { getByTestId } = renderFleet();
    expect(getByTestId("complete-setup")).toBeInTheDocument();
  });

  it("should render CompleteSetup when unfiltered count fails but main fleet shows miners", async () => {
    const useFleetModule = await import("@/protoFleet/api/useFleet");

    vi.mocked(useFleetModule.default).mockImplementation((options: any) => {
      if (options.pageSize === 1) {
        // Unfiltered count fetch failed: hasInitialLoadCompleted is true (set in finally)
        // but totalMiners stayed at 0 (never updated on error)
        return createFleetMock({ totalMiners: 0, hasInitialLoadCompleted: true });
      }
      return createFleetMock({ minerIds: ["m1"], totalMiners: 1, hasInitialLoadCompleted: true });
    });

    const { getByTestId } = renderFleet();
    expect(getByTestId("complete-setup")).toBeInTheDocument();
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
});
