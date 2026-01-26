import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import type { FleetSlice } from "./fleetSlice";
import { createFleetSlice } from "./fleetSlice";
import {
  deviceActions,
  settingsActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";

type TestStore = { fleet: FleetSlice };

describe("fleetSlice - Batch Operations", () => {
  let store: any;

  beforeEach(() => {
    vi.clearAllMocks();
    // Create a fresh store for each test
    store = create<TestStore>()(
      immer((set, get, _api) => ({
        fleet: createFleetSlice(set as any, get as any, _api as any),
      })),
    );
  });

  describe("startBatchOperation", () => {
    it("should add a new batch operation to byId", () => {
      const batchIdentifier = "batch-123";
      const action = deviceActions.reboot;
      const deviceIdentifiers = ["device-1", "device-2"];

      store.getState().fleet.startBatchOperation({
        batchIdentifier,
        action,
        deviceIdentifiers,
      });

      const state = store.getState().fleet;
      expect(state.batchOperations.byBatchId[batchIdentifier]).toBeDefined();
      expect(state.batchOperations.byBatchId[batchIdentifier].batchIdentifier).toBe(batchIdentifier);
      expect(state.batchOperations.byBatchId[batchIdentifier].action).toBe(action);
      expect(state.batchOperations.byBatchId[batchIdentifier].deviceIdentifiers).toEqual(deviceIdentifiers);
      expect(state.batchOperations.byBatchId[batchIdentifier].status).toBe("in_progress");
      expect(state.batchOperations.byBatchId[batchIdentifier].startedAt).toBeGreaterThan(0);
    });

    it("should add batch ID to byDevice index for all devices", () => {
      const batchIdentifier = "batch-123";
      const deviceIdentifiers = ["device-1", "device-2", "device-3"];

      store.getState().fleet.startBatchOperation({
        batchIdentifier,
        action: deviceActions.reboot,
        deviceIdentifiers,
      });

      const state = store.getState().fleet;
      deviceIdentifiers.forEach((deviceId) => {
        expect(state.batchOperations.byDeviceId[deviceId]).toContain(batchIdentifier);
      });
    });

    it("should support multiple batches for the same device", () => {
      const batch1 = "batch-1";
      const batch2 = "batch-2";
      const deviceId = "device-1";

      store.getState().fleet.startBatchOperation({
        batchIdentifier: batch1,
        action: deviceActions.reboot,
        deviceIdentifiers: [deviceId],
      });

      store.getState().fleet.startBatchOperation({
        batchIdentifier: batch2,
        action: settingsActions.miningPool,
        deviceIdentifiers: [deviceId],
      });

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId[deviceId]).toHaveLength(2);
      expect(state.batchOperations.byDeviceId[deviceId]).toContain(batch1);
      expect(state.batchOperations.byDeviceId[deviceId]).toContain(batch2);
    });

    it("should not add duplicate batch IDs to the same device", () => {
      const batchIdentifier = "batch-123";
      const deviceId = "device-1";

      store.getState().fleet.startBatchOperation({
        batchIdentifier,
        action: deviceActions.reboot,
        deviceIdentifiers: [deviceId],
      });

      store.getState().fleet.startBatchOperation({
        batchIdentifier,
        action: deviceActions.reboot,
        deviceIdentifiers: [deviceId],
      });

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId[deviceId]).toHaveLength(1);
    });
  });

  describe("completeBatchOperation", () => {
    beforeEach(() => {
      // Set up a batch operation
      store.getState().fleet.startBatchOperation({
        batchIdentifier: "batch-123",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-1", "device-2"],
      });
    });

    it("should remove batch from byId", () => {
      store.getState().fleet.completeBatchOperation("batch-123");

      const state = store.getState().fleet;
      expect(state.batchOperations.byBatchId["batch-123"]).toBeUndefined();
    });

    it("should remove batch ID from all devices in byDevice", () => {
      store.getState().fleet.completeBatchOperation("batch-123");

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toBeUndefined();
    });

    it("should handle completing a non-existent batch gracefully", () => {
      expect(() => {
        store.getState().fleet.completeBatchOperation("non-existent");
      }).not.toThrow();
    });

    it("should preserve other batches for the same device", () => {
      const batch2 = "batch-456";
      store.getState().fleet.startBatchOperation({
        batchIdentifier: batch2,
        action: settingsActions.miningPool,
        deviceIdentifiers: ["device-1"],
      });

      store.getState().fleet.completeBatchOperation("batch-123");

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId["device-1"]).toEqual([batch2]);
      expect(state.batchOperations.byBatchId[batch2]).toBeDefined();
    });
  });

  describe("removeDevicesFromBatch", () => {
    beforeEach(() => {
      // Set up a batch operation with multiple devices
      store.getState().fleet.startBatchOperation({
        batchIdentifier: "batch-123",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-1", "device-2", "device-3"],
      });
    });

    it("should remove specified devices from batch", () => {
      store.getState().fleet.removeDevicesFromBatch("batch-123", ["device-1", "device-2"]);

      const state = store.getState().fleet;
      const batch = state.batchOperations.byBatchId["batch-123"];

      expect(batch).toBeDefined();
      expect(batch.deviceIdentifiers).toEqual(["device-3"]);
    });

    it("should remove batch ID from specified devices in byDevice", () => {
      store.getState().fleet.removeDevicesFromBatch("batch-123", ["device-1"]);

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toContain("batch-123");
      expect(state.batchOperations.byDeviceId["device-3"]).toContain("batch-123");
    });

    it("should delete batch entirely if all devices are removed", () => {
      store.getState().fleet.removeDevicesFromBatch("batch-123", ["device-1", "device-2", "device-3"]);

      const state = store.getState().fleet;
      expect(state.batchOperations.byBatchId["batch-123"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-3"]).toBeUndefined();
    });

    it("should handle removing devices from non-existent batch gracefully", () => {
      expect(() => {
        store.getState().fleet.removeDevicesFromBatch("non-existent", ["device-1"]);
      }).not.toThrow();
    });

    it("should handle removing non-existent devices from batch gracefully", () => {
      expect(() => {
        store.getState().fleet.removeDevicesFromBatch("batch-123", ["non-existent-device"]);
      }).not.toThrow();

      const state = store.getState().fleet;
      expect(state.batchOperations.byBatchId["batch-123"]).toBeDefined();
    });

    it("should preserve other batches for the same device", () => {
      const batch2 = "batch-456";
      store.getState().fleet.startBatchOperation({
        batchIdentifier: batch2,
        action: settingsActions.miningPool,
        deviceIdentifiers: ["device-1", "device-4"],
      });

      store.getState().fleet.removeDevicesFromBatch("batch-123", ["device-1"]);

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId["device-1"]).toEqual([batch2]);
      expect(state.batchOperations.byBatchId[batch2]).toBeDefined();
    });

    it("should handle partial removal in mixed success/failure scenario", () => {
      // Simulate a scenario where device-1 and device-2 failed, but device-3 succeeded
      const failedDevices = ["device-1", "device-2"];

      store.getState().fleet.removeDevicesFromBatch("batch-123", failedDevices);

      const state = store.getState().fleet;
      const batch = state.batchOperations.byBatchId["batch-123"];

      expect(batch).toBeDefined();
      expect(batch.deviceIdentifiers).toEqual(["device-3"]);
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-3"]).toContain("batch-123");
    });
  });

  describe("cleanupStaleBatches", () => {
    it("should remove batches older than 5 minutes", () => {
      const staleTime = Date.now() - 6 * 60 * 1000; // 6 minutes ago
      const recentTime = Date.now() - 1 * 60 * 1000; // 1 minute ago

      // Add a stale batch
      store.getState().fleet.startBatchOperation({
        batchIdentifier: "stale-batch",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-1"],
      });
      // Manually set the timestamp to be stale
      store.setState((state: TestStore) => {
        state.fleet.batchOperations.byBatchId["stale-batch"].startedAt = staleTime;
      });

      // Add a recent batch
      store.getState().fleet.startBatchOperation({
        batchIdentifier: "recent-batch",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-2"],
      });
      store.setState((state: TestStore) => {
        state.fleet.batchOperations.byBatchId["recent-batch"].startedAt = recentTime;
      });

      store.getState().fleet.cleanupStaleBatches();

      const state = store.getState().fleet;
      expect(state.batchOperations.byBatchId["stale-batch"]).toBeUndefined();
      expect(state.batchOperations.byBatchId["recent-batch"]).toBeDefined();
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toContain("recent-batch");
    });

    it("should handle empty batch operations", () => {
      expect(() => {
        store.getState().fleet.cleanupStaleBatches();
      }).not.toThrow();
    });

    it("should clean up byDevice indexes when removing stale batches", () => {
      const staleTime = Date.now() - 6 * 60 * 1000;

      store.getState().fleet.startBatchOperation({
        batchIdentifier: "stale-batch",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-1", "device-2"],
      });

      store.setState((state: TestStore) => {
        state.fleet.batchOperations.byBatchId["stale-batch"].startedAt = staleTime;
      });

      store.getState().fleet.cleanupStaleBatches();

      const state = store.getState().fleet;
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toBeUndefined();
    });
  });

  describe("Integration scenarios", () => {
    it("should handle complete lifecycle: start -> complete", () => {
      const batchId = "batch-lifecycle";
      const devices = ["device-1", "device-2"];

      // Start
      store.getState().fleet.startBatchOperation({
        batchIdentifier: batchId,
        action: settingsActions.miningPool,
        deviceIdentifiers: devices,
      });

      let state = store.getState().fleet;
      expect(state.batchOperations.byBatchId[batchId]).toBeDefined();
      expect(state.batchOperations.byDeviceId["device-1"]).toContain(batchId);

      // Complete
      store.getState().fleet.completeBatchOperation(batchId);

      state = store.getState().fleet;
      expect(state.batchOperations.byBatchId[batchId]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
    });

    it("should handle multiple concurrent batches", () => {
      const batches = [
        { id: "batch-1", action: deviceActions.reboot, devices: ["device-1", "device-2"] },
        { id: "batch-2", action: settingsActions.miningPool, devices: ["device-2", "device-3"] },
        { id: "batch-3", action: deviceActions.shutdown, devices: ["device-1", "device-3"] },
      ];

      batches.forEach((batch) => {
        store.getState().fleet.startBatchOperation({
          batchIdentifier: batch.id,
          action: batch.action,
          deviceIdentifiers: batch.devices,
        });
      });

      const state = store.getState().fleet;

      // Check byId
      batches.forEach((batch) => {
        expect(state.batchOperations.byBatchId[batch.id]).toBeDefined();
      });

      // Check byDevice indexes
      expect(state.batchOperations.byDeviceId["device-1"]).toEqual(expect.arrayContaining(["batch-1", "batch-3"]));
      expect(state.batchOperations.byDeviceId["device-2"]).toEqual(expect.arrayContaining(["batch-1", "batch-2"]));
      expect(state.batchOperations.byDeviceId["device-3"]).toEqual(expect.arrayContaining(["batch-2", "batch-3"]));
    });

    it("should handle mixed success/failure with removeDevicesFromBatch", () => {
      const batchId = "batch-mixed";
      const allDevices = ["device-1", "device-2", "device-3", "device-4"];
      const failedDevices = ["device-1", "device-3"];
      const successfulDevices = ["device-2", "device-4"];

      // Start batch with 4 devices
      store.getState().fleet.startBatchOperation({
        batchIdentifier: batchId,
        action: settingsActions.miningPool,
        deviceIdentifiers: allDevices,
      });

      let state = store.getState().fleet;
      expect(state.batchOperations.byBatchId[batchId].deviceIdentifiers).toEqual(allDevices);

      // Remove failed devices
      store.getState().fleet.removeDevicesFromBatch(batchId, failedDevices);

      state = store.getState().fleet;
      const batch = state.batchOperations.byBatchId[batchId];

      // Batch should still exist with only successful devices
      expect(batch).toBeDefined();
      expect(batch.deviceIdentifiers).toEqual(successfulDevices);

      // Failed devices should not have batch in byDevice
      expect(state.batchOperations.byDeviceId["device-1"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-3"]).toBeUndefined();

      // Successful devices should still have batch in byDevice
      expect(state.batchOperations.byDeviceId["device-2"]).toContain(batchId);
      expect(state.batchOperations.byDeviceId["device-4"]).toContain(batchId);

      // Complete the batch for successful devices
      store.getState().fleet.completeBatchOperation(batchId);

      state = store.getState().fleet;
      expect(state.batchOperations.byBatchId[batchId]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-2"]).toBeUndefined();
      expect(state.batchOperations.byDeviceId["device-4"]).toBeUndefined();
    });
  });
});
