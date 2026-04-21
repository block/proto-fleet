import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { INACTIVE_PLACEHOLDER } from "./constants";
import MinerIpAddress from "./MinerIpAddress";
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

describe("MinerIpAddress", () => {
  it("renders placeholder when IP address is not available", () => {
    const miner = createMockMiner({ ipAddress: "" });

    render(<MinerIpAddress miner={miner} />);

    expect(screen.getByText(INACTIVE_PLACEHOLDER)).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders non-clickable IP when there is no URL", () => {
    const miner = createMockMiner({ ipAddress: "192.168.1.100", url: "" });

    render(<MinerIpAddress miner={miner} />);

    expect(screen.getByText("192.168.1.100")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders a link that opens in new tab for HTTP URLs", () => {
    const httpUrl = "http://192.168.1.100";
    const miner = createMockMiner({ ipAddress: "192.168.1.100", url: httpUrl });

    render(<MinerIpAddress miner={miner} />);

    const link = screen.getByRole("link", { name: "192.168.1.100" });
    expect(link).toHaveAttribute("href", httpUrl);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("renders a link that opens in new tab for HTTPS URLs", () => {
    const httpsUrl = "https://192.168.1.100";
    const miner = createMockMiner({ ipAddress: "192.168.1.100", url: httpsUrl });

    render(<MinerIpAddress miner={miner} />);

    const link = screen.getByRole("link", { name: "192.168.1.100" });
    expect(link).toHaveAttribute("href", httpsUrl);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });
});
