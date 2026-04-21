import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerIssuesCell from "./MinerIssuesCell";
import type { DeviceListItem } from "./types";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

vi.mock("./MinerIssues", () => ({
  default: ({ onClick }: { onClick: () => void }) => (
    <button onClick={onClick} data-testid="miner-issues">
      Issues
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

describe("MinerIssuesCell", () => {
  it("calls onOpenStatusFlow when issues are clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();

    render(<MinerIssuesCell device={createMockDevice()} errorsLoaded onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByTestId("miner-issues"));

    expect(onOpenStatusFlow).toHaveBeenCalledTimes(1);
    expect(onOpenStatusFlow).toHaveBeenCalledWith("test-device-id");
  });
});
