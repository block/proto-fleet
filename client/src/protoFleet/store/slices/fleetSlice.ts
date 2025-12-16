import { create as createSchema } from "@bufbuild/protobuf";
import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import { MeasurementSchema } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { type ErrorMessage, type Status, type Summary } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  type DeviceStatusUpdate,
  MeasurementConfig_MeasurementType,
  type MeasurementUpdate,
  type MinerTelemetry,
  type MinerStateSnapshot as ProtoMinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DeviceStatus,
  MeasurementType,
  MinerStateCounts,
  MinerStateCountsSchema,
  type TelemetryUpdate,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

// =============================================================================
// Type Definitions
// =============================================================================

// Extended MinerStateSnapshot type with error status
export interface MinerStateSnapshot extends ProtoMinerStateSnapshot {
  errorStatus?: {
    status: Status;
    summary: Summary;
    errors: ErrorMessage[];
    countsBySeverity: Record<string, number>;
  };
}

// =============================================================================
// Helper Functions
// =============================================================================

function updateMeasurement(measurementUpdate: MeasurementUpdate, miner: MinerStateSnapshot): void {
  const type = measurementUpdate.measurementType;
  const measurement = measurementUpdate.measurement;

  if (!measurement) return;

  const measurementTypeToProperty = {
    [MeasurementConfig_MeasurementType.HASHRATE]: "hashrate",
    [MeasurementConfig_MeasurementType.POWER_USAGE]: "powerUsage",
    [MeasurementConfig_MeasurementType.TEMPERATURE]: "temperature",
    [MeasurementConfig_MeasurementType.EFFICIENCY]: "efficiency",
  } as const;

  const propertyName = measurementTypeToProperty[type as keyof typeof measurementTypeToProperty];

  if (propertyName) {
    const currentValues = miner[propertyName];

    if (currentValues && currentValues.length > 0) {
      miner[propertyName] = [...currentValues.slice(1), measurement];
    } else {
      miner[propertyName] = [measurement];
    }
  }
}

function updateTelemetryMeasurement(telemetryUpdate: TelemetryUpdate, miner: MinerStateSnapshot): void {
  if (!telemetryUpdate.data) return;

  const type = telemetryUpdate.data.measurementType;
  const telemetryData = telemetryUpdate.data;

  // Convert telemetry data to measurement format using proper protobuf creation
  const measurement = createSchema(MeasurementSchema, {
    value: telemetryData.value,
    unit: telemetryData.unit,
    timestamp: telemetryData.timestamp,
  });

  const measurementTypeToProperty = {
    [MeasurementType.HASHRATE]: "hashrate",
    [MeasurementType.POWER]: "powerUsage",
    [MeasurementType.TEMPERATURE]: "temperature",
    [MeasurementType.EFFICIENCY]: "efficiency",
  } as const;

  const propertyName = measurementTypeToProperty[type as keyof typeof measurementTypeToProperty];

  if (propertyName) {
    const currentValues = miner[propertyName];

    if (currentValues && currentValues.length > 0) {
      miner[propertyName] = [...currentValues.slice(1), measurement];
    } else {
      miner[propertyName] = [measurement];
    }
  }
}

function updateDeviceStatus(deviceStatus: DeviceStatusUpdate, miner: MinerStateSnapshot): void {
  if (!miner.deviceStatus) {
    miner.deviceStatus = DeviceStatus.UNSPECIFIED;
  }

  miner.deviceStatus = deviceStatus.status;
}

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

  totalMiners: number; // total number of miners in the fleet
  deviceStatusCounts: MinerStateCounts; // counts of miners by device status

  // Loading states
  isLoading: boolean;
  isStreaming: boolean;
  cursor: string;

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
  updateMinerMeasurement: (deviceId: string, measurement: MeasurementUpdate) => void;
  updateMinerTelemetry: (deviceId: string, telemetryUpdate: TelemetryUpdate) => void;
  updateBatchTelemetry: (telemetryData: MinerTelemetry[]) => void;
  updateMinerDeviceStatus: (deviceId: string, deviceStatusUpdate: DeviceStatusUpdate) => void;
  updateMinerTimestamp: (deviceId: string, timestamp: any) => void;
  setLoading: (loading: boolean) => void;
  setStreaming: (streaming: boolean) => void;
  setCursor: (cursor: string) => void;
  notifyPairingCompleted: () => void;

  // Error management actions
  setMinerErrors: (deviceId: string, errorStatus: MinerStateSnapshot["errorStatus"]) => void;
  updateMinerError: (deviceId: string, error: ErrorMessage) => void;
  removeMinerError: (deviceId: string, errorId: string) => void;
  updateMinersErrorStatuses: (errorStatuses: Record<string, any>) => void;

  // Selectors
  getMinersArray: () => MinerStateSnapshot[];
  isHashing: (deviceId: string) => boolean;
}

