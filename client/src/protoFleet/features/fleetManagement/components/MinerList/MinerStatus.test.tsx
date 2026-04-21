import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MinerStatus from "./MinerStatus";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  deviceActions,
  performanceActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import type { BatchOperation } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";

vi.mock("@/shared/hooks/useNeedsAttention", () => ({
  useNeedsAttention: vi.fn(() => false),
}));

vi.mock("@/shared/hooks/useStatusSummary", () => ({
  useMinerStatus: vi.fn(() => "Hashing"),
}));

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
    deviceStatus: DeviceStatus.ONLINE,
    pairingStatus: PairingStatus.PAIRED,
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

function createBatch(overrides: Partial<BatchOperation> = {}): BatchOperation {
  return {
    batchIdentifier: "batch-123",
    action: deviceActions.reboot,
    deviceIdentifiers: ["test-device"],
    startedAt: Date.now(),
    status: "in_progress",
    ...overrides,
  };
}

describe("MinerStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Loading state display", () => {
    it("should show loading state when device has active batch operation and hasn't reached expected status", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.OFFLINE });

      render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: deviceActions.reboot })]}
          errorsLoaded
        />,
      );

      expect(screen.getByText("Rebooting")).toBeInTheDocument();
    });

    it("should show pool assignment loading state", () => {
      const miner = createMockMiner({
        deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
      });

      render(
        <MinerStatus miner={miner} errors={[]} activeBatches={[createBatch({ action: "mining-pool" })]} errorsLoaded />,
      );

      expect(screen.getByText("Adding pools")).toBeInTheDocument();
    });

    it("should show spinner during batch operation", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });

      const { container } = render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: deviceActions.shutdown })]}
          errorsLoaded
        />,
      );

      expect(screen.getByText("Sleeping")).toBeInTheDocument();
      expect(container.querySelector(".animate-spin")).toBeInTheDocument();
    });

    it("should prioritize loading state over normal status", async () => {
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });

      render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: deviceActions.blinkLEDs })]}
          errorsLoaded
        />,
      );

      expect(screen.getByText("Blinking LEDs")).toBeInTheDocument();
      expect(screen.queryByText("Hashing")).not.toBeInTheDocument();
    });

    it("should show unpairing loading state during unpair batch operation", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });

      render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: deviceActions.unpair })]}
          errorsLoaded
        />,
      );

      expect(screen.getByText("Unpairing")).toBeInTheDocument();
      expect(screen.queryByText("Hashing")).not.toBeInTheDocument();
    });

    it("should show manage power loading state during manage-power batch operation", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });

      const { container } = render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: performanceActions.managePower })]}
          errorsLoaded
        />,
      );

      expect(screen.getByText("Updating power")).toBeInTheDocument();
      expect(screen.queryByText("Hashing")).not.toBeInTheDocument();
      expect(container.querySelector(".animate-spin")).toBeInTheDocument();
    });

    it("should show first batch when device has multiple active batches", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.OFFLINE });

      render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[
            createBatch({
              batchIdentifier: "batch-1",
              action: deviceActions.reboot,
            }),
            createBatch({ batchIdentifier: "batch-2", action: "mining-pool" }),
          ]}
          errorsLoaded
        />,
      );

      expect(screen.getByText("Rebooting")).toBeInTheDocument();
    });
  });

  describe("Normal status display", () => {
    it("should show normal status when no batch operations", async () => {
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });

      render(<MinerStatus miner={miner} errors={[]} activeBatches={[]} errorsLoaded />);

      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Rebooting")).not.toBeInTheDocument();
    });

    it("should show needs attention status when no batches", async () => {
      const { useNeedsAttention } = await import("@/shared/hooks/useNeedsAttention");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const miner = createMockMiner({
        pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
        deviceStatus: DeviceStatus.ONLINE,
      });

      render(<MinerStatus miner={miner} errors={[]} activeBatches={[]} errorsLoaded />);

      expect(screen.getByText("Needs attention")).toBeInTheDocument();
    });
  });

  describe("Status after pool assignment", () => {
    it("should clear needs attention when pool assigned to device without errors", async () => {
      const { useNeedsAttention } = await import("@/shared/hooks/useNeedsAttention");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const miner = createMockMiner({
        deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
      });

      const { rerender } = render(<MinerStatus miner={miner} errors={[]} activeBatches={[]} errorsLoaded />);
      expect(screen.getByText("Needs attention")).toBeInTheDocument();

      // Optimistic update: status changes to ONLINE after pool assignment
      vi.mocked(useNeedsAttention).mockReturnValue(false);
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      const updatedMiner = createMockMiner({
        deviceStatus: DeviceStatus.ONLINE,
      });
      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[]} errorsLoaded />);
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Needs attention")).not.toBeInTheDocument();
    });

    it("should still show needs attention when pool assigned to device with hardware errors", async () => {
      const { useNeedsAttention } = await import("@/shared/hooks/useNeedsAttention");
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");

      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const miner = createMockMiner({
        deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
      });

      const { rerender } = render(<MinerStatus miner={miner} errors={[]} activeBatches={[]} errorsLoaded />);
      expect(screen.getByText("Needs attention")).toBeInTheDocument();

      // Optimistic update: status changes to ERROR (has hardware errors)
      vi.mocked(useNeedsAttention).mockReturnValue(true);
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const updatedMiner = createMockMiner({
        deviceStatus: DeviceStatus.ERROR,
      });
      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[]} errorsLoaded />);
      expect(screen.getByText("Needs attention")).toBeInTheDocument();
    });
  });

  describe("Loading state clears when expected status reached", () => {
    it("should show 'Sleeping' when device reaches INACTIVE during shutdown batch", async () => {
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");

      const batch = createBatch({ action: deviceActions.shutdown });
      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });

      const { rerender } = render(<MinerStatus miner={miner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      expect(screen.getByText("Sleeping")).toBeInTheDocument();

      // Device reaches INACTIVE status
      vi.mocked(useMinerStatus).mockReturnValue("Sleeping");
      const updatedMiner = createMockMiner({
        deviceStatus: DeviceStatus.INACTIVE,
      });

      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should now show actual "Sleeping" status (not loading)
      expect(screen.getByText("Sleeping")).toBeInTheDocument();
    });

    it("should show actual status when device reaches non-INACTIVE during wakeUp batch", async () => {
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useMinerStatus).mockReturnValue("Sleeping");

      const batch = createBatch({ action: deviceActions.wakeUp });
      const miner = createMockMiner({ deviceStatus: DeviceStatus.INACTIVE });

      const { rerender } = render(<MinerStatus miner={miner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should show loading state initially
      expect(screen.getByText("Waking")).toBeInTheDocument();

      // Device reaches ONLINE status
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");
      const updatedMiner = createMockMiner({
        deviceStatus: DeviceStatus.ONLINE,
      });

      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should now show actual "Hashing" status
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Waking up")).not.toBeInTheDocument();
    });

    it("should show loading during reboot until minimum 15 seconds elapsed", async () => {
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useMinerStatus).mockReturnValue("Offline");

      const now = Date.now();
      const batch = createBatch({
        action: deviceActions.reboot,
        startedAt: now - 10000,
      });
      const miner = createMockMiner({ deviceStatus: DeviceStatus.OFFLINE });

      const { rerender } = render(<MinerStatus miner={miner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should show loading state (less than 15s elapsed)
      expect(screen.getByText("Rebooting")).toBeInTheDocument();

      // Device reaches ONLINE status after only 10s
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");
      const updatedMiner = createMockMiner({
        deviceStatus: DeviceStatus.ONLINE,
      });

      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should still show "Rebooting" loading state (< 15s elapsed)
      expect(screen.getByText("Rebooting")).toBeInTheDocument();
      expect(screen.queryByText("Hashing")).not.toBeInTheDocument();

      // Update batch to 16 seconds ago (> 15s minimum)
      const olderBatch = createBatch({
        action: deviceActions.reboot,
        startedAt: now - 16000,
      });

      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[olderBatch]} errorsLoaded />);

      // Now should show actual "Hashing" status (> 15s elapsed and status is ONLINE)
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Rebooting")).not.toBeInTheDocument();
    });

    it("should show actual status when device reaches non-NEEDS_MINING_POOL during pool assignment", async () => {
      const { useMinerStatus } = await import("@/shared/hooks/useStatusSummary");
      vi.mocked(useMinerStatus).mockReturnValue("Needs attention");

      const batch = createBatch({ action: "mining-pool" });
      const miner = createMockMiner({
        deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
      });

      const { rerender } = render(<MinerStatus miner={miner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should show loading state initially
      expect(screen.getByText("Adding pools")).toBeInTheDocument();

      // Device reaches ONLINE status
      vi.mocked(useMinerStatus).mockReturnValue("Hashing");
      const updatedMiner = createMockMiner({
        deviceStatus: DeviceStatus.ONLINE,
      });

      rerender(<MinerStatus miner={updatedMiner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      // Should now show actual "Hashing" status
      expect(screen.getByText("Hashing")).toBeInTheDocument();
      expect(screen.queryByText("Adding pools")).not.toBeInTheDocument();
    });

    it("should continue showing loading when device hasn't reached expected status", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.ONLINE });
      const batch = createBatch({ action: deviceActions.shutdown });

      render(<MinerStatus miner={miner} errors={[]} activeBatches={[batch]} errorsLoaded />);

      expect(screen.getByText("Sleeping")).toBeInTheDocument();
    });
  });

  describe("Click handling", () => {
    it("should call onClick when clickable and loading state", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.OFFLINE });
      const onClick = vi.fn();

      render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: deviceActions.reboot })]}
          errorsLoaded
          onClick={onClick}
        />,
      );

      const element = screen.getByText("Rebooting");
      element.click();

      expect(onClick).toHaveBeenCalledTimes(1);
    });

    it("should render as a button when clickable", () => {
      const miner = createMockMiner({ deviceStatus: DeviceStatus.OFFLINE });
      const onClick = vi.fn();

      const { container } = render(
        <MinerStatus
          miner={miner}
          errors={[]}
          activeBatches={[createBatch({ action: deviceActions.reboot })]}
          errorsLoaded
          onClick={onClick}
        />,
      );

      const button = container.querySelector("button");
      expect(button).toBeTruthy();
      expect(button?.className).toContain("cursor-pointer");
      expect(button?.className).toContain("hover:underline");
    });
  });
});
