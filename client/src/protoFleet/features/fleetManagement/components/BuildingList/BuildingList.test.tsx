import { Fragment, type ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  BuildingSchema,
  type BuildingWithCounts,
  BuildingWithCountsSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import {
  type Container,
  ContainerHASchema,
  ContainerSchema,
  TankHealthSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});
afterAll(() => {
  Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
});

vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
  usePopover: () => ({
    triggerRef: { current: null },
    setPopoverRenderMode: vi.fn(),
  }),
  popoverSizes: { small: "small" },
  default: ({ children, testId }: { children: ReactNode; testId?: string }) => (
    <div data-testid={testId}>{children}</div>
  ),
}));

vi.mock("@/shared/hooks/useClickOutside", () => ({
  useClickOutside: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: { getMiningStatus: vi.fn().mockResolvedValue({ mining: true }) },
  miningGroupsClient: {
    getState: vi.fn().mockResolvedValue({ supported: false, enabled: false }),
  },
  fleetManagementClient: {
    listMinerStateSnapshots: vi.fn(),
    deleteMiners: vi.fn().mockResolvedValue({ deletedCount: 1 }),
  },
}));

vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({
    stopMining: vi.fn(),
    startMining: vi.fn(),
    reboot: vi.fn(),
    downloadLogs: vi.fn(),
    streamCommandBatchUpdates: vi.fn(() => Promise.resolve()),
    getCommandBatchLogBundle: vi.fn(),
  }),
}));

vi.mock("../BulkActions/BulkActionConfirmDialog", () => ({ default: () => null }));

// Grant the wired-action permission set so FleetGroupActionsMenu's
// permission filter doesn't strip every entry under test.
vi.mock(import("@/protoFleet/store"), async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    usePermissions: () => [
      "miner:read",
      "miner:blink_led",
      "miner:download_logs",
      "miner:firmware_update",
      "miner:reboot",
      "miner:stop_mining",
      "miner:start_mining",
      "miner:delete",
      "miner:set_power_target",
      "miner:set_cooling_mode",
      "miner:rename",
      "miner:update_worker_names",
      "miner:update_password",
      "miner:update_pools",
      "pool:read",
      "rack:read",
      "rack:manage",
      "site:read",
      "site:manage",
    ],
  };
});

// eslint-disable-next-line import-x/order -- import must come after vi.mock calls
import BuildingList from "./BuildingList";

const makeBuilding = (id: number, name: string, siteId: number) =>
  create(BuildingWithCountsSchema, {
    building: create(BuildingSchema, { id: BigInt(id), name, siteId: BigInt(siteId) }),
    rackCount: 0n,
  });

const makeSite = (id: number, name: string) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: BigInt(id), name }),
    buildingCount: 0n,
    deviceCount: 0n,
  });

const PathProbe = () => {
  const location = useLocation();
  return <span data-testid="probe-path">{location.pathname + location.search}</span>;
};

type EditBuildingCallback = (building: BuildingWithCounts) => void;

const renderList = ({
  onEditBuilding,
  onContainerUnpaired,
  containers,
  selectedIds,
  onSelectedIdsChange,
  activeSite,
  initialEntry = "/fleet/buildings",
  routePath = "/fleet/buildings",
}: {
  onEditBuilding?: EditBuildingCallback;
  onContainerUnpaired?: (deviceIdentifier: string) => void;
  containers?: Container[];
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
  activeSite?: ActiveSite;
  initialEntry?: string;
  routePath?: string;
} = {}) =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route
          path={routePath}
          element={
            <>
              <BuildingList
                buildings={[makeBuilding(42, "Alpha", 7)]}
                containers={containers}
                onContainerUnpaired={onContainerUnpaired}
                sites={[makeSite(7, "North")]}
                onEditBuilding={onEditBuilding}
                selectedIds={selectedIds}
                onSelectedIdsChange={onSelectedIdsChange}
                activeSite={activeSite}
              />
              <PathProbe />
            </>
          }
        />
        <Route path="/containers/:id" element={<PathProbe />} />
        <Route path="/buildings/:id" element={<PathProbe />} />
        <Route path="/fleet/racks" element={<PathProbe />} />
        <Route path="/fleet/miners" element={<PathProbe />} />
        <Route path="/:siteScope/fleet/racks" element={<PathProbe />} />
        <Route path="/:siteScope/fleet/miners" element={<PathProbe />} />
      </Routes>
    </MemoryRouter>,
  );

