import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerStatusCell from "./MinerStatusCell";
import type { DeviceListItem } from "./types";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

vi.mock("./MinerStatus", () => ({
  default: ({ onClick }: { onClick: () => void }) => (
    <button onClick={onClick} data-testid="miner-status">
      Status
    </button>
  ),
}));

function createMockDevice(overrides: Partial<DeviceListItem> = {}): DeviceListItem {
  return {
    deviceIdentifier: "test-device-id",
    miner: {
      deviceIdentifier: "test-device-id",
      name: "",
      macAddress: "",
      serialNumber: "",
      powerUsage: [],
      temperature: [],
      hashrate: [],
      efficiency: [],
      ipAddress: "",
      url: "",
      deviceStatus: 0,
      pairingStatus: 0,
      model: "",
      manufacturer: "",
      temperatureStatus: 0,
      firmwareVersion: "",
      groupLabels: [],
      rackLabel: "",
      driverName: "",
      workerName: "",
    } as unknown as MinerStateSnapshot,
    errors: [],
    activeBatches: [],
    ...overrides,
  };
}

describe("MinerStatusCell", () => {
  it("calls onOpenStatusFlow when status is clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();

    render(<MinerStatusCell device={createMockDevice()} errorsLoaded onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByTestId("miner-status"));

    expect(onOpenStatusFlow).toHaveBeenCalledTimes(1);
    expect(onOpenStatusFlow).toHaveBeenCalledWith("test-device-id");
  });
});
