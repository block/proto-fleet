import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MinerWorkerName from "./MinerWorkerName";
import {
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

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
    pairingStatus: PairingStatus.PAIRED,
    model: "",
    manufacturer: "",
    temperatureStatus: 0,
    firmwareVersion: "",
    driverName: "",
    workerName: "",
    ...overrides,
  } as MinerStateSnapshot;
}

describe("MinerWorkerName", () => {
  it("renders a default marker when the worker name matches the MAC address", () => {
    const miner = createMockMiner({
      macAddress: "aa:bb:cc:dd:ee:ff",
      workerName: "aa:bb:cc:dd:ee:ff",
    });

    render(<MinerWorkerName miner={miner} />);

    expect(screen.getByText("aa:bb:cc:dd:ee:ff")).toBeInTheDocument();
    expect(screen.getByLabelText("Default worker name")).toBeInTheDocument();
    expect(screen.getByLabelText("Default worker name")).toHaveAttribute("data-no-row-click");
  });

  it("renders explanatory tooltip content for the default worker name icon", () => {
    const miner = createMockMiner({
      macAddress: "aa:bb:cc:dd:ee:ff",
      workerName: "aa:bb:cc:dd:ee:ff",
    });

    render(<MinerWorkerName miner={miner} />);

    expect(screen.getByLabelText("Default worker name")).toBeInTheDocument();
    expect(screen.getByText(/Fleet uses the miner MAC address/)).toBeInTheDocument();
  });

  it("does not render a default marker for a custom worker name", () => {
    const miner = createMockMiner({
      macAddress: "aa:bb:cc:dd:ee:ff",
      workerName: "worker-01",
    });

    render(<MinerWorkerName miner={miner} />);

    expect(screen.getByText("worker-01")).toBeInTheDocument();
    expect(screen.queryByLabelText("Default worker name")).not.toBeInTheDocument();
  });

  it("renders the inactive placeholder without a default marker when the worker name is empty", () => {
    const miner = createMockMiner({
      macAddress: "aa:bb:cc:dd:ee:ff",
      workerName: "",
    });

    render(<MinerWorkerName miner={miner} />);

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByLabelText("Default worker name")).not.toBeInTheDocument();
  });

  it("renders an empty cell when authentication is required", () => {
    const miner = createMockMiner({
      macAddress: "",
      workerName: "02:00:00:4E:FC:D5",
      pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
    });

    const { container } = render(<MinerWorkerName miner={miner} />);

    expect(container.textContent).toBe("");
    expect(screen.queryByText("—")).not.toBeInTheDocument();
    expect(screen.queryByText("02:00:00:4E:FC:D5")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Default worker name")).not.toBeInTheDocument();
  });

  it("hides a non-default stored worker name when authentication is required", () => {
    const miner = createMockMiner({
      macAddress: "aa:bb:cc:dd:ee:ff",
      workerName: "worker-01",
      pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
    });

    const { container } = render(<MinerWorkerName miner={miner} />);

    expect(container.textContent).toBe("");
    expect(screen.queryByText("worker-01")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Default worker name")).not.toBeInTheDocument();
  });
});
