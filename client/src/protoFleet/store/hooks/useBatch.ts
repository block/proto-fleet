import type { BatchOperation } from "../slices/batchSlice";
import { useFleetStore } from "../useFleetStore";

// =============================================================================
// Batch State Selectors
// =============================================================================

export const useBatchStateVersion = () => useFleetStore((state) => state.batch.batchStateVersion);

// =============================================================================
// Batch Action Selectors
// =============================================================================

export const useStartBatchOperation = () => useFleetStore((state) => state.batch.startBatchOperation);
export const useCompleteBatchOperation = () => useFleetStore((state) => state.batch.completeBatchOperation);
export const useRemoveDevicesFromBatch = () => useFleetStore((state) => state.batch.removeDevicesFromBatch);
export const useCleanupStaleBatches = () => useFleetStore((state) => state.batch.cleanupStaleBatches);

// =============================================================================
// Batch Query Helpers (read directly from store for fresh data)
// =============================================================================

export function getActiveBatches(deviceId: string): BatchOperation[] {
  const { byDeviceId, byBatchId } = useFleetStore.getState().batch;
  const batchIds = byDeviceId[deviceId];
  if (!batchIds || batchIds.length === 0) return [];
  return batchIds.map((id) => byBatchId[id]).filter(Boolean);
}

export function getAllBatches(): BatchOperation[] {
  return Object.values(useFleetStore.getState().batch.byBatchId);
}
