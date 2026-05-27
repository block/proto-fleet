import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingModals from "./BuildingModals";
import { emptyBuildingFormValues } from "@/protoFleet/api/buildings";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type BuildingModalsApi } from "@/protoFleet/features/buildings/hooks/useBuildingModals";

// The ManageBuildingModal fetches building racks on mount — mock the API
// surface so the host test doesn't hit a network path.
vi.mock("@/protoFleet/api/buildings", async () => {
  const actual = await vi.importActual<typeof import("@/protoFleet/api/buildings")>("@/protoFleet/api/buildings");
  return {
    ...actual,
    useBuildings: () => ({
      listBuildingsBySite: vi.fn(),
      listAllBuildings: vi.fn(),
      getBuilding: vi.fn(),
      listBuildingRacks: vi.fn(),
      createBuilding: vi.fn(),
      updateBuilding: vi.fn(),
      deleteBuilding: vi.fn(),
      assignRackToBuilding: vi.fn(),
    }),
  };
});

const makeRow = (id: bigint, name: string, rackCount: bigint = 0n) =>
  create(BuildingWithCountsSchema, { building: create(BuildingSchema, { id, name, siteId: 7n }), rackCount });

const makeApi = (overrides: Partial<BuildingModalsApi> = {}): BuildingModalsApi => ({
  state: { kind: "none" },
  deleteTarget: null,
  saving: false,
  deleting: false,
  openDetailsCreate: vi.fn(),
  openDetailsEdit: vi.fn(),
  openManage: vi.fn(),
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
    render(<BuildingModals modals={modals} />);
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

    render(<BuildingModals modals={modals} />);
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

    render(<BuildingModals modals={modals} />);

    expect(screen.getByTestId("building-delete-dialog")).toBeInTheDocument();
    expect(screen.getByText(/Delete building "Main"\?/)).toBeInTheDocument();
  });

  it("Cancelling the cascade dialog dismisses only the dialog, leaving underlying state", () => {
    const row = makeRow(11n, "Main");
    const modals = makeApi({
      state: { kind: "manage", row, siteName: "North DC" },
      deleteTarget: row,
    });

    render(<BuildingModals modals={modals} />);
    fireEvent.click(screen.getByTestId("building-delete-dialog-cancel"));

    expect(modals.dismissDeleteConfirm).toHaveBeenCalled();
    expect(modals.dismiss).not.toHaveBeenCalled();
  });
});
