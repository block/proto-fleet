import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerIssuesCell from "./MinerIssuesCell";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import * as storeModule from "@/protoFleet/store";

vi.mock("@/protoFleet/store");

vi.mock("./MinerIssues", () => ({
  default: ({ onClick }: { onClick: () => void }) => (
    <button onClick={onClick} data-testid="miner-issues">
      Issues
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

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: ({ onAuthenticated }: { onAuthenticated: (username: string, password: string) => void }) => (
    <div data-testid="authenticate-fleet-modal">
      <button onClick={() => onAuthenticated("testuser", "testpass")} data-testid="authenticate-button">
        Authenticate
      </button>
    </div>
  ),
}));

describe("MinerIssuesCell", () => {
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
    it("should show authenticate miners modal when clicking issues with authentication required", async () => {
      const user = userEvent.setup();

      // Mock authentication needed
      vi.mocked(storeModule.useMiner).mockReturnValue({
        deviceIdentifier,
        pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      } as any);

      render(<MinerIssuesCell deviceIdentifier={deviceIdentifier} />);

      const issuesButton = screen.getByTestId("miner-issues");
      await user.click(issuesButton);

      expect(screen.getByTestId("authenticate-miners")).toBeInTheDocument();
    });
  });

  describe("Pool Selection Behavior", () => {
    it("should show Fleet auth modal then pool selection when clicking issues with needs mining pool", async () => {
      const user = userEvent.setup();

      // Mock needs mining pool
      vi.mocked(storeModule.useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);

      render(<MinerIssuesCell deviceIdentifier={deviceIdentifier} />);

      const issuesButton = screen.getByTestId("miner-issues");
      await user.click(issuesButton);

      // First should show Fleet auth modal
      expect(screen.getByTestId("authenticate-fleet-modal")).toBeInTheDocument();

      // After authenticating, should show pool selection
      const authenticateButton = screen.getByTestId("authenticate-button");
      await user.click(authenticateButton);

      expect(screen.getByTestId("pool-selection")).toBeInTheDocument();
      expect(screen.queryByTestId("authenticate-fleet-modal")).not.toBeInTheDocument();
    });
  });

  describe("Status Modal Behavior", () => {
    it("should show status modal for hardware errors", async () => {
      const user = userEvent.setup();

      render(<MinerIssuesCell deviceIdentifier={deviceIdentifier} />);

      const issuesButton = screen.getByTestId("miner-issues");
      await user.click(issuesButton);

      expect(screen.getByTestId("status-modal")).toBeInTheDocument();
    });
  });
});
