import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MinerStatus from "./MinerStatus";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { deviceActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";

// Mock the store hooks
vi.mock("@/protoFleet/store", () => ({
  useMiner: vi.fn(),
  useMinerDeviceStatus: vi.fn(),
  useMinerActiveBatches: vi.fn(() => []),
  useFleetStore: vi.fn((selector) => {
    if (typeof selector === "function") {
      return selector({
        fleet: {
          selectErrorsByDevice: vi.fn(() => []),
          errors: { metadata: { lastFetchedAt: Date.now() } },
        },
      });
    }
    return { fleet: { selectErrorsByDevice: vi.fn(() => []) } };
  }),
}));

vi.mock("@/shared/hooks/useNeedsAttention", () => ({
  useNeedsAttention: vi.fn(() => false),
}));

vi.mock("@/shared/hooks/useStatusSummary", () => ({
  useMinerStatus: vi.fn(() => "Hashing"),
}));

describe("MinerStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Loading state display", () => {
    it("should show loading state when device has active batch operation and hasn't reached expected status", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device is OFFLINE during reboot (hasn't reached expected status yet)
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.OFFLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      render(<MinerStatus deviceIdentifier="device-1" />);

      expect(screen.getByText("Rebooting")).toBeInTheDocument();
    });

    it("should show pool assignment loading state", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-456",
          action: "mining-pool",
          deviceIdentifiers: ["device-2"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      render(<MinerStatus deviceIdentifier="device-2" />);

      expect(screen.getByText("Adding pools")).toBeInTheDocument();
    });

    it("should show ProgressCircular spinner during batch operation", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-789",
          action: deviceActions.shutdown,
          deviceIdentifiers: ["device-3"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      const { container } = render(<MinerStatus deviceIdentifier="device-3" />);

      // Check for the ProgressCircular component (it uses svg with specific class)
      const progressCircular = container.querySelector("svg");
      expect(progressCircular).toBeInTheDocument();
    });

    it("should prioritize loading state over normal status", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);

      // Device is actively hashing
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      // But also has active batch
      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-101",
          action: deviceActions.blinkLEDs,
          deviceIdentifiers: ["device-4"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      render(<MinerStatus deviceIdentifier="device-4" />);

      // Should show loading message, not "Hashing"
      expect(screen.getByText("Blinking LEDs")).toBeInTheDocument();
      expect(screen.queryByText("Hashing")).not.toBeInTheDocument();
    });

    it("should show first batch when device has multiple active batches", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device is OFFLINE during reboot (hasn't reached expected status yet)
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.OFFLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-5"],
          startedAt: Date.now(),
          status: "in_progress",
        },
        {
          batchIdentifier: "batch-2",
          action: "mining-pool",
          deviceIdentifiers: ["device-5"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      render(<MinerStatus deviceIdentifier="device-5" />);

      // Should show the first batch's message
      expect(screen.getByText("Rebooting")).toBeInTheDocument();
    });
  });

  describe("Normal status display", () => {
    it("should show normal status when no batch operations", async () => {
      const { useMiner, useMinerDeviceStatus, useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([]);

      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      render(<MinerStatus deviceIdentifier="device-6" />);

      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Rebooting")).not.toBeInTheDocument();
    });

    it("should show needs attention status when no batches", async () => {
      const { useMiner, useMinerDeviceStatus, useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useNeedsAttention } = await import("@/shared/hooks/useNeedsAttention");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      } as any);

      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([]);

      vi.mocked(useNeedsAttention).mockReturnValue(true);

      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      render(<MinerStatus deviceIdentifier="device-7" />);

      expect(screen.getByText("Needs attention")).toBeInTheDocument();
    });
  });

  describe("Status after pool assignment", () => {
    it("should clear needs attention when pool assigned to device without errors", async () => {
      const { useMiner, useMinerDeviceStatus, useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      const { useNeedsAttention } = await import("@/shared/hooks/useNeedsAttention");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device initially needs pool
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);
      vi.mocked(useMinerActiveBatches).mockReturnValue([]);
      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const { rerender } = render(<MinerStatus deviceIdentifier="device-pool-1" />);
      expect(screen.getByText("Needs attention")).toBeInTheDocument();

      // Optimistic update: status changes to ONLINE after pool assignment
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
      vi.mocked(useNeedsAttention).mockReturnValue(false);
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      rerender(<MinerStatus deviceIdentifier="device-pool-1" />);
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Needs attention")).not.toBeInTheDocument();
    });

    it("should still show needs attention when pool assigned to device with hardware errors", async () => {
      const { useMiner, useMinerDeviceStatus, useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      const { useNeedsAttention } = await import("@/shared/hooks/useNeedsAttention");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device initially needs pool AND has hardware errors
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);
      vi.mocked(useMinerActiveBatches).mockReturnValue([]);
      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const { rerender } = render(<MinerStatus deviceIdentifier="device-pool-2" />);
      expect(screen.getByText("Needs attention")).toBeInTheDocument();

      // Optimistic update: status changes to ERROR (has hardware errors) after pool assignment
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ERROR);
      // Still needs attention due to hardware errors
      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      rerender(<MinerStatus deviceIdentifier="device-pool-2" />);
      expect(screen.getByText("Needs attention")).toBeInTheDocument();
    });
  });

  describe("Loading state clears when expected status reached", () => {
    it("should show 'Sleeping' when device reaches INACTIVE during shutdown batch", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device initially online
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-shutdown",
          action: deviceActions.shutdown,
          deviceIdentifiers: ["device-shutdown"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      const { rerender, container } = render(<MinerStatus deviceIdentifier="device-shutdown" />);

      // Should show loading state initially with spinner
      expect(screen.getByText("Sleeping")).toBeInTheDocument();
      let progressCircular = container.querySelector("svg");
      expect(progressCircular).toBeInTheDocument();

      // Device reaches INACTIVE status
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.INACTIVE);
      vi.mocked(useMinerStatus).mockReturnValue("Sleeping");

      rerender(<MinerStatus deviceIdentifier="device-shutdown" />);

      // Should now show actual "Sleeping" status (not loading)
      expect(screen.getByText("Sleeping")).toBeInTheDocument();
      // Loading spinner should not be present
      progressCircular = container.querySelector("svg");
      expect(progressCircular).not.toBeInTheDocument();
    });

    it("should show actual status when device reaches non-INACTIVE during wakeUp batch", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device initially inactive
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.INACTIVE);
      vi.mocked(useMinerStatus).mockReturnValue("Sleeping");

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-wakeup",
          action: deviceActions.wakeUp,
          deviceIdentifiers: ["device-wakeup"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      const { rerender } = render(<MinerStatus deviceIdentifier="device-wakeup" />);

      // Should show loading state initially
      expect(screen.getByText("Waking")).toBeInTheDocument();

      // Device reaches ONLINE status
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      rerender(<MinerStatus deviceIdentifier="device-wakeup" />);

      // Should now show actual "Hashing" status
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Waking up")).not.toBeInTheDocument();
    });

    it("should show loading during reboot until minimum 15 seconds elapsed", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device initially offline
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.OFFLINE);
      vi.mocked(useMinerStatus).mockReturnValue("Offline");

      const now = Date.now();
      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-reboot",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-reboot"],
          startedAt: now - 10000, // Started 10 seconds ago (< 15s minimum)
          status: "in_progress",
        },
      ]);

      const { rerender } = render(<MinerStatus deviceIdentifier="device-reboot" />);

      // Should show loading state (less than 15s elapsed)
      expect(screen.getByText("Rebooting")).toBeInTheDocument();

      // Device reaches ONLINE status after only 10s
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      rerender(<MinerStatus deviceIdentifier="device-reboot" />);

      // Should still show "Rebooting" loading state (< 15s elapsed)
      expect(screen.getByText("Rebooting")).toBeInTheDocument();
      expect(screen.queryByText("Hashing")).not.toBeInTheDocument();

      // Update batch to 16 seconds ago (> 15s minimum)
      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-reboot",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-reboot"],
          startedAt: now - 16000, // Started 16 seconds ago (> 15s minimum)
          status: "in_progress",
        },
      ]);

      rerender(<MinerStatus deviceIdentifier="device-reboot" />);

      // Now should show actual "Hashing" status (> 15s elapsed and status is ONLINE)
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Rebooting")).not.toBeInTheDocument();
    });

    it("should show actual status when device reaches non-NEEDS_MINING_POOL during pool assignment", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device initially needs pool
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.NEEDS_MINING_POOL);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-pool",
          action: "mining-pool",
          deviceIdentifiers: ["device-pool"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      const { rerender } = render(<MinerStatus deviceIdentifier="device-pool" />);

      // Should show loading state initially
      expect(screen.getByText("Adding pools")).toBeInTheDocument();

      // Device reaches ONLINE status
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      rerender(<MinerStatus deviceIdentifier="device-pool" />);

      // Should now show actual "Hashing" status
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Adding pools")).not.toBeInTheDocument();
    });

    it("should continue showing loading when device hasn't reached expected status", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device is ONLINE but shutdown batch expects INACTIVE
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-stuck",
          action: deviceActions.shutdown,
          deviceIdentifiers: ["device-stuck"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      render(<MinerStatus deviceIdentifier="device-stuck" />);

      // Should continue showing loading state
      expect(screen.getByText("Sleeping")).toBeInTheDocument();

      // Should have loading spinner
      const { container } = render(<MinerStatus deviceIdentifier="device-stuck" />);
      const progressCircular = container.querySelector("svg");
      expect(progressCircular).toBeInTheDocument();
    });
  });

  describe("Click handling", () => {
    it("should call onClick when clickable and loading state", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device is OFFLINE during reboot (hasn't reached expected status yet)
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.OFFLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-click",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-click"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      const onClick = vi.fn();
      render(<MinerStatus deviceIdentifier="device-click" onClick={onClick} />);

      const element = screen.getByText("Rebooting");
      element.click();

      expect(onClick).toHaveBeenCalledTimes(1);
    });

    it("should apply hover styles when clickable", async () => {
      const { useMinerActiveBatches } = await import("@/protoFleet/store");
      const { useMiner, useMinerDeviceStatus } = await import("@/protoFleet/store");

      vi.mocked(useMiner).mockReturnValue({
        pairingStatus: PairingStatus.PAIRED,
      } as any);

      // Device is OFFLINE during reboot (hasn't reached expected status yet)
      vi.mocked(useMinerDeviceStatus).mockReturnValue(DeviceStatus.OFFLINE);

      vi.mocked(useMinerActiveBatches).mockReturnValue([
        {
          batchIdentifier: "batch-hover",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-hover"],
          startedAt: Date.now(),
          status: "in_progress",
        },
      ]);

      const onClick = vi.fn();
      const { container } = render(<MinerStatus deviceIdentifier="device-hover" onClick={onClick} />);

      const wrapper = container.querySelector("div");
      expect(wrapper?.className).toContain("cursor-pointer");
      expect(wrapper?.className).toContain("hover:underline");
    });
  });
});
