import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MinerFirmware from "./MinerFirmware";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

function createMockMiner(overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot {
  return {
    deviceIdentifier: "test-device",
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
    ...overrides,
  } as MinerStateSnapshot;
}

describe("MinerFirmware", () => {
  it("renders the firmware version when available", () => {
    const miner = createMockMiner({ firmwareVersion: "1.2.3" });

    render(<MinerFirmware miner={miner} />);

    expect(screen.getByText("1.2.3")).toBeInTheDocument();
  });

  it("renders empty cell when firmware version is empty string", () => {
    const miner = createMockMiner({ firmwareVersion: "" });

    const { container } = render(<MinerFirmware miner={miner} />);

    expect(container.querySelector("span")?.textContent).toBe("");
  });

  it("renders date-based version format", () => {
    const miner = createMockMiner({ firmwareVersion: "2024.01.15" });

    render(<MinerFirmware miner={miner} />);

    expect(screen.getByText("2024.01.15")).toBeInTheDocument();
  });

  it("renders semantic version with pre-release tag", () => {
    const miner = createMockMiner({ firmwareVersion: "v1.0.0-beta" });

    render(<MinerFirmware miner={miner} />);

    expect(screen.getByText("v1.0.0-beta")).toBeInTheDocument();
  });
});
