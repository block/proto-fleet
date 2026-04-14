import { describe, expect, it } from "vitest";
import { deviceActions, settingsActions } from "../components/MinerActionsMenu/constants";
import { hasReachedExpectedStatus, isActionLoading } from "./batchStatusCheck";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { BatchOperation } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";

function createBatch(overrides: Partial<BatchOperation> = {}): BatchOperation {
  return {
    batchIdentifier: "batch-123",
    action: deviceActions.reboot,
    deviceIdentifiers: ["device-1"],
    startedAt: Date.now(),
    status: "in_progress",
    ...overrides,
  };
}

describe("hasReachedExpectedStatus", () => {
  describe("mining pool action", () => {
    it("returns true when status is not NEEDS_MINING_POOL", () => {
      expect(hasReachedExpectedStatus(settingsActions.miningPool, DeviceStatus.ONLINE)).toBe(true);
      expect(hasReachedExpectedStatus(settingsActions.miningPool, DeviceStatus.OFFLINE)).toBe(true);
      expect(hasReachedExpectedStatus(settingsActions.miningPool, DeviceStatus.INACTIVE)).toBe(true);
    });

    it("returns false when status is NEEDS_MINING_POOL", () => {
      expect(hasReachedExpectedStatus(settingsActions.miningPool, DeviceStatus.NEEDS_MINING_POOL)).toBe(false);
    });

    it("returns false when status is undefined", () => {
      expect(hasReachedExpectedStatus(settingsActions.miningPool, undefined)).toBe(false);
    });
  });

  describe("shutdown action", () => {
    it("returns true when status is INACTIVE", () => {
      expect(hasReachedExpectedStatus(deviceActions.shutdown, DeviceStatus.INACTIVE)).toBe(true);
    });

    it("returns false when status is not INACTIVE", () => {
      expect(hasReachedExpectedStatus(deviceActions.shutdown, DeviceStatus.ONLINE)).toBe(false);
      expect(hasReachedExpectedStatus(deviceActions.shutdown, DeviceStatus.OFFLINE)).toBe(false);
      expect(hasReachedExpectedStatus(deviceActions.shutdown, DeviceStatus.UNSPECIFIED)).toBe(false);
    });

    it("returns false when status is undefined", () => {
      expect(hasReachedExpectedStatus(deviceActions.shutdown, undefined)).toBe(false);
    });
  });

  describe("wakeUp action", () => {
    it("returns true when status is not INACTIVE", () => {
      expect(hasReachedExpectedStatus(deviceActions.wakeUp, DeviceStatus.ONLINE)).toBe(true);
      expect(hasReachedExpectedStatus(deviceActions.wakeUp, DeviceStatus.OFFLINE)).toBe(true);
      expect(hasReachedExpectedStatus(deviceActions.wakeUp, DeviceStatus.NEEDS_MINING_POOL)).toBe(true);
    });

    it("returns false when status is INACTIVE", () => {
      expect(hasReachedExpectedStatus(deviceActions.wakeUp, DeviceStatus.INACTIVE)).toBe(false);
    });

    it("returns false when status is undefined", () => {
      expect(hasReachedExpectedStatus(deviceActions.wakeUp, undefined)).toBe(false);
    });
  });

  describe("reboot action", () => {
    it("returns false when less than 15 seconds have elapsed", () => {
      const now = Date.now();
      const startedAt = now - 10000; // 10 seconds ago

      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.ONLINE, startedAt)).toBe(false);
      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.OFFLINE, startedAt)).toBe(false);
      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.INACTIVE, startedAt)).toBe(false);
    });

    it("returns true when 15+ seconds elapsed and status is not OFFLINE", () => {
      const now = Date.now();
      const startedAt = now - 16000; // 16 seconds ago

      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.ONLINE, startedAt)).toBe(true);
      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.INACTIVE, startedAt)).toBe(true);
      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.NEEDS_MINING_POOL, startedAt)).toBe(true);
    });

    it("returns false when 15+ seconds elapsed but status is OFFLINE", () => {
      const now = Date.now();
      const startedAt = now - 16000; // 16 seconds ago

      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.OFFLINE, startedAt)).toBe(false);
    });

    it("returns false when status is undefined", () => {
      const now = Date.now();
      const startedAt = now - 16000;

      expect(hasReachedExpectedStatus(deviceActions.reboot, undefined, startedAt)).toBe(false);
    });

    it("returns false when no startedAt provided (defaults to 0 elapsed)", () => {
      // Without startedAt, elapsed = 0, which is < 15000
      expect(hasReachedExpectedStatus(deviceActions.reboot, DeviceStatus.ONLINE)).toBe(false);
    });
  });

  describe("firmware update action", () => {
    it("returns true when status is REBOOT_REQUIRED", () => {
      expect(hasReachedExpectedStatus(deviceActions.firmwareUpdate, DeviceStatus.REBOOT_REQUIRED)).toBe(true);
    });

    it("returns false when status is UPDATING", () => {
      expect(hasReachedExpectedStatus(deviceActions.firmwareUpdate, DeviceStatus.UPDATING)).toBe(false);
    });

    it("returns false when status is ONLINE", () => {
      expect(hasReachedExpectedStatus(deviceActions.firmwareUpdate, DeviceStatus.ONLINE)).toBe(false);
    });

    it("returns false when status is undefined", () => {
      expect(hasReachedExpectedStatus(deviceActions.firmwareUpdate, undefined)).toBe(false);
    });
  });

  describe("unknown action", () => {
    it("returns false for unknown actions", () => {
      expect(hasReachedExpectedStatus("unknown-action", DeviceStatus.ONLINE)).toBe(false);
      expect(hasReachedExpectedStatus("unknown-action", DeviceStatus.INACTIVE)).toBe(false);
    });
  });
});

describe("isActionLoading", () => {
  it("returns false when batch is undefined", () => {
    expect(isActionLoading(undefined, DeviceStatus.ONLINE)).toBe(false);
  });

  it("returns false when action has no statusColumnLoadingMessages entry", () => {
    const batch = createBatch({ action: deviceActions.downloadLogs });
    expect(isActionLoading(batch, DeviceStatus.ONLINE)).toBe(false);
  });

  it("returns false when device has reached expected status", () => {
    const batch = createBatch({ action: deviceActions.shutdown });
    expect(isActionLoading(batch, DeviceStatus.INACTIVE)).toBe(false);
  });

  it("returns true when device has not reached expected status", () => {
    const batch = createBatch({ action: deviceActions.shutdown });
    expect(isActionLoading(batch, DeviceStatus.ONLINE)).toBe(true);
  });
});
