import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import FleetBuildingsPage from "./FleetBuildingsPage";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import type { FleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const listBuildingsMock = vi.hoisted(() => vi.fn());
const useActiveSiteMock = vi.hoisted(() => vi.fn());
const buildingListSpy = vi.hoisted(() => vi.fn());
const buildingCardSpy = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/api/buildings")>();
  return {
    ...actual,
    useBuildings: () => ({ listBuildings: listBuildingsMock }),
  };
});

vi.mock("@/protoFleet/api/sites", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/api/sites")>();
  return {
    ...actual,
    useSites: () => ({ assignBuildingsToSite: vi.fn() }),
  };
});

vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>();
  return {
    ...actual,
    useActiveSite: useActiveSiteMock,
  };
});

// Keep the real Zustand store (so view-mode persistence is under test) and
// only force permissions on.
vi.mock("@/protoFleet/store", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/store")>();
  return {
    ...actual,
    useHasPermission: () => true,
  };
});

vi.mock("@/protoFleet/features/fleetManagement/components/BuildingList", () => ({
  default: (props: { buildings: BuildingWithCounts[] }) => {
    buildingListSpy(props);
    return <div data-testid="building-list">{props.buildings.length} rows</div>;
  },
}));

vi.mock("@/protoFleet/features/buildings/components/BuildingCard", () => ({
  default: (props: { building: BuildingWithCounts }) => {
    buildingCardSpy(props);
    return <div data-testid="building-card">{props.building.building?.name}</div>;
  },
}));

// The modal stack is irrelevant to the view toggle and drags in heavy
// dependencies, so render it inert.
vi.mock("@/protoFleet/features/buildings/components/BuildingModals", () => ({
  default: () => null,
}));

// The reparent flow transitively pulls in the QR scanner, which imports a
// wasm module Vite's test server refuses to transform. Stub it out.
vi.mock("@/protoFleet/features/fleetManagement/hooks/useQrScanner", () => ({
  canUseLiveCamera: () => false,
  useQrScanner: () => ({}),
}));

const buildings = [
  { building: { id: 1n, name: "Building 1" } },
  { building: { id: 2n, name: "Building 2" } },
] as unknown as BuildingWithCounts[];

const fleetContext = {
  sites: [{ site: { id: 8n, name: "Austin" } }],
  sitesError: null,
  sitesLoaded: true,
  siteCatalogAccessGranted: true,
  refetchSites: vi.fn(),
} as unknown as FleetOutletContext;

const renderPage = (initialEntry = "/fleet/buildings", outletContext: FleetOutletContext = fleetContext) =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/fleet" element={<Outlet context={outletContext} />}>
          <Route path="buildings" element={<FleetBuildingsPage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

describe("FleetBuildingsPage view toggle", () => {
  beforeEach(() => {
    listBuildingsMock.mockReset();
    // Resolve the list synchronously so the page leaves its loading state.
    listBuildingsMock.mockImplementation(({ onSuccess }: { onSuccess?: (rows: BuildingWithCounts[]) => void }) => {
      onSuccess?.(buildings);
      return Promise.resolve();
    });
    useActiveSiteMock.mockReturnValue({ activeSite: { kind: "all" }, setActiveSite: vi.fn() });
    buildingListSpy.mockReset();
    buildingCardSpy.mockReset();
    // Reset the persisted preference to the default before each case.
    useFleetStore.getState().ui.setBuildingsViewMode("grid");
  });

  test("defaults to the grid view, rendering a BuildingCard per building with a count line", () => {
    renderPage();

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(screen.queryByTestId("building-list")).not.toBeInTheDocument();
    expect(screen.getByTestId("fleet-buildings-count-label")).toHaveTextContent("2 buildings");
  });

  test("switching to list view renders the table and persists the preference", async () => {
    const user = userEvent.setup();
    renderPage();

    // Both the mobile and desktop toggles render the control; either works.
    await user.click(screen.getAllByRole("button", { name: "View list" })[0]!);

    expect(screen.getByTestId("building-list")).toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("list");
  });

  test("respects the ?display=list URL param over the stored grid preference", () => {
    renderPage("/fleet/buildings?display=list");

    expect(screen.getByTestId("building-list")).toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
  });
});
