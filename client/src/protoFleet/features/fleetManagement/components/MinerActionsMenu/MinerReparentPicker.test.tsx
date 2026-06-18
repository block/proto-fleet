import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import MinerReparentPicker from "./MinerReparentPicker";
import { PerDeviceBuildingConflictReason } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

// Building-reparent flow only. The two-step force-clear confirm is the
// part with branching logic: the server enumerates per-device conflicts,
// the client opens the "Move miners between buildings?" dialog ONLY when
// every conflict is clearable, and Continue re-dispatches with
// force_clear_conflicting_rack_membership=true. A mixed response (a
// non-clearable DEVICE_NOT_FOUND) must surface an error toast and never
// open the dialog or retry.

const { mockAssignDevicesToBuilding, mockPushToast } = vi.hoisted(() => ({
  mockAssignDevicesToBuilding: vi.fn(),
  mockPushToast: vi.fn(() => 1),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ assignDevicesToBuilding: mockAssignDevicesToBuilding }),
}));
vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({ assignDevicesToSite: vi.fn() }),
}));
vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ assignDevicesToRack: vi.fn(), getDeviceSet: vi.fn(), listRacks: vi.fn() }),
}));
vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {},
  buildingsClient: {},
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: mockPushToast,
  removeToast: vi.fn(),
  updateToast: vi.fn(),
  STATUSES: { success: "success", error: "error", loading: "loading", queued: "queued" },
}));

// ParentPickerModal: expose its onConfirm via a button so the test can
// "pick" a target building. Targets building id 42.
vi.mock("@/protoFleet/components/ParentPickerModal", () => ({
  default: ({ onConfirm }: { onConfirm: (ids: bigint[]) => void | Promise<void> }) => (
    <button type="button" onClick={() => void onConfirm([42n])}>
      pick-target
    </button>
  ),
}));

// Dialog: render the title and its buttons so the test can assert the
// confirm dialog appeared and click Continue.
vi.mock("@/shared/components/Dialog", () => ({
  default: ({ title, buttons }: { title: string; buttons: { text: string; onClick: () => void }[] }) => (
    <div>
      <span>{title}</span>
      {buttons.map((b) => (
        <button type="button" key={b.text} onClick={b.onClick}>
          {b.text}
        </button>
      ))}
    </div>
  ),
}));

const renderPicker = () =>
  render(
    <MinerReparentPicker
      kind="building"
      deviceIdentifiers={["d1"]}
      selectionMode="subset"
      sourceLabel="1 miner"
      successMessage={(count) => `Moved ${count} miner(s).`}
      onClose={vi.fn()}
    />,
  );

const DIALOG_TITLE = "Move miners between buildings?";

describe("MinerReparentPicker — building force-clear flow", () => {
  beforeEach(() => {
    mockAssignDevicesToBuilding.mockReset();
    mockPushToast.mockReset();
    mockPushToast.mockReturnValue(1);
  });

  it("all-clearable conflicts open the confirm dialog; Continue re-dispatches with force=true", async () => {
    // First dispatch (force=false) returns a single clearable conflict.
    mockAssignDevicesToBuilding.mockImplementationOnce(({ onError }) => {
      onError("", [
        { deviceIdentifier: "d1", reason: PerDeviceBuildingConflictReason.DEVICE_IN_RACK_AT_OTHER_BUILDING },
      ]);
    });
    // Second dispatch (force=true) succeeds.
    mockAssignDevicesToBuilding.mockImplementationOnce(({ onSuccess }) => {
      onSuccess(1n, 1n);
    });

    renderPicker();
    fireEvent.click(screen.getByText("pick-target"));

    expect(await screen.findByText(DIALOG_TITLE)).toBeInTheDocument();

    fireEvent.click(screen.getByText("Continue"));

    await waitFor(() => expect(mockAssignDevicesToBuilding).toHaveBeenCalledTimes(2));
    const secondCall = mockAssignDevicesToBuilding.mock.calls[1][0];
    expect(secondCall.forceClearConflictingRackMembership).toBe(true);
    expect(secondCall.targetBuildingId).toBe(42n);
    expect(secondCall.deviceIdentifiers).toEqual(["d1"]);
  });

  it("a non-clearable DEVICE_NOT_FOUND surfaces an error toast and does NOT open the dialog or retry", async () => {
    mockAssignDevicesToBuilding.mockImplementationOnce(({ onError }) => {
      onError("", [
        { deviceIdentifier: "d1", reason: PerDeviceBuildingConflictReason.DEVICE_IN_RACK_AT_OTHER_BUILDING },
        { deviceIdentifier: "d2", reason: PerDeviceBuildingConflictReason.DEVICE_NOT_FOUND },
      ]);
    });

    renderPicker();
    fireEvent.click(screen.getByText("pick-target"));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith(
        expect.objectContaining({ status: "error", message: expect.stringContaining("non-clearable") }),
      ),
    );
    expect(screen.queryByText(DIALOG_TITLE)).not.toBeInTheDocument();
    expect(mockAssignDevicesToBuilding).toHaveBeenCalledTimes(1);
  });
});
