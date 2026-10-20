import { Link, MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import FleetBuildingsPage from "./FleetBuildingsPage";
import {
  type BuildingWithCounts,
  BuildingWithCountsSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { FleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));
vi.mock("@/protoFleet/api/clients", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/api/clients")>()),
  fleetManagementClient: { deleteMiners: vi.fn().mockResolvedValue({ deletedCount: 1 }) },
}));

const inventoryMock = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/useFleetInventory", () => ({ useFleetInventory: inventoryMock }));
const containersMock = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/useContainers", () => ({ useContainers: containersMock }));

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
  default: (props: { buildings: BuildingWithCounts[]; onSelectedIdsChange?: (ids: string[]) => void }) => {
    buildingListSpy(props);
    return (
      <div data-testid="building-list">
        {props.buildings.length} rows
        <button type="button" onClick={() => props.onSelectedIdsChange?.(["1"])}>
          select-first
        </button>
      </div>
    );
  },
}));

// The bulk action bar renders only when a selection exists; a marker is
// enough to assert its presence/absence across view switches.
vi.mock("@/protoFleet/features/fleetManagement/components/FleetGroupActionsMenu/FleetGroupListActionBar", () => ({
  default: () => <div data-testid="bulk-action-bar" />,
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
  create(BuildingWithCountsSchema, { building: { id: 1n, name: "Building 1" } }),
  create(BuildingWithCountsSchema, { building: { id: 2n, name: "Building 2" } }),
];

const inventoryResult = (containers: ReturnType<typeof create<typeof ContainerSchema>>[] = []) => ({
  items: [
    ...buildings.map((building) =>
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.BUILDING, id: building.building!.id },
        details: { case: "building", value: building },
      }),
    ),
    ...containers.map((container) =>
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.CONTAINER, id: 3n },
        deviceIdentifier: container.deviceIdentifier,
        details: { case: "container", value: container },
      }),
    ),
  ],
  total: buildings.length + containers.length,
  loading: false,
  currentPage: 0,
  hasNextPage: false,
  hasPreviousPage: false,
  refresh: vi.fn(),
  reset: vi.fn(),
  nextPage: vi.fn(),
  previousPage: vi.fn(),
});

const fleetContext = {
  sites: [{ site: { id: 8n, name: "Austin" } }],
  sitesError: null,
  sitesLoaded: true,
  siteCatalogAccessGranted: true,
  refetchSites: vi.fn(),
  activeSavedViewId: undefined,
} as unknown as FleetOutletContext;

