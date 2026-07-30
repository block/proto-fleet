import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ManageRackModal from "./ManageRackModal";
import { RackCoolingType, RackOrderIndex } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const mockSaveRack = vi.fn();
const mockUpdateRack = vi.fn();
const mockGetDeviceSet = vi.fn();
// Membership commits and the placement Save both go through this. Succeeds by
// default; individual tests re-stub it to inspect or fail a call.
interface AssignCall {
  targetRackId?: bigint;
  deviceIdentifiers: string[];
  slotAssignments?: { deviceIdentifier: string; position?: { row: number; column: number } }[];
  onSuccess?: () => void;
  onError?: (message: string) => void;
}
const mockAssignDevicesToRack = vi.fn((args: AssignCall) => args.onSuccess?.());
// The rack always exists when this modal opens, so its membership comes from the
// server. Resolve both loads immediately with one already-placed miner.
const mockGetRackSlots = vi.fn(({ onSuccess }: { onSuccess: (slots: unknown[]) => void }) => onSuccess([]));
const mockListGroupMembers = vi.fn(({ onSuccess }: { onSuccess: (ids: string[]) => void }) => onSuccess(["miner-1"]));
const mockBlinkLED = vi.fn();

const miners: Record<string, MinerStateSnapshot> = {
  "miner-1": {
    deviceIdentifier: "miner-1",
    name: "Miner 1",
    ipAddress: "192.168.2.10",
    macAddress: "00:11:22:33:44:55",
    model: "Antminer S21",
  } as MinerStateSnapshot,
};

// The manage-miners pickers read the SitePicker scope via useActiveSite, which
// calls useNavigate/useLocation and therefore needs a Router. Stub the hook to
// "all sites" (empty scope) so these tests don't require a Router wrapper; keep
// the rest of the barrel (siteFilterFromActive) real.
vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>()),
  useActiveSite: () => ({ activeSite: { kind: "all" as const }, setActiveSite: vi.fn() }),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    saveRack: mockSaveRack,
    assignDevicesToRack: mockAssignDevicesToRack,
    updateRack: mockUpdateRack,
    getDeviceSet: mockGetDeviceSet,
    getRackSlots: mockGetRackSlots,
    listGroupMembers: mockListGroupMembers,
  }),
}));

vi.mock("@/protoFleet/api/useFleet", () => ({
  default: () => ({ miners }),
}));

vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({ blinkLED: mockBlinkLED }),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({
    width: 390,
    height: 844,
    isPhone: true,
    isTablet: false,
    isDesktop: false,
  }),
}));

vi.mock("@/protoFleet/components/FullScreenTwoPaneModal", () => ({
  __esModule: true,
  default: ({ open, buttons, primaryPane, secondaryPane }: any) =>
    open ? (
      <div data-testid="manage-rack-modal">
        {buttons?.map((button: any) => (
          <button
            key={button.text}
            type="button"
            disabled={Boolean(button.loading || button.disabled)}
            onClick={button.onClick}
          >
            {button.text}
          </button>
        ))}
        <div>{primaryPane}</div>
        <div>{secondaryPane}</div>
      </div>
    ) : null,
}));

const defaultProps = {
  show: true,
  rackSettings: {
    label: "Rack A",
    zone: "Zone A",
    rows: 1,
    columns: 1,
    orderIndex: RackOrderIndex.BOTTOM_LEFT,
    coolingType: RackCoolingType.AIR,
  },
  existingRackId: 7n,
  existingRacks: [],
  onDismiss: vi.fn(),
  onSave: vi.fn(),
};

const renderManageRackModal = () =>
  render(
    <MemoryRouter>
      <ManageRackModal {...defaultProps} />
    </MemoryRouter>,
  );

describe("ManageRackModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetRackSlots.mockImplementation(({ onSuccess }) => onSuccess([]));
    mockListGroupMembers.mockImplementation(({ onSuccess }) => onSuccess(["miner-1"]));
    mockAssignDevicesToRack.mockImplementation((args) => args.onSuccess?.());
  });

  it("clears a selected slot when the slot actions sheet is dismissed", () => {
    renderManageRackModal();

    fireEvent.click(screen.getByTestId("rack-slot-01"));
    fireEvent.click(screen.getByTestId("rack-slot-actions-sheet"));
    fireEvent.click(screen.getByText("Miner 1"));

    expect(screen.queryByText("Position 01")).not.toBeInTheDocument();
    expect(screen.getByTestId("rack-slot-01")).toHaveAttribute("data-slot-state", "empty");
  });

  it("preserves a selected slot after choosing Select from list", () => {
    renderManageRackModal();

    fireEvent.click(screen.getByTestId("rack-slot-01"));
    fireEvent.click(within(screen.getByTestId("rack-slot-actions-sheet-content")).getByText("Select from list"));
    fireEvent.click(screen.getByText("Miner 1"));

    expect(screen.getByText("Position 01")).toBeInTheDocument();
    expect(screen.getByTestId("rack-slot-01")).toHaveAttribute("data-slot-state", "assigned");
  });

  it("disables Save until a miner's slot actually changes", () => {
    renderManageRackModal();

    // The rack loaded with one unplaced miner, so there is no placement delta.
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    fireEvent.click(screen.getByTestId("rack-slot-01"));
    fireEvent.click(within(screen.getByTestId("rack-slot-actions-sheet-content")).getByText("Select from list"));
    fireEvent.click(screen.getByText("Miner 1"));

    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("Save sends only the miners whose slot changed, with the new position", async () => {
    renderManageRackModal();

    fireEvent.click(screen.getByTestId("rack-slot-01"));
    fireEvent.click(within(screen.getByTestId("rack-slot-actions-sheet-content")).getByText("Select from list"));
    fireEvent.click(screen.getByText("Miner 1"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockAssignDevicesToRack).toHaveBeenCalledTimes(1));
    const request = mockAssignDevicesToRack.mock.calls[0][0];
    expect(request.targetRackId).toBe(7n);
    expect(request.deviceIdentifiers).toEqual(["miner-1"]);
    expect(request.slotAssignments).toEqual([
      expect.objectContaining({
        deviceIdentifier: "miner-1",
        position: expect.objectContaining({ row: 0, column: 0 }),
      }),
    ]);
  });

  it("keeps a miner in the list when its removal fails to persist", async () => {
    mockAssignDevicesToRack.mockImplementation((args) => args.onError?.("rack is locked"));
    renderManageRackModal();

    fireEvent.click(screen.getByRole("button", { name: "Actions for Miner 1" }));
    fireEvent.click(screen.getByText("Remove miner"));

    // The unassign was attempted (no target rack) and refused, so the row stays.
    await waitFor(() => expect(mockAssignDevicesToRack).toHaveBeenCalledTimes(1));
    expect(mockAssignDevicesToRack.mock.calls[0][0].targetRackId).toBeUndefined();
    expect(mockAssignDevicesToRack.mock.calls[0][0].deviceIdentifiers).toEqual(["miner-1"]);
    expect(screen.getByText("Miner 1")).toBeInTheDocument();
  });
});