const trigger = () => screen.getByTestId("building-list-row-42-actions-trigger");

describe("BuildingList row actions menu", () => {
  it("exposes the Figma action set when the trigger is clicked", () => {
    renderList({ onEditBuilding: vi.fn() });
    fireEvent.click(trigger());
    for (const label of [
      "Manage power",
      "Update firmware",
      "Edit pool",
      "View building",
      "View racks",
      "View miners",
      "Edit building",
      "Add to group",
      "Manage security",
      "Unpair miners",
    ]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("View racks scopes the /fleet/racks redirect to the building", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View racks"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/fleet/racks?building=42");
  });

  it("View racks preserves the active site path scope", () => {
    renderList({
      activeSite: { kind: "site", id: "7", slug: "north" },
      initialEntry: "/north/fleet/buildings",
      routePath: "/:siteScope/fleet/buildings",
    });
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View racks"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/north/fleet/racks?building=42");
  });

  it("View miners scopes the /fleet/miners redirect to the building", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View miners"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/fleet/miners?building=42");
  });

  it("View building navigates to the detail page", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View building"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/buildings/42");
  });

  it("Edit building forwards the row to the host without navigating", () => {
    const onEditBuilding = vi.fn();
    renderList({ onEditBuilding });
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("Edit building"));
    expect(onEditBuilding).toHaveBeenCalledTimes(1);
    expect(onEditBuilding.mock.calls[0][0].building?.name).toBe("Alpha");
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/fleet/buildings");
  });

  it("hides Edit building when the host does not supply a handler", () => {
    renderList();
    fireEvent.click(trigger());
    expect(screen.queryByText("Edit building")).not.toBeInTheDocument();
  });

  it("shows controlled row checkboxes when selection props are supplied", () => {
    const onSelectedIdsChange = vi.fn();
    renderList({ selectedIds: [], onSelectedIdsChange });

    const checkbox = screen.getByTestId("list-body").querySelector("input[type='checkbox']") as HTMLInputElement;
    fireEvent.click(checkbox);

    expect(onSelectedIdsChange).toHaveBeenCalledWith(["42"]);
  });
});

const container = create(ContainerSchema, {
  deviceIdentifier: "42",
  name: "A container",
  site: { id: 7n, label: "North" },
  hashrateHs: 100e12,
  powerW: 1000000,
  tanks: [create(TankHealthSchema, { position: 1, hashingCount: 14, attentionCount: 2 })],
});

