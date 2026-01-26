import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerName from "./MinerName";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import * as storeModule from "@/protoFleet/store";
import * as useNeedsAttentionModule from "@/shared/hooks/useNeedsAttention";

vi.mock("@/protoFleet/store");
vi.mock("@/shared/hooks/useNeedsAttention");

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu", () => ({
  default: () => <div data-testid="actions-menu">Actions Menu</div>,
}));

vi.mock("@/protoFleet/components/StatusModal", () => ({
  ProtoFleetStatusModal: () => <div data-testid="status-modal">Status Modal</div>,
}));

vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: () => <div data-testid="pool-selection">Pool Selection</div>,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerFrame", () => ({
  default: () => <div data-testid="miner-frame">Miner Frame</div>,
}));

describe("MinerName", () => {
  const mockWindowOpen = vi.fn();
  const deviceIdentifier = "test-device-id";
  const mockUrl = "http://192.168.1.100";
  const mockName = "Test Miner";

  beforeEach(() => {
    vi.clearAllMocks();
    window.open = mockWindowOpen;

    // Default mocks
    vi.mocked(storeModule.useMiner).mockReturnValue({
      deviceIdentifier,
      name: mockName,
      pairingStatus: PairingStatus.PAIRED,
    } as any);
    vi.mocked(storeModule.useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
    vi.mocked(storeModule.useMinerName).mockReturnValue(mockName);
    vi.mocked(storeModule.useMinerUrl).mockReturnValue(mockUrl);
    vi.mocked(storeModule.useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return () => ({});
      }
      return {};
    });
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);
  });

  describe("Alert Icon Visibility with Authentication Required", () => {
    it("should not show alert icon when authentication required (disabled row)", () => {
      // Mock authentication needed and needs attention
      vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
      vi.mocked(storeModule.useMiner).mockReturnValue({
        deviceIdentifier,
        name: mockName,
        pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      } as any);

      render(<MinerName deviceIdentifier={deviceIdentifier} />);

      const alertButton = screen.queryByRole("button", { name: /view issues/i });
      expect(alertButton).not.toBeInTheDocument();
    });
  });

  describe("Alert Icon Click without Authentication Required", () => {
    it("should show status modal when clicking alert icon for other issues", async () => {
      const user = userEvent.setup();

      // Mock needs attention but not authentication
      vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);

      render(<MinerName deviceIdentifier={deviceIdentifier} />);

      const alertButton = screen.getByRole("button", { name: /view issues/i });
      await user.click(alertButton);

      expect(mockWindowOpen).not.toHaveBeenCalled();
      expect(screen.getByTestId("status-modal")).toBeInTheDocument();
    });

    it("should show pool selection when clicking alert icon for mining pool needed", async () => {
      const user = userEvent.setup();

      // Mock needs mining pool
      vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
      vi.mocked(storeModule.useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);

      render(<MinerName deviceIdentifier={deviceIdentifier} />);

      const alertButton = screen.getByRole("button", { name: /view issues/i });
      await user.click(alertButton);

      expect(mockWindowOpen).not.toHaveBeenCalled();
      expect(screen.getByTestId("pool-selection")).toBeInTheDocument();
    });
  });

  describe("Alert Icon Visibility", () => {
    it("should not show alert icon when no issues", () => {
      // Mock no needs attention
      vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);

      render(<MinerName deviceIdentifier={deviceIdentifier} />);

      const alertButton = screen.queryByRole("button", { name: /view issues/i });
      expect(alertButton).not.toBeInTheDocument();
    });
  });
});
