import { create } from "zustand";
import { subscribeWithSelector } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";
import {
  type ComponentStatusUpdate,
  ComponentStatusUpdate_Component,
  MeasurementConfig_MeasurementType,
  type MeasurementUpdate,
  MinerComponentStatus,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

interface FleetState {
  // Core data
  miners: Record<string, MinerStateSnapshot>; // deviceIdentifier -> miner data
  minerIds: string[]; // ordered list of miner IDs for the fleet

  // Loading states
  isLoading: boolean;
  isStreaming: boolean;
  cursor: string;

  // Actions
  setMiners: (miners: MinerStateSnapshot[]) => void;
  updateMinerMeasurement: (
    deviceId: string,
    measurement: MeasurementUpdate,
  ) => void;
  updateMinerStatus: (deviceId: string, status: ComponentStatusUpdate) => void;
  updateMinerTimestamp: (deviceId: string, timestamp: any) => void;
  setLoading: (loading: boolean) => void;
  setStreaming: (streaming: boolean) => void;
  setCursor: (cursor: string) => void;

  // Selectors
  getMinersArray: () => MinerStateSnapshot[];
}

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
      miner[propertyName] = [measurement, ...currentValues.slice(0, -1)];
    } else {
      miner[propertyName] = [measurement];
    }
  }
}

function updateStatus(
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

export const useFleetStore = create<FleetState>()(
  subscribeWithSelector(
    immer((set, get) => ({
      // Initial state
      miners: {},
      minerIds: [],
      isLoading: false,
      isStreaming: false,
      cursor: "",

      // Actions
      setMiners: (miners) =>
        set((state) => {
          state.miners = {};
          state.minerIds = [];

          miners.forEach((miner) => {
            state.miners[miner.deviceIdentifier] = miner;
            state.minerIds.push(miner.deviceIdentifier);
          });
        }),

      updateMinerMeasurement: (deviceId, measurementUpdate) =>
        set((state) => {
          const miner = state.miners[deviceId];
          if (miner) {
            updateMeasurement(measurementUpdate, miner);
          }
        }),

      updateMinerStatus: (deviceId, statusUpdate) =>
        set((state) => {
          const miner = state.miners[deviceId];
          if (miner) {
            updateStatus(statusUpdate, miner);
          }
        }),

      updateMinerTimestamp: (deviceId, timestamp) =>
        set((state) => {
          const miner = state.miners[deviceId];
          if (miner) {
            miner.timestamp = timestamp;
          }
        }),

      setLoading: (loading) =>
        set((state) => {
          state.isLoading = loading;
        }),

      setStreaming: (streaming) =>
        set((state) => {
          state.isStreaming = streaming;
        }),

      setCursor: (cursor) =>
        set((state) => {
          state.cursor = cursor;
        }),

      // Selectors
      getMinersArray: () => {
        const state = get();
        return state.minerIds.map((id) => state.miners[id]).filter(Boolean);
      },
    })),
  ),
);

// Selector hooks for specific miner data
export const useMiner = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]);

export const useMinerIds = () => useFleetStore((state) => state.minerIds);

export const useFleetMiners = () =>
  useFleetStore((state) => state.getMinersArray());

export const useIsLoading = () => useFleetStore((state) => state.isLoading);

export const useIsStreaming = () => useFleetStore((state) => state.isStreaming);

// Property-specific selectors for surgical updates
export const useMinerName = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.name);

export const useMinerMacAddress = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.macAddress);

export const useMinerStatus = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.status);

export const useMinerHashrate = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.hashrate);

export const useMinerEfficiency = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.efficiency);

export const useMinerPowerUsage = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.powerUsage);

export const useMinerTemperature = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.temperature);

export const useMinerUrl = (deviceId: string) =>
  useFleetStore((state) => state.miners[deviceId]?.url);
