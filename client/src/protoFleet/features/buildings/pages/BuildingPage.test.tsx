import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingPage from "./BuildingPage";
import {
  type Building,
  type BuildingRack,
  BuildingSchema,
  type BuildingWithCounts,
  BuildingWithCountsSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const getBuildingMock = vi.hoisted(() => vi.fn());
const listAllBuildingsMock = vi.hoisted(() => vi.fn());
const listBuildingRacksMock = vi.hoisted(() => vi.fn());
const listSitesMock = vi.hoisted(() => vi.fn());
const mockUseTelemetryMetrics = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    getBuilding: getBuildingMock,
    listAllBuildings: listAllBuildingsMock,
    listBuildingRacks: listBuildingRacksMock,
  }),
}));

vi.mock("@/protoFleet/api/sites", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoFleet/api/sites")>();
  return {
    ...actual,
    useSites: () => ({
      listSites: listSitesMock,
    }),
  };
});

// BuildingPage reads the site catalog (for the breadcrumb site label) from the
// shell-level SitesProvider. Drive it directly here.
const sitesCtx = vi.hoisted(() => ({
  current: {
    sites: undefined as SiteWithCounts[] | undefined,
    sitesError: null as string | null,
    sitesLoaded: false,
    sitesSettled: false,
    sitesPermissionDenied: false,
    siteCatalogAccessGranted: false,
    refetchSites: vi.fn(),
  },
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => sitesCtx.current,
}));

vi.mock("@/protoFleet/api/useBuildingStats", () => ({
  useBuildingStats: () => ({
    stats: {
      deviceIdentifiers: [],
      rackCount: 2,
      deviceCount: 0,
      reportingCount: 0,
      hashrateReportingCount: 0,
      efficiencyReportingCount: 0,
      powerReportingCount: 0,
      totalHashrateThs: 0,
      avgEfficiencyJth: 0,
      totalPowerKw: 0,
      hashingCount: 0,
      brokenCount: 0,
      offlineCount: 0,
      sleepingCount: 0,
      rackHealth: [
        {
          rackId: 1n,
          rackLabel: "R01",
          aisleIndex: 0,
          positionInAisle: 0,
          hashingCount: 8,
          brokenCount: 0,
          offlineCount: 0,
          sleepingCount: 0,
        },
        {
          rackId: 2n,
          rackLabel: "R02",
          aisleIndex: 0,
          positionInAisle: 1,
          hashingCount: 6,
          brokenCount: 1,
          offlineCount: 0,
          sleepingCount: 0,
        },
      ],
    },
    error: null,
    hasLoaded: true,
    refetch: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/api/useComponentErrors", () => ({
  useComponentErrors: () => ({
    controlBoardErrors: 0,
    fanErrors: 0,
    hashboardErrors: 0,
    psuErrors: 0,
  }),
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: (options: unknown) => mockUseTelemetryMetrics(options),
}));

vi.mock("@/protoFleet/features/buildings/components/BuildingModals", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/buildings/hooks/useBuildingModals", () => ({
  useBuildingModals: () => ({
    openManage: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: ({ className, gapClassName }: { className?: string; gapClassName?: string }) => (
    <div className={className} data-gap-class={gapClassName} data-testid="building-page-performance-grid">
      Performance section
    </div>
  ),
}));

vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({
    refs: {
      vertical: {
        start: { current: null },
        end: { current: null },
      },
    },
  }),
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

const installLocalStorageMock = () => {
  const storage = new Map<string, string>();
  const localStorageMock: Storage = {
    get length() {
      return storage.size;
    },
    clear: () => storage.clear(),
    getItem: (key) => storage.get(key) ?? null,
    key: (index) => Array.from(storage.keys())[index] ?? null,
    removeItem: (key) => {
      storage.delete(key);
    },
    setItem: (key, value) => {
      storage.set(key, value);
    },
  };

  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: localStorageMock,
  });
};

if (typeof globalThis.localStorage === "undefined") {
  installLocalStorageMock();
}

const renderPage = (initialEntry = "/buildings/123") =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/buildings/:id" element={<BuildingPage />} />
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );

