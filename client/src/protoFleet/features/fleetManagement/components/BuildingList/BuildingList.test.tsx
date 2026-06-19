import { Fragment, type ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  BuildingSchema,
  type BuildingWithCounts,
  BuildingWithCountsSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

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
  fleetManagementClient: { listMinerStateSnapshots: vi.fn() },
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
  selectedIds,
  onSelectedIdsChange,
  activeSite,
  initialEntry = "/fleet/buildings",
  routePath = "/fleet/buildings",
}: {
  onEditBuilding?: EditBuildingCallback;
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
  activeSite?: { kind: "site"; id: string };
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
      activeSite: { kind: "site", id: "7" },
      initialEntry: "/7/fleet/buildings",
      routePath: "/:siteScope/fleet/buildings",
    });
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View racks"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/7/fleet/racks?building=42");
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