const renderPage = (initialEntry = "/fleet/buildings", outletContext: FleetOutletContext = fleetContext) =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <nav>
        <Link to="/elsewhere">Leave Buildings</Link>
        <Link to="/fleet/buildings">Buildings tab</Link>
      </nav>
      <Routes>
        <Route path="/elsewhere" element={<div>Another page</div>} />
        <Route path="/fleet" element={<Outlet context={outletContext} />}>
          <Route path="buildings" element={<FleetBuildingsPage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

describe("FleetBuildingsPage view toggle", () => {
  beforeEach(() => {
    containersMock.mockReturnValue({ containers: [], loading: false });
    inventoryMock.mockReturnValue(inventoryResult());
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

  test("renders containers alongside buildings in grid mode", () => {
    containersMock.mockReturnValue({
      containers: [create(ContainerSchema, { deviceIdentifier: "ct1", name: "Container 1", stale: true })],
      loading: false,
    });
    inventoryMock.mockReturnValue(
      inventoryResult([create(ContainerSchema, { deviceIdentifier: "ct1", name: "Container 1", stale: true })]),
    );
    renderPage("/fleet/buildings?display=grid");
    expect(screen.getByTestId("container-card-ct1")).toBeInTheDocument();
    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(screen.getByTestId("fleet-buildings-count-label")).toHaveTextContent("3 facilities");
  });

  test("passes containers into the same list instead of rendering a separate table", () => {
    const container = create(ContainerSchema, { deviceIdentifier: "ct1", name: "Container 1" });
    containersMock.mockReturnValue({ containers: [container], loading: false });
    inventoryMock.mockReturnValue(inventoryResult([container]));
    renderPage();
    expect(buildingListSpy).toHaveBeenLastCalledWith(expect.objectContaining({ buildings, containers: [container] }));
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByText("Containers (1)")).not.toBeInTheDocument();
  });

  test.each(["list", "grid"])(
    "removes an unpaired container immediately from %s, even with stale inventory",
    async (display) => {
      const container = create(ContainerSchema, { deviceIdentifier: "ct1", name: "Container 1" });
      const result = inventoryResult([container]);
      inventoryMock.mockReturnValue(result);
      useFleetStore.getState().auth.setPermissions(["miner:read", "miner:delete"]);
      const user = userEvent.setup();
      renderPage(`/fleet/buildings?display=${display}`);
      if (display === "list") {
        const props = buildingListSpy.mock.lastCall![0];
        act(() => props.onContainerUnpaired("ct1"));
        expect(buildingListSpy).toHaveBeenLastCalledWith(
          expect.objectContaining({
            containers: [],
            inventoryItems: expect.not.arrayContaining([result.items[2]]),
            total: 2,
          }),
        );
      } else {
        await user.click(screen.getByRole("button", { name: "Actions for Container 1" }));
        await user.click(screen.getByRole("button", { name: "Unpair" }));
        await user.click(screen.getByRole("button", { name: "Unpair" }));
        expect(await screen.findByTestId("fleet-buildings-count-label")).toHaveTextContent("2 facilities");
        expect(screen.queryByTestId("container-card-ct1")).not.toBeInTheDocument();
      }
      expect(result.refresh).toHaveBeenCalled();
      // The mocked request still returns the deleted container; switching views must not resurrect it.
      await user.click(screen.getAllByRole("button", { name: display === "list" ? "View grid" : "View list" })[0]);
      expect(screen.queryByTestId("container-card-ct1")).not.toBeInTheDocument();
      if (display === "grid") expect(buildingListSpy.mock.lastCall![0].containers).toEqual([]);
    },
  );

  test("removes an unpaired container from the legacy list", () => {
    hasPermissionMock.mockImplementation((permission) => permission !== "miner:read");
    const container = create(ContainerSchema, { deviceIdentifier: "ct1", name: "Container 1" });
    containersMock.mockReturnValue({ containers: [container], loading: false });
    renderPage();
    act(() => buildingListSpy.mock.lastCall![0].onContainerUnpaired("ct1"));
    expect(buildingListSpy.mock.lastCall![0].containers).toEqual([]);
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
    expect(screen.getByTestId("fleet-buildings-count-label")).toHaveTextContent("2 facilities");
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("grid");
  });

  test("respects the ?display=grid URL param over the stored list preference", () => {
    renderPage("/fleet/buildings?display=grid");

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(screen.queryByTestId("building-list")).not.toBeInTheDocument();
  });

  test.each([
    { storedMode: "grid", linkedMode: "list", viewTestId: "building-list" },
    { storedMode: "list", linkedMode: "grid", viewTestId: "building-card" },
  ] as const)(
    "keeps the deep-linked $linkedMode view after navigating away and back",
    async ({ storedMode, linkedMode, viewTestId }) => {
      const user = userEvent.setup();
      useFleetStore.getState().ui.setBuildingsViewMode(storedMode);
      renderPage(`/fleet/buildings?display=${linkedMode}`);

      expect(screen.getAllByTestId(viewTestId)).not.toHaveLength(0);

      await user.click(screen.getByRole("link", { name: "Leave Buildings" }));
      expect(screen.getByText("Another page")).toBeInTheDocument();
      await user.click(screen.getByRole("link", { name: "Buildings tab" }));

      expect(screen.getAllByTestId(viewTestId)).not.toHaveLength(0);
      expect(useFleetStore.getState().ui.buildingsViewMode).toBe(linkedMode);
    },
  );

  test("ignores an invalid display parameter without overwriting the saved preference", async () => {
    const user = userEvent.setup();
    useFleetStore.getState().ui.setBuildingsViewMode("grid");
    renderPage("/fleet/buildings?display=invalid");

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("grid");

    await user.click(screen.getByRole("link", { name: "Leave Buildings" }));
    await user.click(screen.getByRole("link", { name: "Buildings tab" }));

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("grid");
  });

  test("keeps the saved preference when an active saved view supplies the display mode", async () => {
    const user = userEvent.setup();
    renderPage("/fleet/buildings?display=grid&view=grid-buildings", {
      ...fleetContext,
      activeSavedViewId: "grid-buildings",
    });

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("list");

    await user.click(screen.getByRole("link", { name: "Leave Buildings" }));
    await user.click(screen.getByRole("link", { name: "Buildings tab" }));

    expect(screen.getByTestId("building-list")).toBeInTheDocument();
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("list");
  });

  test("clears a list-mode selection and drops the bulk action bar when switching to grid", async () => {
    const user = userEvent.setup();
    renderPage();

    // Seed a selection in list view; the bulk action bar mounts.
    await user.click(screen.getByRole("button", { name: "select-first" }));
    expect(screen.getByTestId("bulk-action-bar")).toBeInTheDocument();

    await user.click(screen.getAllByRole("button", { name: "View grid" })[0]!);

    // Grid entry clears the stale selection, so the bar must not linger.
    expect(screen.queryByTestId("bulk-action-bar")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
  });

  test("forces list view without losing the deep-linked preference when the reader lacks stats permissions", async () => {
    // A site:read-only reader (no fleet:read / miner:read) reaches the tab,
    // but the grid's BuildingCards would 403 on GetBuildingStats, so grid is
    // unavailable even via ?display=grid.
    hasPermissionMock.mockImplementation((permission: string) => permission === "site:read");
    const user = userEvent.setup();

    renderPage("/fleet/buildings?display=grid");

    expect(screen.getByTestId("building-list")).toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "View grid" })).not.toBeInTheDocument();
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("grid");

    await user.click(screen.getByRole("link", { name: "Leave Buildings" }));
    hasPermissionMock.mockImplementation(() => true);
    await user.click(screen.getByRole("link", { name: "Buildings tab" }));

    expect(screen.getAllByTestId("building-card")).toHaveLength(2);
    expect(useFleetStore.getState().ui.buildingsViewMode).toBe("grid");
  });
});
