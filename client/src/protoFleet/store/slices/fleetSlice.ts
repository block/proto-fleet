import { create as createSchema } from "@bufbuild/protobuf";
import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import { type ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  type MinerListFilter,
  type MinerStateSnapshot as ProtoMinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DeviceStatus,
  MinerStateCounts,
  MinerStateCountsSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

// =============================================================================
// Type Definitions
// =============================================================================

// Extended MinerStateSnapshot type
export interface MinerStateSnapshot extends ProtoMinerStateSnapshot {
  // Lightweight error references for quick access
  errorIds?: string[];
  hasErrors?: boolean;
}

// Normalized error state structure
export interface ErrorState {
  // Primary storage - all ErrorMessage objects live here
  byId: Record<string, ErrorMessage>;

  // Indexing structures (only store error IDs)
  byDevice: Record<string, string[]>;

  // Metadata for staleness/subscription tracking
  metadata: {
    lastFetchedAt: number | null;
    lastFetchScope: "all" | "devices" | null;
    fetchedDeviceIds: string[]; // Which devices we last fetched
    activeSubscription: "component" | "device" | null;
  };
}

// Batch operation types
export type SupportedAction = string; // This will match the action types from constants.ts

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

// Batch operations state structure
export interface BatchOperationsState {
  byBatchId: Record<string, BatchOperation>; // batchIdentifier → BatchOperation
  byDeviceId: Record<string, string[]>; // deviceIdentifier → array of batchIdentifiers
}

// =============================================================================
// Helper Functions
// =============================================================================

const isHashing = (minerSnapshot: MinerStateSnapshot) => {
  if (!minerSnapshot) return false;
  if (minerSnapshot.deviceStatus === DeviceStatus.OFFLINE) return false;
  if (minerSnapshot.deviceStatus === DeviceStatus.INACTIVE) return false;
  const hashrate = getLatestMeasurementWithData(minerSnapshot.hashrate);
  if (!hashrate) return false;
  return hashrate.value > 0;
};

// =============================================================================
// Fleet Slice Interface
// =============================================================================

export interface FleetSlice {
  // Core data
  miners: Record<string, MinerStateSnapshot>; // deviceIdentifier -> miner data
  minerIds: string[]; // ordered list of miner IDs for the fleet

  // NEW: Normalized error state
  errors: ErrorState;

  // Batch operations state
  batchOperations: BatchOperationsState;

  totalMiners: number; // total number of miners in the fleet
  deviceStatusCounts: MinerStateCounts; // counts of miners by device status

  // Loading states
  isLoading: boolean;
  cursor: string;

  // Current filter applied to the fleet list (synced from URL params)
  currentFilter: MinerListFilter | null;

  // Pairing coordination
  lastPairingCompletedAt: number; // timestamp when pairing operations last completed

  // Refetch callback
  refetchMiners?: () => void;

  // Actions
  setMiners: (miners: MinerStateSnapshot[]) => void;
  appendMiners: (miners: MinerStateSnapshot[]) => void;
  addMiners: (additions: any[]) => void; // Add miners with position hints
  removeMiners: (deviceIds: string[]) => void; // Remove miners by ID
  setTotalMiners: (count: number) => void;
  setDeviceStatusCounts: (counts: MinerStateCounts) => void;
  setRefetchCallback: (callback?: () => void) => void;
  setCurrentFilter: (filter: MinerListFilter | null) => void;
  updateMinerTimestamp: (deviceId: string, timestamp: any) => void;
  updateMinerName: (deviceId: string, name: string) => void;
  setLoading: (loading: boolean) => void;
  setCursor: (cursor: string) => void;
  notifyPairingCompleted: () => void;

  // Normalized error management actions
  setErrors: (errors: ErrorMessage[], scope: "all" | "devices", deviceIds?: string[]) => void;
  handleErrorStreamEvent: (event: "OPENED" | "UPDATED" | "CLOSED", error: ErrorMessage) => void;

  // Batch operations actions
  startBatchOperation: (batch: BatchOperationInput) => void;
  completeBatchOperation: (batchIdentifier: string) => void;
  removeDevicesFromBatch: (batchIdentifier: string, deviceIds: string[]) => void;
  cleanupStaleBatches: () => void;

  // Selectors
  getMinersArray: () => MinerStateSnapshot[];
  isHashing: (deviceId: string) => boolean;

  // Error selectors
  selectErrorsByDevice: (deviceId: string) => ErrorMessage[];
}

// =============================================================================
// Fleet Slice Creator
// =============================================================================

export const createFleetSlice: StateCreator<FleetStore, [["zustand/immer", never]], [], FleetSlice> = (set, get) => ({
  // Initial state
  miners: {},
  minerIds: [],
  errors: {
    byId: {},
    byDevice: {},
    metadata: {
      lastFetchedAt: null,
      lastFetchScope: null,
      fetchedDeviceIds: [],
      activeSubscription: null,
    },
  },
  batchOperations: {
    byBatchId: {},
    byDeviceId: {},
  },
  totalMiners: 0,
  deviceStatusCounts: createSchema(MinerStateCountsSchema, {}),
  isLoading: false,
  cursor: "",
  currentFilter: null,
  lastPairingCompletedAt: 0,
  refetchMiners: undefined,

  // Actions
  setMiners: (miners) =>
    set((state) => {
      state.fleet.miners = {};
      state.fleet.minerIds = [];

      miners.forEach((miner) => {
        state.fleet.miners[miner.deviceIdentifier] = miner;
        state.fleet.minerIds.push(miner.deviceIdentifier);
      });
    }),

  appendMiners: (miners) =>
    set((state) => {
      const existingIds = new Set(state.fleet.minerIds);

      miners.forEach((miner) => {
        // Only add if not already present
        if (!existingIds.has(miner.deviceIdentifier)) {
          state.fleet.miners[miner.deviceIdentifier] = miner;
          state.fleet.minerIds.push(miner.deviceIdentifier);
        }
      });
    }),

  addMiners: (additions) =>
    set((state) => {
      // Sort additions by position to process in order
      const sortedAdditions = [...additions].sort((a, b) => a.position - b.position);

      sortedAdditions.forEach((addition) => {
        const miner = addition.miner;
        const position = addition.position;

        // Skip if position is beyond what we've loaded
        // This prevents gaps in our displayed list
        if (position > state.fleet.minerIds.length) {
          return;
        }

        // Add miner to the map
        state.fleet.miners[miner.deviceIdentifier] = miner;

        // Remove from current position if it exists
        const currentIndex = state.fleet.minerIds.indexOf(miner.deviceIdentifier);
        if (currentIndex !== -1) {
          state.fleet.minerIds.splice(currentIndex, 1);
        }

        // Insert at specified position
        // Position is always provided by the server for consistent ordering
        state.fleet.minerIds.splice(position, 0, miner.deviceIdentifier);
      });
    }),

  removeMiners: (deviceIds) =>
    set((state) => {
      deviceIds.forEach((deviceId) => {
        // Remove from map
        delete state.fleet.miners[deviceId];

        // Remove from ordered list
        const index = state.fleet.minerIds.indexOf(deviceId);
        if (index !== -1) {
          state.fleet.minerIds.splice(index, 1);
        }
      });
    }),

  setTotalMiners: (count) =>
    set((state) => {
      state.fleet.totalMiners = count;
    }),

  setDeviceStatusCounts: (counts) =>
    set((state) => {
      state.fleet.deviceStatusCounts = counts;
    }),

  setRefetchCallback: (callback) =>
    set((state) => {
      state.fleet.refetchMiners = callback;
    }),

  setCurrentFilter: (filter) =>
    set((state) => {
      state.fleet.currentFilter = filter ?? null;
    }),

  updateMinerTimestamp: (deviceId, timestamp) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        miner.timestamp = timestamp;
      }
    }),

  updateMinerName: (deviceId, name) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        miner.name = name;
      }
    }),

  setLoading: (loading) =>
    set((state) => {
      state.fleet.isLoading = loading;
    }),

  setCursor: (cursor) =>
    set((state) => {
      state.fleet.cursor = cursor;
    }),

  notifyPairingCompleted: () =>
    set((state) => {
      state.fleet.lastPairingCompletedAt = Date.now();
    }),

  // Normalized error management actions
  setErrors: (errors, scope, deviceIds) =>
    set((state) => {
      if (scope === "devices" && deviceIds) {
        // Scoped update: only clear errors for the specific devices being fetched
        deviceIds.forEach((deviceId) => {
          const existingErrorIds = state.fleet.errors.byDevice[deviceId] || [];
          existingErrorIds.forEach((errorId) => {
            delete state.fleet.errors.byId[errorId];
          });
          delete state.fleet.errors.byDevice[deviceId];

          const miner = state.fleet.miners[deviceId];
          if (miner) {
            miner.errorIds = [];
            miner.hasErrors = false;
          }
        });
      } else {
        // Full update: clear everything (used for initial load / "all" scope)
        state.fleet.errors.byId = {};
        state.fleet.errors.byDevice = {};

        Object.values(state.fleet.miners).forEach((miner) => {
          miner.errorIds = [];
          miner.hasErrors = false;
        });
      }

      // Populate with new errors
      errors.forEach((error) => {
        state.fleet.errors.byId[error.errorId] = error;

        if (!state.fleet.errors.byDevice[error.deviceIdentifier]) {
          state.fleet.errors.byDevice[error.deviceIdentifier] = [];
        }
        state.fleet.errors.byDevice[error.deviceIdentifier].push(error.errorId);

        const miner = state.fleet.miners[error.deviceIdentifier];
        if (miner) {
          if (!miner.errorIds) miner.errorIds = [];
          miner.errorIds.push(error.errorId);
          miner.hasErrors = true;
        }
      });

      // Update metadata
      state.fleet.errors.metadata = {
        lastFetchedAt: Date.now(),
        lastFetchScope: scope,
        fetchedDeviceIds: deviceIds || [],
        activeSubscription: scope === "all" ? "component" : "device",
      };
    }),

  handleErrorStreamEvent: (event, error) =>
    set((state) => {
      if (event === "CLOSED") {
        // Remove error from all indexes
        delete state.fleet.errors.byId[error.errorId];

        // Remove from device index
        if (state.fleet.errors.byDevice[error.deviceIdentifier]) {
          state.fleet.errors.byDevice[error.deviceIdentifier] = state.fleet.errors.byDevice[
            error.deviceIdentifier
          ].filter((id) => id !== error.errorId);
          if (state.fleet.errors.byDevice[error.deviceIdentifier].length === 0) {
            delete state.fleet.errors.byDevice[error.deviceIdentifier];
          }
        }

        // Update miner
        const miner = state.fleet.miners[error.deviceIdentifier];
        if (miner) {
          miner.errorIds = miner.errorIds?.filter((id) => id !== error.errorId) || [];
          miner.hasErrors = miner.errorIds.length > 0;
        }
      } else {
        // OPENED or UPDATED - add/update error
        state.fleet.errors.byId[error.errorId] = error;

        // Ensure indexes exist and add if not present
        if (!state.fleet.errors.byDevice[error.deviceIdentifier]) {
          state.fleet.errors.byDevice[error.deviceIdentifier] = [];
        }
        if (!state.fleet.errors.byDevice[error.deviceIdentifier].includes(error.errorId)) {
          state.fleet.errors.byDevice[error.deviceIdentifier].push(error.errorId);
        }

        // Update miner
        const miner = state.fleet.miners[error.deviceIdentifier];
        if (miner) {
          if (!miner.errorIds) miner.errorIds = [];
          if (!miner.errorIds.includes(error.errorId)) {
            miner.errorIds.push(error.errorId);
          }
          miner.hasErrors = true;
        }
      }
    }),

  // Batch operations actions
  startBatchOperation: (batch) =>
    set((state) => {
      const batchOperation: BatchOperation = {
        batchIdentifier: batch.batchIdentifier,
        action: batch.action,
        deviceIdentifiers: batch.deviceIdentifiers,
        startedAt: Date.now(),
        status: "in_progress",
      };

      // Add to byBatchId map
      state.fleet.batchOperations.byBatchId[batch.batchIdentifier] = batchOperation;

      // Add to byDeviceId index for each device
      batch.deviceIdentifiers.forEach((deviceId) => {
        if (!state.fleet.batchOperations.byDeviceId[deviceId]) {
          state.fleet.batchOperations.byDeviceId[deviceId] = [];
        }
        // Only add if not already present
        if (!state.fleet.batchOperations.byDeviceId[deviceId].includes(batch.batchIdentifier)) {
          state.fleet.batchOperations.byDeviceId[deviceId].push(batch.batchIdentifier);
        }
      });
    }),

  completeBatchOperation: (batchIdentifier) =>
    set((state) => {
      const batch = state.fleet.batchOperations.byBatchId[batchIdentifier];
      if (!batch) {
        console.warn(`[Store] Batch ${batchIdentifier} not found for completion`);
        return;
      }

      // Remove batch ID from each device's array
      batch.deviceIdentifiers.forEach((deviceId) => {
        if (state.fleet.batchOperations.byDeviceId[deviceId]) {
          state.fleet.batchOperations.byDeviceId[deviceId] = state.fleet.batchOperations.byDeviceId[deviceId].filter(
            (id) => id !== batchIdentifier,
          );
          // Delete empty arrays
          if (state.fleet.batchOperations.byDeviceId[deviceId].length === 0) {
            delete state.fleet.batchOperations.byDeviceId[deviceId];
          }
        }
      });

      // Delete batch from byBatchId
      delete state.fleet.batchOperations.byBatchId[batchIdentifier];
    }),

  removeDevicesFromBatch: (batchIdentifier, deviceIds) =>
    set((state) => {
      const batch = state.fleet.batchOperations.byBatchId[batchIdentifier];
      if (!batch) return;

      // Remove batch ID from specified devices
      deviceIds.forEach((deviceId) => {
        if (state.fleet.batchOperations.byDeviceId[deviceId]) {
          state.fleet.batchOperations.byDeviceId[deviceId] = state.fleet.batchOperations.byDeviceId[deviceId].filter(
            (id) => id !== batchIdentifier,
          );
          // Delete empty arrays
          if (state.fleet.batchOperations.byDeviceId[deviceId].length === 0) {
            delete state.fleet.batchOperations.byDeviceId[deviceId];
          }
        }
      });

      // Remove devices from batch's deviceIdentifiers
      batch.deviceIdentifiers = batch.deviceIdentifiers.filter((id) => !deviceIds.includes(id));

      // If no devices left in batch, delete the batch entirely
      if (batch.deviceIdentifiers.length === 0) {
        delete state.fleet.batchOperations.byBatchId[batchIdentifier];
      }
    }),

  cleanupStaleBatches: () =>
    set((state) => {
      const now = Date.now();
      const staleThreshold = 5 * 60 * 1000; // 5 minutes in milliseconds

      // Find batches older than threshold
      const staleBatchIds = Object.keys(state.fleet.batchOperations.byBatchId).filter((batchId) => {
        const batch = state.fleet.batchOperations.byBatchId[batchId];
        return now - batch.startedAt > staleThreshold;
      });

      // Complete each stale batch
      staleBatchIds.forEach((batchId) => {
        const batch = state.fleet.batchOperations.byBatchId[batchId];
        if (!batch) return;

        // Remove from device indexes
        batch.deviceIdentifiers.forEach((deviceId) => {
          if (state.fleet.batchOperations.byDeviceId[deviceId]) {
            state.fleet.batchOperations.byDeviceId[deviceId] = state.fleet.batchOperations.byDeviceId[deviceId].filter(
              (id) => id !== batchId,
            );
            if (state.fleet.batchOperations.byDeviceId[deviceId].length === 0) {
              delete state.fleet.batchOperations.byDeviceId[deviceId];
            }
          }
        });

        // Remove from byId
        delete state.fleet.batchOperations.byBatchId[batchId];
      });
    }),

  // Selectors
  getMinersArray: () => {
    const state = get();
    return state.fleet.minerIds.map((id) => state.fleet.miners[id]).filter(Boolean);
  },

  isHashing: (deviceId: string) => {
    const state = get();
    return isHashing(state.fleet.miners[deviceId]);
  },

  // Error selectors
  selectErrorsByDevice: (deviceId: string) => {
    const state = get();
    const errorIds = state.fleet.errors.byDevice[deviceId] || [];
    return errorIds.map((id) => state.fleet.errors.byId[id]).filter(Boolean);
  },
});
