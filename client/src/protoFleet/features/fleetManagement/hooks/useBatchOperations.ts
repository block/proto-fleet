import {
  getActiveBatches,
  getAllBatches,
  useBatchStateVersion,
  useCleanupStaleBatches,
  useCompleteBatchOperation,
  useRemoveDevicesFromBatch,
  useStartBatchOperation,
} from "@/protoFleet/store";

// Re-export types from the store slice for consumers
export type { BatchOperation, BatchOperationInput } from "@/protoFleet/store/slices/batchSlice";

/**
 * Manages ephemeral batch operation state for the fleet page.
 * Tracks in-progress operations (firmware updates, reboots, etc.) so
 * MinerStatus can show an in-progress state while an action is running.
 *
 * State is stored in the Zustand batch slice so it survives route navigation
 * (e.g., rebooting from Groups page then navigating to Miners page).
 */
export function useBatchOperations() {
  const startBatchOperation = useStartBatchOperation();
  const completeBatchOperation = useCompleteBatchOperation();
  const removeDevicesFromBatch = useRemoveDevicesFromBatch();
  const cleanupStaleBatches = useCleanupStaleBatches();
  const batchStateVersion = useBatchStateVersion();

  return {
    startBatchOperation,
    completeBatchOperation,
    removeDevicesFromBatch,
    cleanupStaleBatches,
    /** Reads directly from the store — always returns fresh data. */
    getAllBatches,
    /** Reads directly from the store — always returns fresh data. */
    getActiveBatches,
    /** Monotonic counter that increments on every batch state mutation. Use as a memo dependency. */
    batchStateVersion,
  };
}
