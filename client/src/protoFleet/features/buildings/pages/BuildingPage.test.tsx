import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingPage from "./BuildingPage";
import { type Building, type BuildingRack, BuildingSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const getBuildingMock = vi.hoisted(() => vi.fn());
const listBuildingRacksMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    getBuilding: getBuildingMock,
    listBuildingRacks: listBuildingRacksMock,
  }),
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
  useTelemetryMetrics: () => ({ data: { metrics: [] } }),
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
  DeviceSetPerformanceSection: () => <div>Performance section</div>,
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
    listBuildingRacksMock.mockImplementation(({ onSuccess }: { onSuccess: (racks: BuildingRack[]) => void }) =>
      onSuccess([]),
    );
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
});
