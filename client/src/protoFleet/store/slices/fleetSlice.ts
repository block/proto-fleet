import { create as createSchema } from "@bufbuild/protobuf";
import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import { MeasurementSchema } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import {
  type ComponentStatusUpdate,
  ComponentStatusUpdate_Component,
  type DeviceStatusUpdate,
  MeasurementConfig_MeasurementType,
  type MeasurementUpdate,
  MinerComponentStatus,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DeviceStatus,
  MeasurementType,
  MinerStateCounts,
  MinerStateCountsSchema,
  type TelemetryUpdate,
  type TemperatureStatusCount,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

// =============================================================================
// Helper Functions
// =============================================================================

function updateMeasurement(
  measurementUpdate: MeasurementUpdate,
  miner: MinerStateSnapshot,
): void {
  const type = measurementUpdate.measurementType;
  const measurement = measurementUpdate.measurement;

  if (!measurement) return;

  const measurementTypeToProperty = {
    [MeasurementConfig_MeasurementType.HASHRATE]: "hashrate",
    [MeasurementConfig_MeasurementType.POWER_USAGE]: "powerUsage",
    [MeasurementConfig_MeasurementType.TEMPERATURE]: "temperature",
    [MeasurementConfig_MeasurementType.EFFICIENCY]: "efficiency",
  } as const;

  const propertyName =
    measurementTypeToProperty[type as keyof typeof measurementTypeToProperty];

  if (propertyName) {
    const currentValues = miner[propertyName];

    if (currentValues && currentValues.length > 0) {
      miner[propertyName] = [...currentValues.slice(1), measurement];
    } else {
      miner[propertyName] = [measurement];
    }
  }
}

function updateTelemetryMeasurement(
  telemetryUpdate: TelemetryUpdate,
  miner: MinerStateSnapshot,
): void {
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

  const propertyName =
    measurementTypeToProperty[type as keyof typeof measurementTypeToProperty];

  if (propertyName) {
    const currentValues = miner[propertyName];

    if (currentValues && currentValues.length > 0) {
      miner[propertyName] = [...currentValues.slice(1), measurement];
    } else {
      miner[propertyName] = [measurement];
    }
  }
}

function updateComponentStatus(
  { status, component }: ComponentStatusUpdate,
  miner: MinerStateSnapshot,
): void {
  if (!miner.status) {
    miner.status = {
      controlBoard: 0,
      fans: 0,
      hashBoards: 0,
      psu: 0,
    } as MinerComponentStatus;
  }

  const componentToProperty = {
    [ComponentStatusUpdate_Component.CONTROL_BOARD]: "controlBoard",
    [ComponentStatusUpdate_Component.FANS]: "fans",
    [ComponentStatusUpdate_Component.HASH_BOARDS]: "hashBoards",
    [ComponentStatusUpdate_Component.PSU]: "psu",
  } as const;

  const propertyName =
    componentToProperty[component as keyof typeof componentToProperty];
  if (propertyName) {
    miner.status[propertyName] = status;
  }
}

function updateDeviceStatus(
  deviceStatus: DeviceStatusUpdate,
  miner: MinerStateSnapshot,
): void {
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
  temperatureStatusCounts: TemperatureStatusCount[]; // temperature status distribution over time

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
  setTemperatureStatusCounts: (counts: TemperatureStatusCount[]) => void;
  setRefetchCallback: (callback?: () => void) => void;
  updateMinerMeasurement: (
    deviceId: string,
    measurement: MeasurementUpdate,
  ) => void;
  updateMinerTelemetry: (
    deviceId: string,
    telemetryUpdate: TelemetryUpdate,
  ) => void;
  updateMinerComponentStatus: (
    deviceId: string,
    status: ComponentStatusUpdate,
  ) => void;
  updateMinerDeviceStatus: (
    deviceId: string,
    deviceStatusUpdate: DeviceStatusUpdate,
  ) => void;
  updateMinerTimestamp: (deviceId: string, timestamp: any) => void;
  setLoading: (loading: boolean) => void;
  setStreaming: (streaming: boolean) => void;
  setCursor: (cursor: string) => void;
  notifyPairingCompleted: () => void;

  // Selectors
  getMinersArray: () => MinerStateSnapshot[];
  isHashing: (deviceId: string) => boolean;
}

// =============================================================================
// Fleet Slice Creator
// =============================================================================

export const createFleetSlice: StateCreator<
  FleetStore,
  [["zustand/immer", never]],
  [],
  FleetSlice
> = (set, get) => ({
  // Initial state
  miners: {},
  minerIds: [],
  totalMiners: 0,
  deviceStatusCounts: createSchema(MinerStateCountsSchema, {}),
  temperatureStatusCounts: [],
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
      const sortedAdditions = [...additions].sort(
        (a, b) => a.position - b.position,
      );

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
        const currentIndex = state.fleet.minerIds.indexOf(
          miner.deviceIdentifier,
        );
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

  setTemperatureStatusCounts: (counts) =>
    set((state) => {
      state.fleet.temperatureStatusCounts = counts;
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

  updateMinerComponentStatus: (deviceId, statusUpdate) =>
    set((state) => {
      const miner = state.fleet.miners[deviceId];
      if (miner) {
        updateComponentStatus(statusUpdate, miner);
      }
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

  // Selectors
  getMinersArray: () => {
    const state = get();
    return state.fleet.minerIds
      .map((id) => state.fleet.miners[id])
      .filter(Boolean);
  },

  isHashing: (deviceId: string) => {
    const state = get();
    return isHashing(state.fleet.miners[deviceId]);
  },
});
