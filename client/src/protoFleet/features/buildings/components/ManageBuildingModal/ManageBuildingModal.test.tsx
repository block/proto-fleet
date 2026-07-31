import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ManageBuildingModal from "./ManageBuildingModal";
import { BuildingSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

// The rack picker owns building membership, so accepting the reparent warning
// commits the move immediately via a member-only AssignRacksToBuilding.
// Placement is the only thing that stays staged for the outer Save. These tests
// drive that flow end to end.
const mockApi = vi.hoisted(() => ({
  listBuildingsBySite: vi.fn(),
  listBuildings: vi.fn(),
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

describe("ManageBuildingModal reparent commit-on-confirm", () => {
  beforeEach(() => {
    mockApi.listBuildingsBySite.mockReset();
    mockApi.listBuildings.mockReset();
    mockApi.listBuildingRacks.mockReset();
    mockApi.assignRacksToBuilding.mockReset();
    mockListRacks.mockReset();
    // Building opens with no racks placed yet.
    mockApi.listBuildingRacks.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.listBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.listBuildings.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.assignRacksToBuilding.mockImplementation(({ onSuccess }) => onSuccess?.(0n));
    mockListRacks.mockImplementation(({ onSuccess }) =>
      onSuccess?.([createRack(1n, "Alpha", 20n, 7n), createRack(2n, "Beta", 9n, 7n, 5)]),
    );
  });

  it("commits the reparent on Move via a member-only assign", async () => {
    renderModal();
    await openPickerAndPickBeta();

    // Picking alone writes nothing — the warning has to be accepted first.
    expect(mockApi.assignRacksToBuilding).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));

    // Accepting commits membership: a member-only assign into this building
    // (targetBuildingId → the rack moves out of its old building). No cell is
    // chosen yet; placement is the operator's next step, on the outer Save.
    await waitFor(() => expect(mockApi.assignRacksToBuilding).toHaveBeenCalled());
    const movedThisBuilding = mockApi.assignRacksToBuilding.mock.calls
      .map((c) => c[0])
      .find((arg) => arg.targetBuildingId === 20n && arg.racks.some((r: { rackId: bigint }) => r.rackId === 2n));
    expect(movedThisBuilding).toBeTruthy();
    expect(movedThisBuilding.racks).toContainEqual({ rackId: 2n });

    // Membership is committed, so the placement Save has nothing left to write.
    await waitFor(() => expect(screen.getByTestId("manage-building-save")).toBeDisabled());
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

  it("refreshes the host as soon as the reparent commits, not on dismiss", async () => {
    // The membership write changes the host's rack counts, so onSaved fires
    // with the commit rather than waiting for a Save that may never come.
    const onSaved = vi.fn();
    renderModal(onSaved);
    await openPickerAndPickBeta();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    onSaved.mockClear();
    await userEvent.click(screen.getByLabelText("Close dialog"));
    // Nothing left staged, so the dismiss itself has nothing to reconcile.
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

describe("ManageBuildingModal Save dirty gate", () => {
  beforeEach(() => {
    mockApi.listBuildingsBySite.mockReset();
    mockApi.listBuildings.mockReset();
    mockApi.listBuildingRacks.mockReset();
    mockApi.assignRacksToBuilding.mockReset();
    mockListRacks.mockReset();
    // Alpha loads already placed at aisle 0, position 0 — so the working set
    // starts clean against the snapshot.
    mockApi.listBuildingRacks.mockImplementation(({ onSuccess }) =>
      onSuccess?.([{ rackId: 1n, rackLabel: "Alpha", aisleIndex: 0, positionInAisle: 0 }]),
    );
    mockApi.listBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.listBuildings.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockApi.assignRacksToBuilding.mockImplementation(({ onSuccess }) => onSuccess?.(0n));
    mockListRacks.mockImplementation(({ onSuccess }) => onSuccess?.([createRack(1n, "Alpha", 20n, 7n)]));
  });

  it("row-level Remove rack unassigns immediately", async () => {
    renderModal();
    await screen.findByTestId("manage-building-assigned-rack-1");

    await userEvent.click(screen.getByTestId("manage-building-remove-rack-1"));

    // Unassign = a member-only assign with no target building.
    await waitFor(() => expect(mockApi.assignRacksToBuilding).toHaveBeenCalled());
    const call = mockApi.assignRacksToBuilding.mock.calls[0][0];
    expect(call.targetBuildingId).toBeUndefined();
    expect(call.racks).toEqual([{ rackId: 1n }]);
    // The row drops once the write lands, and Save has nothing left to do.
    await waitFor(() => expect(screen.queryByTestId("manage-building-assigned-rack-1")).not.toBeInTheDocument());
    expect(screen.getByTestId("manage-building-save")).toBeDisabled();
  });

  it("keeps the row when the unassign fails", async () => {
    mockApi.assignRacksToBuilding.mockImplementation(({ onError }) => onError?.("network down"));
    renderModal();
    await screen.findByTestId("manage-building-assigned-rack-1");

    await userEvent.click(screen.getByTestId("manage-building-remove-rack-1"));

    await waitFor(() => expect(screen.getByText(/Failed to update racks/)).toBeInTheDocument());
    expect(screen.getByTestId("manage-building-assigned-rack-1")).toBeInTheDocument();
  });

  it("disables Save until a placement actually changes", async () => {
    renderModal();
    // Loaded and clean — nothing to commit, so Save must not offer a no-op
    // write (which previously toasted a save that dispatched no RPCs).
    await waitFor(() => expect(screen.getByTestId("manage-building-save")).toBeDisabled());

    // Move Alpha to a different cell: select the row, then click a free cell.
    await userEvent.click(screen.getByTestId("manage-building-assigned-rack-1"));
    await userEvent.click(screen.getByTestId("manage-building-grid-cell-1-1"));

    await waitFor(() => expect(screen.getByTestId("manage-building-save")).not.toBeDisabled());
  });
});
