import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingModals from "./BuildingModals";
import { emptyBuildingFormValues } from "@/protoFleet/api/buildings";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type BuildingModalsApi } from "@/protoFleet/features/buildings/hooks/useBuildingModals";

// The ManageBuildingModal fetches building racks on mount — mock the API
// surface so the host test doesn't hit a network path. Module-level
// mock fns keep function identity stable across renders so effects
// that depend on them don't loop when synchronous setState fires
// inside the effect body.
const mockApi = {
  listBuildingsBySite: vi.fn(),
  listAllBuildings: vi.fn(),
  getBuilding: vi.fn(),
  listBuildingRacks: vi.fn(),
  createBuilding: vi.fn(),
  updateBuilding: vi.fn(),
  deleteBuilding: vi.fn(),
  assignRacksToBuilding: vi.fn(),
  // The racks picker's Building facet options.
  listBuildings: vi.fn(({ onSuccess }: { onSuccess?: (rows: unknown[]) => void }) => onSuccess?.([])),
};
vi.mock("@/protoFleet/api/buildings", async () => {
  const actual = await vi.importActual<typeof import("@/protoFleet/api/buildings")>("@/protoFleet/api/buildings");
  return {
    ...actual,
    useBuildings: () => mockApi,
  };
});

// The standalone racks picker fetches its own rack list and site catalog. Same
// stable-identity rule as mockApi above: the picker's facet effect lists
// listSites / listBuildings as deps, so a fresh vi.fn() per render would re-run
// it forever.
const mockDeviceSets = {
  listRacks: vi.fn(),
  saveRack: vi.fn(),
  createRacks: vi.fn(),
};
vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => mockDeviceSets,
}));
const mockSitesApi = {
  listSites: vi.fn(({ onSuccess }: { onSuccess?: (rows: unknown[]) => void }) => onSuccess?.([])),
};
vi.mock("@/protoFleet/api/sites", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/api/sites")>()),
  useSites: () => mockSitesApi,
}));

const seedRacks = (racks: { id: bigint; label: string }[]) => {
  mockDeviceSets.listRacks.mockImplementation(({ onSuccess, onFinally }) => {
    onSuccess?.(
      racks.map((r) =>
        create(DeviceSetSchema, {
          id: r.id,
          label: r.label,
          // No building, this building's site: eligible to assign.
          typeDetails: { case: "rackInfo", value: create(RackInfoSchema, { rows: 1, columns: 1, siteId: 7n }) },
        }),
      ),
      "",
      racks.length,
    );
    onFinally?.();
  });
};

const makeRow = (id: bigint, name: string, rackCount: bigint = 0n) =>
  create(BuildingWithCountsSchema, { building: create(BuildingSchema, { id, name, siteId: 7n }), rackCount });

const makeSites = () => [
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 7n, name: "North DC" }) }),
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 9n, name: "South DC" }) }),
];

const makeApi = (overrides: Partial<BuildingModalsApi> = {}): BuildingModalsApi => ({
  state: { kind: "none" },
  deleteTarget: null,
  saving: false,
  deleting: false,
  manageUnassignedMinerCount: undefined,
  openDetailsCreate: vi.fn(),
  openDetailsEdit: vi.fn(),
  openManage: vi.fn(),
  openRacksPicker: vi.fn(),
  pickerAssignRacks: vi.fn().mockResolvedValue(true),
  dismiss: vi.fn(),
  dismissDeleteConfirm: vi.fn(),
  detailsCreate: vi.fn().mockResolvedValue(null),
  detailsSaveEdit: vi.fn().mockResolvedValue(null),
  manageEditDetails: vi.fn(),
  requestDeleteCurrent: vi.fn(),
  deleteConfirm: vi.fn().mockResolvedValue(undefined),
  refreshBuildings: vi.fn(),
  ...overrides,
});

