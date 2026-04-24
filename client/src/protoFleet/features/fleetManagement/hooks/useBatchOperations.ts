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
 * Action-only view over the batch slice. Selects stable action callbacks from
 * the store without subscribing to `batchStateVersion`, so consumers that only
 * dispatch (e.g. per-row action menus) don't re-render on every batch mutation.
 */
export function useBatchActions() {
  const startBatchOperation = useStartBatchOperation();
  const completeBatchOperation = useCompleteBatchOperation();
  const removeDevicesFromBatch = useRemoveDevicesFromBatch();
  const cleanupStaleBatches = useCleanupStaleBatches();

  return {
    startBatchOperation,
    completeBatchOperation,
    removeDevicesFromBatch,
    cleanupStaleBatches,
  };
}

/**
 * Manages ephemeral batch operation state for the fleet page.
 * Tracks in-progress operations (firmware updates, reboots, etc.) so
 * MinerStatus can show an in-progress state while an action is running.
 *
 * State is stored in the Zustand batch slice so it survives route navigation
 * (e.g., rebooting from Groups page then navigating to Miners page).
 *
 * Subscribes to `batchStateVersion` — use `useBatchActions` instead if you
 * only need the action callbacks.
 */
export function useBatchOperations() {
  const actions = useBatchActions();
  const batchStateVersion = useBatchStateVersion();

  return {
    ...actions,
    /** Reads directly from the store — always returns fresh data. */
    getAllBatches,
    /** Reads directly from the store — always returns fresh data. */
    getActiveBatches,
    /** Monotonic counter that increments on every batch state mutation. Use as a memo dependency. */
    batchStateVersion,
  };
}