describe("combined building and container rows", () => {
  it("shows HA degraded before an HA report is available", () => {
    renderList({ containers: [container] });
    expect(screen.getByText("HA degraded")).toBeInTheDocument();
  });
  it.each([
    { degraded: true, stale: false, warning: true },
    { degraded: false, stale: true, warning: true },
    { degraded: false, stale: false, warning: false },
  ])("shows HA warning for $degraded / stale=$stale", ({ degraded, stale, warning }) => {
    renderList({
      containers: [create(ContainerSchema, { ...container, ha: create(ContainerHASchema, { degraded, stale }) })],
    });
    expect(screen.queryByText("HA degraded") !== null).toBe(warning);
  });
  it("opens container actions without navigating or offering miner-only commands", async () => {
    const onContainerUnpaired = vi.fn();
    renderList({ containers: [container], onContainerUnpaired });
    fireEvent.click(screen.getByRole("button", { name: "Actions for A container" }));
    expect(await screen.findByRole("button", { name: "Sleep container" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reboot miners" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Unpair" }));
    expect(screen.getByTestId("container-unpair-dialog")).toHaveTextContent("Unpair A container?");
    fireEvent.click(within(screen.getByTestId("container-unpair-dialog")).getByRole("button", { name: "Unpair" }));
    await waitFor(() => expect(onContainerUnpaired).toHaveBeenCalledExactlyOnceWith(container.deviceIdentifier));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/fleet/buildings");
  });
  it("sorts both types together and shares columns and a count", () => {
    renderList({ containers: [container] });
    const rows = screen.getAllByTestId("list-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("A container");
    expect(rows[1]).toHaveTextContent("Alpha");
    expect(screen.getByText("2 buildings")).toBeInTheDocument();
    expect(within(rows[0]).getByText("North")).toBeInTheDocument();
    expect(within(rows[0]).getByText("16")).toBeInTheDocument();
    expect(within(rows[0]).getByText("1")).toBeInTheDocument();
    expect(within(rows[0]).getByRole("img", { name: /14 healthy, 2 need attention/ })).toBeInTheDocument();
    fireEvent.click(within(rows[0]).getByText("A container"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/containers/42");
  });

  it("keeps containers out of building bulk actions even when IDs overlap", () => {
    const onSelectedIdsChange = vi.fn();
    renderList({ containers: [container], selectedIds: [], onSelectedIdsChange });
    const rows = screen.getAllByTestId("list-row");
    const containerCheckbox = rows[0].querySelector("input[type=checkbox]");
    if (containerCheckbox) expect(containerCheckbox).toBeDisabled();
    fireEvent.click(rows[1].querySelector("input[type=checkbox]")!);
    expect(onSelectedIdsChange).toHaveBeenCalledWith(["42"]);
  });

  it("does not display stale container readings as current health or metrics", () => {
    renderList({ containers: [{ ...container, stale: true }] });
    const row = screen.getAllByTestId("list-row")[0];
    expect(within(row).getByText("Readings unavailable or stale")).toBeInTheDocument();
    expect(within(row).queryByRole("img", { name: /healthy/ })).not.toBeInTheDocument();
    expect(within(row).queryByText(/TH\/S|MW/)).not.toBeInTheDocument();
  });
});

it("opens native details without a controller web view", () => {
  const open = vi.spyOn(window, "open").mockImplementation(() => null);
  renderList({ containers: [{ ...container }] });
  fireEvent.click(screen.getByText("A container"));
  expect(open).not.toHaveBeenCalled();
  expect(screen.getByTestId("probe-path")).toHaveTextContent("/containers/42");
  open.mockRestore();
});

it.each([false, true])("pages mixed facilities beyond 100 rows (selection enabled: %s)", (selectable) => {
  const onNextPage = vi.fn();
  const onPrevPage = vi.fn();
  const buildings = Array.from({ length: 99 }, (_, i) => makeBuilding(i + 1, `Building ${i + 1}`, 7));
  const lastBuilding = makeBuilding(100, "Last building", 7);
  const firstItems = [
    create(InventoryItemSchema, {
      ref: { kind: EntityKind.CONTAINER, id: 42n },
      deviceIdentifier: container.deviceIdentifier,
    }),
    ...buildings.map((row) =>
      create(InventoryItemSchema, { ref: { kind: EntityKind.BUILDING, id: row.building!.id } }),
    ),
  ];
  const page = (currentPage: number) => (
    <MemoryRouter>
      <BuildingList
        buildings={currentPage ? [lastBuilding] : buildings}
        containers={currentPage ? [] : [container]}
        inventoryItems={
          currentPage ? [create(InventoryItemSchema, { ref: { kind: EntityKind.BUILDING, id: 100n } })] : firstItems
        }
        sites={[]}
        total={101}
        currentPage={currentPage}
        hasNextPage={currentPage === 0}
        hasPreviousPage={currentPage > 0}
        onNextPage={onNextPage}
        onPrevPage={onPrevPage}
        selectedIds={selectable ? [] : undefined}
        onSelectedIdsChange={selectable ? () => undefined : undefined}
      />
    </MemoryRouter>
  );
  const { rerender } = render(page(0));
  expect(screen.getAllByTestId("list-row")).toHaveLength(100);
  expect(screen.getByText("Showing 1–100 of 101 facilities")).toBeVisible();
  expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  expect(onNextPage).toHaveBeenCalledOnce();
  rerender(page(1));
  expect(screen.getByText("Last building")).toBeVisible();
  expect(screen.getByText("Showing 101–101 of 101 facilities")).toBeVisible();
  expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
  expect(onPrevPage).toHaveBeenCalledOnce();
});
