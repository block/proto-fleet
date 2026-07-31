import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ManageRackModal from "./ManageRackModal";
import { RackCoolingType, RackOrderIndex } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

/**
 * Membership-commit behaviour, driven through stubbed pickers.
 *
 * The real ManageMiners / Scan pickers need a paginated miner list and a camera,
 * and what these tests are about is what ManageRackModal does with their
 * callbacks: which AssignDevicesToRack calls it makes, in what order, and what it
 * leaves on screen when one of them fails. Stubbing the pickers is what makes
 * those outcomes assertable — hence a separate file from ManageRackModal.test.tsx,
 * whose mocks are the real ones.
 */

interface AssignCall {
  targetRackId?: bigint;
  deviceIdentifiers: string[];
  onSuccess?: () => void;
  onError?: (message: string) => void;
}
const mockAssignDevicesToRack = vi.fn((args: AssignCall) => args.onSuccess?.());
const mockGetRackSlots = vi.fn(({ onSuccess }: { onSuccess: (slots: unknown[]) => void }) => onSuccess([]));
const mockListGroupMembers = vi.fn(({ onSuccess }: { onSuccess: (ids: string[]) => void }) => onSuccess(["miner-1"]));

const miners: Record<string, MinerStateSnapshot> = {
  "miner-1": { deviceIdentifier: "miner-1", name: "Miner 1" } as MinerStateSnapshot,
  "miner-2": { deviceIdentifier: "miner-2", name: "Miner 2" } as MinerStateSnapshot,
};

// Exposes onConfirm as a button and renders whatever string comes back, which is
// the channel a still-open picker uses to show a write failure.
vi.mock("./ManageMinersModal", async () => {
  const { useState } = await import("react");
  const ManageMinersStub = ({ onConfirm }: { onConfirm: (...args: never[]) => Promise<string | undefined> }) => {
    const [error, setError] = useState<string>();
    return (
      <div>
        <button
          type="button"
          onClick={() =>
            void (onConfirm as (...a: unknown[]) => Promise<string | undefined>)(
              ["miner-2"],
              false,
              undefined,
              [],
            ).then(setError)
          }
        >
          swap to miner-2
        </button>
        {error ? <p>picker error: {error}</p> : null}
      </div>
    );
  };
  return { __esModule: true, default: ManageMinersStub };
});

// Same idea for the scanner: assign, then undo, both as buttons.
vi.mock("./ScanMinerQrModal", async () => {
  const { useState } = await import("react");
  const ScanMinerQrStub = ({
    onAssign,
    onUndoAssignment,
  }: {
    onAssign: (id: string) => Promise<{ failed?: true; message?: string }>;
    onUndoAssignment: () => Promise<void>;
  }) => {
    const [refusal, setRefusal] = useState<string>();
    return (
      <div>
        <button
          type="button"
          onClick={() =>
            void onAssign("miner-2").then((outcome) => {
              if ("failed" in outcome) setRefusal(outcome.message ?? "cancelled");
            })
          }
        >
          scan miner-2
        </button>
        <button type="button" onClick={() => void onUndoAssignment()}>
          undo scan
        </button>
        {refusal ? <p>scan refused: {refusal}</p> : null}
      </div>
    );
  };
  return { __esModule: true, default: ScanMinerQrStub };
});

vi.mock("@/protoFleet/components/PageHeader/SitePicker", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/components/PageHeader/SitePicker")>()),
  useActiveSite: () => ({ activeSite: { kind: "all" as const }, setActiveSite: vi.fn() }),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    saveRack: vi.fn(),
    assignDevicesToRack: mockAssignDevicesToRack,
    updateRack: vi.fn(),
    getDeviceSet: vi.fn(),
    getRackSlots: mockGetRackSlots,
    listGroupMembers: mockListGroupMembers,
  }),
}));

vi.mock("@/protoFleet/api/useFleet", () => ({ default: () => ({ miners }) }));
vi.mock("@/protoFleet/api/useMinerCommand", () => ({ useMinerCommand: () => ({ blinkLED: vi.fn() }) }));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({ width: 390, height: 844, isPhone: true, isTablet: false, isDesktop: false }),
}));

