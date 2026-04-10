import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MinerModel from "./MinerModel";
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

describe("MinerModel", () => {
  it("renders the model name when available", () => {
    const miner = createMockMiner({ model: "Proto Rig" });

    render(<MinerModel miner={miner} />);

    expect(screen.getByText("Proto Rig")).toBeInTheDocument();
  });

  it("renders placeholder when model is empty string", () => {
    const miner = createMockMiner({ model: "" });

    render(<MinerModel miner={miner} />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders Bitmain model names", () => {
    const miner = createMockMiner({ model: "Antminer S19" });

    render(<MinerModel miner={miner} />);

    expect(screen.getByText("Antminer S19")).toBeInTheDocument();
  });
});