// =============================================================================
// Fleet Slice Creator
// =============================================================================

export const createFleetSlice: StateCreator<FleetStore, [["zustand/immer", never]], [], FleetSlice> = (set, get) => ({
  // Initial state
  miners: {},
  minerIds: [],
  totalMiners: 0,
  deviceStatusCounts: createSchema(MinerStateCountsSchema, {}),
  isLoading: false,
  isStreaming: false,
  cursor: "",
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

  updateMinerMeasurement: (deviceId, measurementUpdate) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        updateMeasurement(measurementUpdate, miner);
      }
    }),

  updateMinerTelemetry: (deviceId, telemetryUpdate) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        updateTelemetryMeasurement(telemetryUpdate, miner);
      }
    }),

  updateBatchTelemetry: (telemetryData) =>
    set((state) => {
      telemetryData.forEach((telemetry) => {
        const miner = state.fleet.miners[telemetry.deviceIdentifier];
        if (miner) {
          if (telemetry.powerUsage.length > 0) {
            miner.powerUsage = telemetry.powerUsage;
          }
          if (telemetry.temperature.length > 0) {
            miner.temperature = telemetry.temperature;
          }
          if (telemetry.hashrate.length > 0) {
            miner.hashrate = telemetry.hashrate;
          }
          if (telemetry.efficiency.length > 0) {
            miner.efficiency = telemetry.efficiency;
          }
          if (telemetry.timestamp) {
            miner.timestamp = telemetry.timestamp;
          }
          if (telemetry.deviceStatus) {
            miner.deviceStatus = telemetry.deviceStatus;
          }
        }
      });
    }),

  updateMinerDeviceStatus: (deviceId, deviceStatusUpdate) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        updateDeviceStatus(deviceStatusUpdate, miner);
      }
    }),

  updateMinerTimestamp: (deviceId, timestamp) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        miner.timestamp = timestamp;
      }
    }),

  setLoading: (loading) =>
    set((state) => {
      state.fleet.isLoading = loading;
    }),

  setStreaming: (streaming) =>
    set((state) => {
      state.fleet.isStreaming = streaming;
    }),

  setCursor: (cursor) =>
    set((state) => {
      state.fleet.cursor = cursor;
    }),

  notifyPairingCompleted: () =>
    set((state) => {
      state.fleet.lastPairingCompletedAt = Date.now();
    }),

  // Error management actions
  setMinerErrors: (deviceId, errorStatus) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        miner.errorStatus = errorStatus;
      }
    }),

  updateMinerError: (deviceId, error) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        if (!miner.errorStatus) {
          miner.errorStatus = {
            status: 0, // STATUS_UNSPECIFIED - will be updated based on severity
            summary: {
              $typeName: "errors.v1.Summary" as const,
              title: "",
              details: "",
              condensed: "",
            } as Summary,
            errors: [],
            countsBySeverity: {},
          };
        }

        // Add or update error in the list
        const existingIndex = miner.errorStatus.errors.findIndex((e) => e.errorId === error.errorId);
        if (existingIndex >= 0) {
          miner.errorStatus.errors[existingIndex] = error;
        } else {
          miner.errorStatus.errors.push(error);
        }

        // Update counts by severity
        // Note: This is a simplified implementation - you may want to recalculate from all errors
        const severityStr = error.severity.toString();
        miner.errorStatus.countsBySeverity[severityStr] = (miner.errorStatus.countsBySeverity[severityStr] || 0) + 1;
      }
    }),

  removeMinerError: (deviceId, errorId) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner?.errorStatus) {
        miner.errorStatus.errors = miner.errorStatus.errors.filter((e) => e.errorId !== errorId);

        // Recalculate counts by severity
        miner.errorStatus.countsBySeverity = {};
        for (const error of miner.errorStatus.errors) {
          const severityStr = error.severity.toString();
          miner.errorStatus.countsBySeverity[severityStr] = (miner.errorStatus.countsBySeverity[severityStr] || 0) + 1;
        }
      }
    }),

  updateMinersErrorStatuses: (errorStatuses) =>
    set((state) => {
      // errorStatuses is a map of deviceId -> DeviceError
      Object.entries(errorStatuses).forEach(([deviceId, deviceError]) => {
        const miner = state.fleet.miners[deviceId];
        if (miner && deviceError) {
          miner.errorStatus = {
            status: deviceError.status,
            summary: deviceError.summary,
            errors: deviceError.errors || [],
            countsBySeverity: deviceError.countsBySeverity || {},
          };
        }
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
});
