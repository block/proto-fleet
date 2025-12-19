import { useShallow } from "zustand/react/shallow";
import type { MinerStateSnapshot } from "../slices/fleetSlice";
import { useFleetStore } from "../useFleetStore";
import type { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

// =============================================================================
// Fleet State Selectors
// =============================================================================

export const useMiner = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]);

export const useMinerIds = () => useFleetStore((state) => state.fleet.minerIds);

export const useTotalMiners = () => useFleetStore((state) => state.fleet.totalMiners);

export const useDeviceStatusCounts = () => useFleetStore((state) => state.fleet.deviceStatusCounts);

export const useFleetMiners = () => useFleetStore(useShallow((state) => state.fleet.getMinersArray()));

export const useIsLoading = () => useFleetStore((state) => state.fleet.isLoading);

export const useIsStreaming = () => useFleetStore((state) => state.fleet.isStreaming);

// =============================================================================
// Property-specific selectors for surgical updates
// =============================================================================

export const useMinerName = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]?.name);

export const useMinerMacAddress = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.macAddress);

export const useMinerIpAddress = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.ipAddress);

export const useMinerDeviceStatus = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.deviceStatus);

// =============================================================================
// Shared Measurement Helper
// =============================================================================

/**
 * Generic hook for retrieving miner measurement data with consistent loading state logic.
 * @param deviceId - The device identifier
 * @param measurementGetter - Function to extract the specific measurement from a miner
 * @returns undefined (skeleton), null (blank), or Measurement[] (data)
 */
const useMinerMeasurement = (
  deviceId: string,
  measurementGetter: (miner: MinerStateSnapshot) => Measurement[] | undefined,
) =>
  useFleetStore((state) => {
    const miner = state.fleet.miners[deviceId];
    if (!miner) return undefined;

    const measurementData = measurementGetter(miner);
    const hasValidData = measurementData && getLatestMeasurementWithData(measurementData);

    if (!hasValidData) {
      if (miner.deviceStatus === DeviceStatus.OFFLINE || miner.deviceStatus === DeviceStatus.INACTIVE) {
        return null;
      }
      return undefined;
    }

    return measurementData;
  });

/**
 * Returns hashrate data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show blank)
 * - Measurement[]: has valid telemetry data (show value, even if 0)
 */
export const useMinerHashrate = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.hashrate);

/**
 * Returns efficiency data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show blank)
 * - Measurement[]: has valid telemetry data (show value, even if 0)
 */
export const useMinerEfficiency = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.efficiency);

/**
 * Returns power usage data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show blank)
 * - Measurement[]: has valid telemetry data (show value, even if 0)
 */
export const useMinerPowerUsage = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.powerUsage);

/**
 * Returns temperature data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show blank)
 * - Measurement[]: has valid telemetry data (show value, even if 0)
 */
export const useMinerTemperature = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.temperature);

export const useMinerUrl = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]?.url);

/**
 * Hook to get device errors from the store
 * @param deviceId The device identifier to get errors for
 * @returns The errors for the device
 */
export const useDeviceErrors = (deviceId: string) => {
  return useFleetStore((state) => state.fleet.selectErrorsByDevice(deviceId));
};

/**
 * Hook to get miner data from the store
 * @param deviceId The device identifier
 * @returns The miner state snapshot
 */
export const useMinerData = (deviceId: string): MinerStateSnapshot | undefined => {
  return useFleetStore((state) => state.fleet.miners[deviceId]);
};

// =============================================================================
// Fleet Action Selectors
// =============================================================================

export const useSetMiners = () => useFleetStore((state) => state.fleet.setMiners);

export const useAppendMiners = () => useFleetStore((state) => state.fleet.appendMiners);

export const useSetTotalMiners = () => useFleetStore((state) => state.fleet.setTotalMiners);

export const useSetDeviceStatusCounts = () => useFleetStore((state) => state.fleet.setDeviceStatusCounts);

export const useSetRefetchCallback = () => useFleetStore((state) => state.fleet.setRefetchCallback);

export const useUpdateMinerMeasurement = () => useFleetStore((state) => state.fleet.updateMinerMeasurement);

export const useUpdateMinerTelemetry = () => useFleetStore((state) => state.fleet.updateMinerTelemetry);

export const useUpdateMinerDeviceStatus = () => useFleetStore((state) => state.fleet.updateMinerDeviceStatus);

export const useUpdateMinerTimestamp = () => useFleetStore((state) => state.fleet.updateMinerTimestamp);

export const useSetLoading = () => useFleetStore((state) => state.fleet.setLoading);

export const useSetStreaming = () => useFleetStore((state) => state.fleet.setStreaming);

export const useSetCursor = () => useFleetStore((state) => state.fleet.setCursor);

export const useLastPairingCompletedAt = () => useFleetStore((state) => state.fleet.lastPairingCompletedAt);

export const useNotifyPairingCompleted = () => useFleetStore((state) => state.fleet.notifyPairingCompleted);
