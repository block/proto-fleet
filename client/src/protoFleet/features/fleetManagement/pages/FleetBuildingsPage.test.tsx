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
const hasPermissionMock = vi.hoisted(() => vi.fn((_: string) => true));

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
// route permission checks through a per-test mock.
vi.mock("@/protoFleet/store", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/store")>();
  return {
    ...actual,
    useHasPermission: (permission: string) => hasPermissionMock(permission),
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
    // Default: all permissions granted. Individual cases override.
    hasPermissionMock.mockReset();
    hasPermissionMock.mockImplementation(() => true);
    // Reset the persisted preference to the default before each case.
    useFleetStore.getState().ui.setBuildingsViewMode("list");
  });

  test("defaults to the list view, rendering the BuildingList table", () => {
    renderPage();

    expect(screen.getByTestId("building-list")).toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
  });

  test("switching to grid view renders a BuildingCard per building with a count line and persists the preference", async () => {
    const user = userEvent.setup();
    renderPage();

    // Both the mobile and desktop toggles render the control; either works.
    await user.click(screen.getAllByRole("button", { name: "View grid" })[0]!);

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(screen.queryByTestId("building-list")).not.toBeInTheDocument();
    expect(screen.getByTestId("fleet-buildings-count-label")).toHaveTextContent("2 buildings");
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("grid");
  });

  test("respects the ?display=grid URL param over the stored list preference", () => {
    renderPage("/fleet/buildings?display=grid");

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(screen.queryByTestId("building-list")).not.toBeInTheDocument();
  });

  test("forces list view and hides the toggle when the reader lacks stats permissions", () => {
    // A site:read-only reader (no fleet:read / miner:read) reaches the tab,
    // but the grid's BuildingCards would 403 on GetBuildingStats, so grid is
    // unavailable even via ?display=grid.
    hasPermissionMock.mockImplementation((permission: string) => permission === "site:read");

    renderPage("/fleet/buildings?display=grid");

    expect(screen.getByTestId("building-list")).toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "View grid" })).not.toBeInTheDocument();
  });
});