describe("BuildingPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    mockUseTelemetryMetrics.mockReturnValue({ data: { metrics: [] } });
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
    getBuildingMock.mockImplementation(({ onSuccess }: { onSuccess: (building: Building | undefined) => void }) =>
      onSuccess(
        create(BuildingSchema, {
          id: 123n,
          name: "Building A",
          siteId: 8n,
          aisles: 1,
          racksPerAisle: 2,
        }),
      ),
    );
    listAllBuildingsMock.mockImplementation(({ onSuccess }: { onSuccess: (buildings: BuildingWithCounts[]) => void }) =>
      onSuccess([
        create(BuildingWithCountsSchema, {
          building: create(BuildingSchema, {
            id: 123n,
            name: "Building A",
            siteId: 8n,
          }),
        }),
      ]),
    );
    listBuildingRacksMock.mockImplementation(({ onSuccess }: { onSuccess: (racks: BuildingRack[]) => void }) =>
      onSuccess([]),
    );
    sitesCtx.current = {
      sites: [
        create(SiteWithCountsSchema, {
          site: create(SiteSchema, {
            id: 8n,
            name: "Austin",
          }),
        }),
      ],
      sitesError: null,
      sitesLoaded: true,
      sitesSettled: true,
      sitesPermissionDenied: false,
      siteCatalogAccessGranted: true,
      refetchSites: vi.fn(),
    };
  });

  it("preserves the selected site when leaving building detail for miners", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderPage();

    await waitFor(() => expect(screen.getByTestId("building-page-view-miners")).toBeVisible());
    fireEvent.click(screen.getByTestId("building-page-view-miners"));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/austin/fleet/miners?building=123");
  });

  it("preserves the selected site when leaving building detail for racks", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderPage();

    await waitFor(() => expect(screen.getByTestId("building-page-view-racks")).toBeVisible());
    fireEvent.click(screen.getByTestId("building-page-view-racks"));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/austin/fleet/racks?building=123");
  });

  it("keeps edit building visible on mobile and moves peer actions into an action sheet", async () => {
    renderPage();

    await waitFor(() => expect(screen.getByTestId("building-page-edit-mobile")).toBeVisible());

    fireEvent.click(screen.getByTestId("building-page-more-actions"));

    expect(screen.getByTestId("building-page-action-sheet")).toBeInTheDocument();
    expect(screen.getByTestId("building-page-view-racks-overflow-item")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("building-page-view-miners-overflow-item"));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/fleet/miners?building=123");
  });

  it("renders a Racks module header with a scoped View racks CTA", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderPage();

    expect(await screen.findByTestId("building-page-racks-title")).toHaveTextContent("Racks");

    fireEvent.click(screen.getByTestId("building-page-racks-section-view"));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/austin/fleet/racks?building=123");
  });

  it("uses the detail view spacing rhythm for sections and section content", async () => {
    renderPage();

    expect(await screen.findByTestId("building-page-metrics-section")).toHaveClass("pt-10");
    expect(screen.getByTestId("building-metric-hashrate-value")).toHaveClass("text-emphasis-400");
    expect(screen.getByTestId("building-metric-power-value")).toHaveClass("text-emphasis-400");
    expect(screen.getByTestId("building-page-racks-section")).toHaveClass("px-4", "pt-10", "laptop:px-8");
    expect(screen.getByTestId("building-page-racks-section").firstElementChild).toHaveClass("gap-3");
    expect(screen.getByTestId("building-page-racks-section-header")).toHaveClass("px-2");
    expect(screen.getByTestId("building-page-racks-card-stack")).toHaveClass("gap-1", "overflow-visible", "p-2");
    expect(screen.getByTestId("building-page-performance-section").querySelector(".sticky")).toHaveClass(
      "pt-10",
      "pb-1",
    );
    expect(screen.getByTestId("building-page-performance-grid")).toHaveClass("p-2");
    expect(screen.getByTestId("building-page-performance-grid")).toHaveAttribute("data-gap-class", "gap-1");
  });

  it("renders the building rack grid from building stats", async () => {
    renderPage();

    await waitFor(() => expect(screen.getByTestId("building-rack-grid")).toBeVisible());

    expect(screen.getByTestId("building-rack-grid-tile-R01")).toBeVisible();
    expect(screen.getByTestId("building-rack-grid-tile-R02")).toBeVisible();
  });

  it("uses all-sites fleet routes when all-sites is selected", async () => {
    renderPage();

    await waitFor(() => expect(screen.getByTestId("building-page-view-miners")).toBeVisible());
    fireEvent.click(screen.getByTestId("building-page-view-miners"));

    expect(screen.getByTestId("location-probe")).toHaveTextContent("/fleet/miners?building=123");
  });

  it("does not request uptime telemetry for building performance charts", async () => {
    renderPage();

    await waitFor(() =>
      expect(mockUseTelemetryMetrics).toHaveBeenCalledWith(
        expect.objectContaining({
          measurementTypes: expect.not.arrayContaining([MeasurementType.UPTIME]),
        }),
      ),
    );
  });
});