vi.mock("@/protoFleet/components/FullScreenTwoPaneModal", () => ({
  __esModule: true,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- test stub mirrors the real prop bag loosely
  default: ({ open, buttons, primaryPane, secondaryPane }: any) =>
    open ? (
      <div data-testid="manage-rack-modal">
        {/* eslint-disable-next-line @typescript-eslint/no-explicit-any -- as above */}
        {buttons?.map((button: any) => (
          <button key={button.text} type="button" onClick={button.onClick}>
            {button.text}
          </button>
        ))}
        <div>{primaryPane}</div>
        <div>{secondaryPane}</div>
      </div>
    ) : null,
}));

// Two slots, so a swap has room for both members mid-flight if the order were
// ever reversed — the assertions here are about ordering, not capacity.
const defaultProps = {
  show: true,
  rackSettings: {
    label: "Rack A",
    zone: "Zone A",
    rows: 1,
    columns: 2,
    orderIndex: RackOrderIndex.BOTTOM_LEFT,
    coolingType: RackCoolingType.AIR,
  },
  existingRackId: 7n,
  existingRacks: [],
  onDismiss: vi.fn(),
  onSave: vi.fn(),
};

const renderModal = () =>
  render(
    <MemoryRouter>
      <ManageRackModal {...defaultProps} />
    </MemoryRouter>,
  );

const openScanner = () => {
  fireEvent.click(screen.getByTestId("rack-slot-01"));
  fireEvent.click(within(screen.getByTestId("rack-slot-actions-sheet-content")).getByText("Scan to assign"));
};

describe("ManageRackModal — membership commits", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetRackSlots.mockImplementation(({ onSuccess }) => onSuccess([]));
    mockListGroupMembers.mockImplementation(({ onSuccess }) => onSuccess(["miner-1"]));
    mockAssignDevicesToRack.mockImplementation((args) => args.onSuccess?.());
  });

  it("undoing a scan takes the miner back out of the rack", async () => {
    renderModal();
    openScanner();

    fireEvent.click(screen.getByText("scan miner-2"));
    await waitFor(() => expect(mockAssignDevicesToRack).toHaveBeenCalledTimes(1));
    expect(mockAssignDevicesToRack.mock.calls[0][0]).toMatchObject({
      targetRackId: 7n,
      deviceIdentifiers: ["miner-2"],
    });

    fireEvent.click(screen.getByText("undo scan"));

    // The undo closure predates its own add, so a membership diff taken from that
    // render would come out empty and write nothing at all.
    await waitFor(() => expect(mockAssignDevicesToRack).toHaveBeenCalledTimes(2));
    expect(mockAssignDevicesToRack.mock.calls[1][0]).toMatchObject({
      targetRackId: undefined,
      deviceIdentifiers: ["miner-2"],
    });
  });

  it("shows a failed scan assignment in the scanner rather than behind it", async () => {
    mockAssignDevicesToRack.mockImplementation((args) => args.onError?.("rack is locked"));
    renderModal();
    openScanner();

    fireEvent.click(screen.getByText("scan miner-2"));
    await waitFor(() => expect(screen.getByText(/scan refused: rack is locked/)).toBeInTheDocument());
  });

  it("keeps a landed removal when the paired addition fails", async () => {
    // Removals go first, so call 1 is the unassign and call 2 is the add.
    mockAssignDevicesToRack.mockImplementation((args) =>
      args.targetRackId === undefined ? args.onSuccess?.() : args.onError?.("rack is full"),
    );
    renderModal();
    expect(screen.getByText("Miner 1")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Manage Miners" }));
    fireEvent.click(screen.getByText("swap to miner-2"));

    await waitFor(() => expect(mockAssignDevicesToRack).toHaveBeenCalledTimes(2));
    expect(mockAssignDevicesToRack.mock.calls[0][0]).toMatchObject({
      targetRackId: undefined,
      deviceIdentifiers: ["miner-1"],
    });

    // The unassign persisted, so Miner 1 must not still read as a member — and
    // the failure has to reach the picker, which is still open over the callout.
    await waitFor(() => expect(screen.getByText(/picker error: rack is full/)).toBeInTheDocument());
    expect(screen.queryByText("Miner 1")).not.toBeInTheDocument();
  });
});
