import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ManageBuildingModal from "./ManageBuildingModal";
import { BuildingSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

// Reparent racks STAGE on confirm (parity with the miner-side promptReparent →
// setRackMiners): accepting the reparent warning folds the rack into the working
// set but writes nothing until the outer Save, which persists it via a
// member-only AssignRacksToBuilding. These tests drive that flow end to end.
const mockApi = vi.hoisted(() => ({
  listBuildingsBySite: vi.fn(),
  listBuildingRacks: vi.fn(),
  assignRacksToBuilding: vi.fn(),
}));
const mockListRacks = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", async () => {
  const actual = await vi.importActual<typeof import("@/protoFleet/api/buildings")>("@/protoFleet/api/buildings");
  return { ...actual, useBuildings: () => mockApi };
});
vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: mockListRacks }),
}));

const building = create(BuildingSchema, { id: 20n, name: "North", siteId: 7n, aisles: 2, racksPerAisle: 2 });

// Alpha (1n) is in this building (eligible); Beta (2n) is in another building on
// the same site (a reassignment / reparent candidate).
const createRack = (id: bigint, label: string, buildingId: bigint, siteId?: bigint, deviceCount = 0) =>
  create(DeviceSetSchema, {
    id,
    label,
    deviceCount,
    typeDetails: { case: "rackInfo", value: create(RackInfoSchema, { rows: 1, columns: 1, buildingId, siteId }) },
  });

const renderModal = (onSaved = vi.fn()) =>
  render(
    <ManageBuildingModal
      open
      building={building}
      siteName="North DC"
      onDismiss={vi.fn()}
      onEditDetails={vi.fn()}
      onDeleteRequested={vi.fn()}
      onSaved={onSaved}
    />,
  );

// Open Manage racks, surface the reparent candidate, pick it, and click Continue
// so the reparent warning dialog is showing.
const openPickerAndPickBeta = async () => {
  await userEvent.click(await screen.findByTestId("manage-building-manage-racks"));
  await screen.findByText("Alpha");
  await userEvent.click(screen.getByLabelText("Show assigned racks"));
  await screen.findByText("Beta");
  const betaCheckbox = screen.getByTestId("list-body").querySelectorAll<HTMLInputElement>("input[type='checkbox']")[1];
  await userEvent.click(betaCheckbox);
  await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));
  await screen.findByText("Move this rack?");
};

describe("ManageBuildingModal reparent commit-on-Continue", () => {
  beforeEach(() => {
    mockApi.listBuildingsBySite.mockReset();
    mockApi.listBuildingRacks.mockReset();
    mockApi.assignRacksToBuilding.mockReset();
    mockListRacks.mockReset();
    // Building opens with no racks placed yet.
    mockApi.listBuildingRacks.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.listBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.assignRacksToBuilding.mockImplementation(({ onSuccess }) => onSuccess?.(0n));
    mockListRacks.mockImplementation(({ onSuccess }) =>
      onSuccess?.([createRack(1n, "Alpha", 20n, 7n), createRack(2n, "Beta", 9n, 7n, 5)]),
    );
  });

  it("stages the reparent on Move without any RPC until Save", async () => {
    renderModal();
    await openPickerAndPickBeta();

    // Neither picking nor confirming the warning writes anything.
    expect(mockApi.assignRacksToBuilding).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(mockApi.assignRacksToBuilding).not.toHaveBeenCalled();

    // The staged rack persists on the outer Save via a member-only assign into
    // this building (targetBuildingId → the rack moves out of its old building).
    await userEvent.click(screen.getByTestId("manage-building-save"));
    await waitFor(() => expect(mockApi.assignRacksToBuilding).toHaveBeenCalled());
    const movedThisBuilding = mockApi.assignRacksToBuilding.mock.calls
      .map((c) => c[0])
      .find((arg) => arg.targetBuildingId === 20n && arg.racks.some((r: { rackId: bigint }) => r.rackId === 2n));
    expect(movedThisBuilding).toBeTruthy();
    expect(movedThisBuilding.racks).toContainEqual({ rackId: 2n }); // member-only; no cell chosen
  });

  it("leaves the working set untouched and writes nothing when the warning is cancelled", async () => {
    renderModal();
    await openPickerAndPickBeta();

    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockApi.assignRacksToBuilding).not.toHaveBeenCalled();
    // Picker stays open; the reparent dialog is gone.
    expect(screen.queryByText("Move this rack?")).not.toBeInTheDocument();
    expect(screen.getByTestId("manage-racks-modal-confirm")).toBeInTheDocument();
  });

  it("does not refresh the host on dismiss after staging a reparent (nothing committed until Save)", async () => {
    // Staging writes nothing server-side, so a plain dismiss must not fire the
    // host refresh — there is no server change to reconcile.
    const onSaved = vi.fn();
    renderModal(onSaved);
    await openPickerAndPickBeta();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));

    await userEvent.click(screen.getByLabelText("Close dialog"));
    expect(mockApi.assignRacksToBuilding).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("does not refresh the host on dismiss when nothing was touched", async () => {
    const onSaved = vi.fn();
    renderModal(onSaved);
    await screen.findByTestId("manage-building-manage-racks");

    await userEvent.click(screen.getByLabelText("Close dialog"));
    expect(onSaved).not.toHaveBeenCalled();
  });
});
