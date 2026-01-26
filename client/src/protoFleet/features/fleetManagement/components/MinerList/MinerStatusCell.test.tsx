import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerStatusCell from "./MinerStatusCell";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import * as storeModule from "@/protoFleet/store";

vi.mock("@/protoFleet/store");

vi.mock("./MinerStatus", () => ({
  default: ({ onClick }: { onClick: () => void }) => (
    <button onClick={onClick} data-testid="miner-status">
      Status
    </button>
  ),
}));

vi.mock("@/protoFleet/components/StatusModal", () => ({
  ProtoFleetStatusModal: () => <div data-testid="status-modal">Status Modal</div>,
}));

vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: () => <div data-testid="pool-selection">Pool Selection</div>,
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateMiners", () => ({
  AuthenticateMiners: () => <div data-testid="authenticate-miners">Authenticate Miners</div>,
}));

describe("MinerStatusCell", () => {
  const deviceIdentifier = "test-device-id";

  beforeEach(() => {
    vi.clearAllMocks();

    // Default mocks
    vi.mocked(storeModule.useMiner).mockReturnValue({
      deviceIdentifier,
      pairingStatus: PairingStatus.PAIRED,
    } as any);
    vi.mocked(storeModule.useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
  });

  describe("Authentication Required Behavior", () => {
    it("should show authenticate miners modal when clicking status with authentication required", async () => {
      const user = userEvent.setup();

      // Mock authentication needed
      vi.mocked(storeModule.useMiner).mockReturnValue({
        deviceIdentifier,
        pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      } as any);

      render(<MinerStatusCell deviceIdentifier={deviceIdentifier} />);

      const statusButton = screen.getByTestId("miner-status");
      await user.click(statusButton);

      expect(screen.getByTestId("authenticate-miners")).toBeInTheDocument();
    });
  });

  describe("Pool Selection Behavior", () => {
    it("should show pool selection modal when clicking status with needs mining pool", async () => {
      const user = userEvent.setup();

      // Mock needs mining pool
      vi.mocked(storeModule.useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);

      render(<MinerStatusCell deviceIdentifier={deviceIdentifier} />);

      const statusButton = screen.getByTestId("miner-status");
      await user.click(statusButton);

      expect(screen.getByTestId("pool-selection")).toBeInTheDocument();
    });
  });

  describe("Status Modal Behavior", () => {
    it("should show status modal for other issues", async () => {
      const user = userEvent.setup();

      render(<MinerStatusCell deviceIdentifier={deviceIdentifier} />);

      const statusButton = screen.getByTestId("miner-status");
      await user.click(statusButton);

      expect(screen.getByTestId("status-modal")).toBeInTheDocument();
    });
  });
});
