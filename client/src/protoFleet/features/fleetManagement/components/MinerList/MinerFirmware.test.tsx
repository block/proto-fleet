import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MinerFirmware from "./MinerFirmware";
import { useMinerFirmwareVersion } from "@/protoFleet/store";

vi.mock("@/protoFleet/store", () => ({
  useMinerFirmwareVersion: vi.fn(),
}));

const mockUseMinerFirmwareVersion = vi.mocked(useMinerFirmwareVersion);

describe("MinerFirmware", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the firmware version when available", () => {
    mockUseMinerFirmwareVersion.mockReturnValue("1.2.3");

    render(<MinerFirmware deviceIdentifier="test-device-1" />);

    expect(screen.getByText("1.2.3")).toBeInTheDocument();
  });

  it("renders placeholder when firmware version is null", () => {
    mockUseMinerFirmwareVersion.mockReturnValue(null as any);

    render(<MinerFirmware deviceIdentifier="test-device-2" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders placeholder when firmware version is undefined", () => {
    mockUseMinerFirmwareVersion.mockReturnValue(undefined as any);

    render(<MinerFirmware deviceIdentifier="test-device-3" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders placeholder when firmware version is empty string", () => {
    mockUseMinerFirmwareVersion.mockReturnValue("");

    render(<MinerFirmware deviceIdentifier="test-device-4" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders date-based version format", () => {
    mockUseMinerFirmwareVersion.mockReturnValue("2024.01.15");

    render(<MinerFirmware deviceIdentifier="test-device-5" />);

    expect(screen.getByText("2024.01.15")).toBeInTheDocument();
  });

  it("renders semantic version with pre-release tag", () => {
    mockUseMinerFirmwareVersion.mockReturnValue("v1.0.0-beta");

    render(<MinerFirmware deviceIdentifier="test-device-6" />);

    expect(screen.getByText("v1.0.0-beta")).toBeInTheDocument();
  });

  it("calls useMinerFirmwareVersion with correct deviceIdentifier", () => {
    mockUseMinerFirmwareVersion.mockReturnValue("1.2.3");

    render(<MinerFirmware deviceIdentifier="specific-miner-id" />);

    expect(mockUseMinerFirmwareVersion).toHaveBeenCalledWith("specific-miner-id");
  });
});
