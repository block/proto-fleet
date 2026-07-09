import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import ManageRackModal from "./ManageRackModal";
import { RackCoolingType, RackOrderIndex } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const mockSaveRack = vi.fn();
const mockGetRackSlots = vi.fn();
const mockListGroupMembers = vi.fn();
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

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    saveRack: mockSaveRack,
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
  default: ({ open, primaryPane, secondaryPane }: any) =>
    open ? (
      <div data-testid="manage-rack-modal">
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
  existingRacks: [],
  seededMinerIds: ["miner-1"],
  onDismiss: vi.fn(),
  onSave: vi.fn(),
};

describe("ManageRackModal", () => {
  it("clears a selected slot when the slot actions sheet is dismissed", () => {
    render(<ManageRackModal {...defaultProps} />);

    fireEvent.click(screen.getByTestId("rack-slot-01"));
    fireEvent.click(screen.getByTestId("rack-slot-actions-sheet"));
    fireEvent.click(screen.getByText("Miner 1"));

    expect(screen.queryByText("Position 01")).not.toBeInTheDocument();
    expect(screen.getByTestId("rack-slot-01")).toHaveAttribute("data-slot-state", "empty");
  });

  it("preserves a selected slot after choosing Select from list", () => {
    render(<ManageRackModal {...defaultProps} />);

    fireEvent.click(screen.getByTestId("rack-slot-01"));
    fireEvent.click(within(screen.getByTestId("rack-slot-actions-sheet-content")).getByText("Select from list"));
    fireEvent.click(screen.getByText("Miner 1"));

    expect(screen.getByText("Position 01")).toBeInTheDocument();
    expect(screen.getByTestId("rack-slot-01")).toHaveAttribute("data-slot-state", "assigned");
  });
});
