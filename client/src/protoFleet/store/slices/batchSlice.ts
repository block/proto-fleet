import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import type { SupportedAction } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";

// =============================================================================
// Types
// =============================================================================

export interface BatchOperation {
  batchIdentifier: string;
  action: SupportedAction;
  deviceIdentifiers: string[];
  startedAt: number;
  status: "in_progress";
}

export interface BatchOperationInput {
  batchIdentifier: string;
  action: SupportedAction;
  deviceIdentifiers: string[];
}

// =============================================================================
// Batch Slice Interface
// =============================================================================

const STALE_THRESHOLD_MS = 5 * 60 * 1000; // 5 minutes

export interface BatchSlice {
  byBatchId: Record<string, BatchOperation>;
  byDeviceId: Record<string, string[]>;
  batchStateVersion: number;

  // Actions
  startBatchOperation: (batch: BatchOperationInput) => void;
  completeBatchOperation: (batchIdentifier: string) => void;
  removeDevicesFromBatch: (batchIdentifier: string, deviceIds: string[]) => void;
  cleanupStaleBatches: () => void;
}

// =============================================================================
// Batch Slice Creator
// =============================================================================

export const createBatchSlice: StateCreator<FleetStore, [["zustand/immer", never]], [], BatchSlice> = (set) => ({
  // Initial state
  byBatchId: {},
  byDeviceId: {},
  batchStateVersion: 0,

  // Actions
  startBatchOperation: (batch) =>
    set((state) => {
      const { byBatchId, byDeviceId } = state.batch;

      // If re-starting an existing batch with different devices, clean up old device index entries
      const existing = byBatchId[batch.batchIdentifier];
      if (existing) {
        for (const oldDeviceId of existing.deviceIdentifiers) {
          if (!batch.deviceIdentifiers.includes(oldDeviceId) && byDeviceId[oldDeviceId]) {
            const filtered = byDeviceId[oldDeviceId].filter((id) => id !== batch.batchIdentifier);
            if (filtered.length === 0) {
              delete byDeviceId[oldDeviceId];
            } else {
              byDeviceId[oldDeviceId] = filtered;
            }
          }
        }
      }

      byBatchId[batch.batchIdentifier] = {
        batchIdentifier: batch.batchIdentifier,
        action: batch.action,
        deviceIdentifiers: batch.deviceIdentifiers,
        startedAt: Date.now(),
        status: "in_progress",
      };

      batch.deviceIdentifiers.forEach((deviceId) => {
        const existingIds = byDeviceId[deviceId] ?? [];
        if (!existingIds.includes(batch.batchIdentifier)) {
          byDeviceId[deviceId] = [...existingIds, batch.batchIdentifier];
        }
      });

      state.batch.batchStateVersion++;
    }),

  completeBatchOperation: (batchIdentifier) =>
    set((state) => {
      const { byBatchId, byDeviceId } = state.batch;
      const batch = byBatchId[batchIdentifier];
      if (!batch) {
        console.warn(`[batchSlice] Batch ${batchIdentifier} not found for completion`);
        return;
      }

      delete byBatchId[batchIdentifier];

      batch.deviceIdentifiers.forEach((deviceId) => {
        if (byDeviceId[deviceId]) {
          const filtered = byDeviceId[deviceId].filter((id) => id !== batchIdentifier);
          if (filtered.length === 0) {
            delete byDeviceId[deviceId];
          } else {
            byDeviceId[deviceId] = filtered;
          }
        }
      });

      state.batch.batchStateVersion++;
    }),

  removeDevicesFromBatch: (batchIdentifier, deviceIds) =>
    set((state) => {
      const { byBatchId, byDeviceId } = state.batch;
      const batch = byBatchId[batchIdentifier];
      if (!batch) return;

      const deviceIdSet = new Set(deviceIds);

      deviceIds.forEach((deviceId) => {
        if (byDeviceId[deviceId]) {
          const filtered = byDeviceId[deviceId].filter((id) => id !== batchIdentifier);
          if (filtered.length === 0) {
            delete byDeviceId[deviceId];
          } else {
            byDeviceId[deviceId] = filtered;
          }
        }
      });

      const remaining = batch.deviceIdentifiers.filter((id) => !deviceIdSet.has(id));
      if (remaining.length === 0) {
        delete byBatchId[batchIdentifier];
      } else {
        byBatchId[batchIdentifier] = { ...batch, deviceIdentifiers: remaining };
      }

      state.batch.batchStateVersion++;
    }),

  cleanupStaleBatches: () =>
    set((state) => {
      const { byBatchId, byDeviceId } = state.batch;
      const now = Date.now();
      const staleBatchIds = Object.keys(byBatchId).filter(
        (batchId) => now - byBatchId[batchId].startedAt > STALE_THRESHOLD_MS,
      );

      if (staleBatchIds.length === 0) return;

      const staleBatchIdSet = new Set(staleBatchIds);
      staleBatchIds.forEach((batchId) => {
        delete byBatchId[batchId];
      });

      Object.keys(byDeviceId).forEach((deviceId) => {
        const filtered = byDeviceId[deviceId].filter((id) => !staleBatchIdSet.has(id));
        if (filtered.length === 0) {
          delete byDeviceId[deviceId];
        } else if (filtered.length !== byDeviceId[deviceId].length) {
          byDeviceId[deviceId] = filtered;
        }
      });

      state.batch.batchStateVersion++;
    }),
});
