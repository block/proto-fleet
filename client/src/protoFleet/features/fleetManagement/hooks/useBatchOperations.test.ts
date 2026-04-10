import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBatchOperations } from "./useBatchOperations";
import {
  deviceActions,
  settingsActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// Reset the Zustand store's batch state between tests
beforeEach(() => {
  const state = useFleetStore.getState();
  // Clear all batch state by completing any existing batches
  for (const batchId of Object.keys(state.batch.byBatchId)) {
    state.batch.completeBatchOperation(batchId);
  }
});

describe("useBatchOperations", () => {
  describe("startBatchOperation", () => {
    it("should add a new batch operation", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1", "device-2"],
        });
      });

      const batches = result.current.getActiveBatches("device-1");
      expect(batches).toHaveLength(1);
      expect(batches[0].batchIdentifier).toBe("batch-123");
      expect(batches[0].action).toBe(deviceActions.reboot);
      expect(batches[0].deviceIdentifiers).toEqual(["device-1", "device-2"]);
      expect(batches[0].status).toBe("in_progress");
      expect(batches[0].startedAt).toBeGreaterThan(0);
    });

    it("should add batch ID to all devices", () => {
      const { result } = renderHook(() => useBatchOperations());
      const deviceIdentifiers = ["device-1", "device-2", "device-3"];

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers,
        });
      });

      deviceIdentifiers.forEach((deviceId) => {
        const batches = result.current.getActiveBatches(deviceId);
        expect(batches).toHaveLength(1);
        expect(batches[0].batchIdentifier).toBe("batch-123");
      });
    });

    it("should support multiple batches for the same device", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
        result.current.startBatchOperation({
          batchIdentifier: "batch-2",
          action: settingsActions.miningPool,
          deviceIdentifiers: ["device-1"],
        });
      });

      const batches = result.current.getActiveBatches("device-1");
      expect(batches).toHaveLength(2);
    });

    it("should not add duplicate batch IDs to the same device", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
        // Same batch again
        result.current.startBatchOperation({
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
      });

      const batches = result.current.getActiveBatches("device-1");
      expect(batches).toHaveLength(1);
    });
  });

  describe("completeBatchOperation", () => {
    it("should remove batch and clean up device indexes", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1", "device-2"],
        });
      });

      act(() => {
        result.current.completeBatchOperation("batch-123");
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(0);
      expect(result.current.getActiveBatches("device-2")).toHaveLength(0);
    });

    it("should handle completing non-existent batch gracefully", () => {
      const { result } = renderHook(() => useBatchOperations());
      const consoleSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

      act(() => {
        result.current.completeBatchOperation("non-existent");
      });

      expect(consoleSpy).toHaveBeenCalledWith(expect.stringContaining("non-existent"));
      consoleSpy.mockRestore();
    });

    it("should preserve other batches for the same device", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
        result.current.startBatchOperation({
          batchIdentifier: "batch-2",
          action: settingsActions.miningPool,
          deviceIdentifiers: ["device-1"],
        });
      });

      act(() => {
        result.current.completeBatchOperation("batch-1");
      });

      const batches = result.current.getActiveBatches("device-1");
      expect(batches).toHaveLength(1);
      expect(batches[0].batchIdentifier).toBe("batch-2");
    });
  });

  describe("removeDevicesFromBatch", () => {
    it("should remove specified devices from batch", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1", "device-2", "device-3"],
        });
      });

      act(() => {
        result.current.removeDevicesFromBatch("batch-123", ["device-1"]);
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(0);
      expect(result.current.getActiveBatches("device-2")).toHaveLength(1);
      expect(result.current.getActiveBatches("device-3")).toHaveLength(1);
    });

    it("should delete batch entirely if all devices are removed", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
      });

      act(() => {
        result.current.removeDevicesFromBatch("batch-123", ["device-1"]);
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(0);
      expect(result.current.getAllBatches()).toHaveLength(0);
    });

    it("should preserve other batches for the same device", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
        result.current.startBatchOperation({
          batchIdentifier: "batch-2",
          action: settingsActions.miningPool,
          deviceIdentifiers: ["device-1"],
        });
      });

      act(() => {
        result.current.removeDevicesFromBatch("batch-1", ["device-1"]);
      });

      const batches = result.current.getActiveBatches("device-1");
      expect(batches).toHaveLength(1);
      expect(batches[0].batchIdentifier).toBe("batch-2");
    });
  });

  describe("cleanupStaleBatches", () => {
    it("should remove batches older than 5 minutes", () => {
      const { result } = renderHook(() => useBatchOperations());

      // Mock Date.now to control time
      const originalNow = Date.now;
      const startTime = 1000000;
      Date.now = () => startTime;

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "old-batch",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
      });

      // Advance time past stale threshold (5 minutes)
      Date.now = () => startTime + 5 * 60 * 1000 + 1;

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "new-batch",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-2"],
        });
      });

      act(() => {
        result.current.cleanupStaleBatches();
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(0);
      expect(result.current.getActiveBatches("device-2")).toHaveLength(1);

      Date.now = originalNow;
    });
  });

  describe("getActiveBatches", () => {
    it("should return empty array for device with no batches", () => {
      const { result } = renderHook(() => useBatchOperations());
      expect(result.current.getActiveBatches("unknown-device")).toEqual([]);
    });

    it("should return all active batches for a device", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-1",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1"],
        });
        result.current.startBatchOperation({
          batchIdentifier: "batch-2",
          action: settingsActions.miningPool,
          deviceIdentifiers: ["device-1"],
        });
      });

      const batches = result.current.getActiveBatches("device-1");
      expect(batches).toHaveLength(2);
      expect(batches.map((b) => b.batchIdentifier)).toEqual(["batch-1", "batch-2"]);
    });
  });

  describe("integration: full lifecycle", () => {
    it("should handle start -> partial remove -> complete", () => {
      const { result } = renderHook(() => useBatchOperations());

      act(() => {
        result.current.startBatchOperation({
          batchIdentifier: "batch-123",
          action: deviceActions.reboot,
          deviceIdentifiers: ["device-1", "device-2", "device-3"],
        });
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(1);
      expect(result.current.getActiveBatches("device-2")).toHaveLength(1);
      expect(result.current.getActiveBatches("device-3")).toHaveLength(1);

      act(() => {
        result.current.removeDevicesFromBatch("batch-123", ["device-1"]);
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(0);
      expect(result.current.getActiveBatches("device-2")).toHaveLength(1);
      expect(result.current.getActiveBatches("device-3")).toHaveLength(1);

      act(() => {
        result.current.completeBatchOperation("batch-123");
      });

      expect(result.current.getActiveBatches("device-1")).toHaveLength(0);
      expect(result.current.getActiveBatches("device-2")).toHaveLength(0);
      expect(result.current.getActiveBatches("device-3")).toHaveLength(0);
      expect(result.current.getAllBatches()).toHaveLength(0);
    });
  });
});