describe("BuildingModals", () => {
  it("renders BuildingSettingsModal in create mode when state.kind = detailsCreate", () => {
    const modals = makeApi({
      state: { kind: "detailsCreate", siteId: 7n, siteName: "North DC", draft: emptyBuildingFormValues() },
    });
    render(<BuildingModals modals={modals} sites={makeSites()} />);
    expect(screen.getByTestId("building-settings-modal")).toBeInTheDocument();
    expect(screen.getByTestId("building-settings-modal-save")).toBeInTheDocument();
  });

  it("Delete in detailsEdit calls requestDeleteCurrent without arguments", () => {
    const row = makeRow(11n, "Main");
    const requestDeleteCurrent = vi.fn();
    const modals = makeApi({
      state: { kind: "detailsEdit", row, siteName: "North DC", draft: emptyBuildingFormValues() },
      requestDeleteCurrent,
    });

    render(<BuildingModals modals={modals} sites={makeSites()} />);
    fireEvent.click(screen.getByTestId("building-settings-modal-delete"));

    expect(requestDeleteCurrent).toHaveBeenCalled();
    expect(modals.deleteConfirm).not.toHaveBeenCalled();
  });

  it("renders the cascade dialog when deleteTarget is set, alongside underlying state", () => {
    const row = makeRow(11n, "Main", 1n);
    const modals = makeApi({
      // Underlying state is manage so the dialog overlays the manage modal.
      state: { kind: "manage", row, siteName: "North DC" },
      deleteTarget: row,
    });

    render(<BuildingModals modals={modals} sites={makeSites()} />);

    expect(screen.getByTestId("building-delete-dialog")).toBeInTheDocument();
    expect(screen.getByText(/Delete building "Main"\?/)).toBeInTheDocument();
  });

  it("racksPicker renders the picker (with its create hand-offs) and no manage surface behind it", async () => {
    seedRacks([{ id: 1n, label: "Alpha" }]);
    const modals = makeApi({ state: { kind: "racksPicker", row: makeRow(11n, "Main"), currentRackIds: [] } });

    render(<BuildingModals modals={modals} sites={makeSites()} />);

    expect(await screen.findByTestId("manage-racks-modal")).toBeInTheDocument();
    expect(screen.getByTestId("manage-racks-modal-create-new")).toBeInTheDocument();
    expect(screen.getByTestId("manage-racks-modal-create-multiple")).toBeInTheDocument();
    // The point of the standalone flow: the host already renders the building,
    // so ManageBuildingModal must not be stacked underneath.
    expect(screen.queryByTestId("manage-building-modal")).not.toBeInTheDocument();
  });

  it("writes the racksPicker delta and closes only once the write lands", async () => {
    seedRacks([{ id: 1n, label: "Alpha" }]);
    const pickerAssignRacks = vi.fn().mockResolvedValue(true);
    const modals = makeApi({
      state: { kind: "racksPicker", row: makeRow(11n, "Main"), currentRackIds: [] },
      pickerAssignRacks,
    });

    render(<BuildingModals modals={modals} sites={makeSites()} />);
    await screen.findByText("Alpha");
    // The row's own checkbox, not the header select-all.
    fireEvent.click(screen.getByTestId("list-body").querySelectorAll("input[type='checkbox']")[0]);
    fireEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    await waitFor(() => expect(pickerAssignRacks).toHaveBeenCalledWith({ added: [1n], removed: [] }));
    await waitFor(() => expect(modals.dismiss).toHaveBeenCalled());
  });

  it("Cancelling the cascade dialog dismisses only the dialog, leaving underlying state", () => {
    const row = makeRow(11n, "Main");
    const modals = makeApi({
      state: { kind: "manage", row, siteName: "North DC" },
      deleteTarget: row,
    });

    render(<BuildingModals modals={modals} sites={makeSites()} />);
    fireEvent.click(screen.getByTestId("building-delete-dialog-cancel"));

    expect(modals.dismissDeleteConfirm).toHaveBeenCalled();
    expect(modals.dismiss).not.toHaveBeenCalled();
  });
});
