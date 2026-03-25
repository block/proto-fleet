import { useShallow } from "zustand/react/shallow";
import type { BatchOperation, MinerStateSnapshot } from "../slices/fleetSlice";
import { useFleetStore } from "../useFleetStore";
import type { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

// =============================================================================
// Constants
// =============================================================================

// Stable reference for empty measurement array (prevents infinite re-renders)
const EMPTY_MEASUREMENT: Measurement[] = [];

// =============================================================================
// Fleet State Selectors
// =============================================================================

export const useMiner = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]);

export const useMinerIds = () => useFleetStore((state) => state.fleet.minerIds);

export const useTotalMiners = () => useFleetStore((state) => state.fleet.totalMiners);

export const useDeviceStatusCounts = () => useFleetStore((state) => state.fleet.deviceStatusCounts);

export const useFleetMiners = () => useFleetStore(useShallow((state) => state.fleet.getMinersArray()));

export const useIsLoading = () => useFleetStore((state) => state.fleet.isLoading);

// =============================================================================
// Property-specific selectors for surgical updates
// =============================================================================

export const useMinerName = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]?.name);

export const useMinerMacAddress = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.macAddress);

export const useMinerIpAddress = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.ipAddress);

export const useMinerModel = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]?.model);

export const useMinerFirmwareVersion = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.firmwareVersion);

export const useMinerWorkerName = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.workerName);

export const useMinerDeviceStatus = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.deviceStatus);

export const useMinerRackLabel = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.rackLabel);

export const useMinerGroupLabels = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.groupLabels);

// =============================================================================
// Shared Measurement Helper
// =============================================================================

/**
 * Generic hook for retrieving miner measurement data with consistent loading state logic.
 * @param deviceId - The device identifier
 * @param measurementGetter - Function to extract the specific measurement from a miner
 * @returns undefined (skeleton), null (dash placeholder), empty array (empty cell), or Measurement[] (data)
 *
 * Special cases:
 * - Returns empty array [] for devices with NEEDS_MINING_POOL or AUTHENTICATION_NEEDED status
 *   (components render this as truly empty cell, not dash)
 */
const useMinerMeasurement = (
  deviceId: string,
  measurementGetter: (miner: MinerStateSnapshot) => Measurement[] | undefined,
) =>
  useFleetStore((state) => {
    const miner = state.fleet.miners[deviceId];
    if (!miner) return undefined;

    // Offline miners should always show placeholder, not stale cached values
    if (miner.deviceStatus === DeviceStatus.OFFLINE) {
      return null;
    }

    // Show empty cell for devices with pool required or auth required status
    const needsPool = miner.deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
    const needsAuth = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
    if (needsPool || needsAuth) {
      return EMPTY_MEASUREMENT; // Stable empty array reference to prevent infinite re-renders
    }

    const measurementData = measurementGetter(miner);
    const hasValidData = measurementData && getLatestMeasurementWithData(measurementData);

    if (!hasValidData) {
      if (miner.deviceStatus === DeviceStatus.INACTIVE) {
        return null;
      }
      return undefined;
    }

    return measurementData;
  });

/**
 * Returns hashrate data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show dash)
 * - []: miner has pool/auth required status (show empty cell)
 * - Measurement[]: has valid telemetry data (show value)
 */
export const useMinerHashrate = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.hashrate);

/**
 * Returns efficiency data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show dash)
 * - []: miner has pool/auth required status (show empty cell)
 * - Measurement[]: has valid telemetry data (show value)
 */
export const useMinerEfficiency = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.efficiency);

/**
 * Returns power usage data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show dash)
 * - []: miner has pool/auth required status (show empty cell)
 * - Measurement[]: has valid telemetry data (show value)
 */
export const useMinerPowerUsage = (deviceId: string) => useMinerMeasurement(deviceId, (miner) => miner.powerUsage);

/**
 * Returns temperature data for a miner.
 * - undefined: miner not in store OR (no valid telemetry data AND device is online) (show skeleton)
 * - null: no valid telemetry data AND device is offline/inactive (show dash)
 * - []: miner has pool/auth required status (show empty cell)
 * - Measurement[]: has valid telemetry data (show value)
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
// Batch Operations Selectors
// =============================================================================

/**
 * Hook to get active batch operations for a specific device
 * @param deviceId The device identifier
 * @returns Array of active batch operations for the device
 */
export const useMinerActiveBatches = (deviceId: string): BatchOperation[] => {
  const result = useFleetStore(
    useShallow((state) => {
      const batchIds = state.fleet.batchOperations.byDeviceId[deviceId];
      if (!batchIds || batchIds.length === 0) {
        return [];
      }

      return batchIds.map((id) => state.fleet.batchOperations.byBatchId[id]).filter(Boolean);
    }),
  );
  return result;
};

// =============================================================================
// Fleet Action Selectors
// =============================================================================

export const useSetMiners = () => useFleetStore((state) => state.fleet.setMiners);

export const useAppendMiners = () => useFleetStore((state) => state.fleet.appendMiners);

export const useSetTotalMiners = () => useFleetStore((state) => state.fleet.setTotalMiners);

export const useSetDeviceStatusCounts = () => useFleetStore((state) => state.fleet.setDeviceStatusCounts);

export const useSetRefetchCallback = () => useFleetStore((state) => state.fleet.setRefetchCallback);

export const useUpdateMinerTimestamp = () => useFleetStore((state) => state.fleet.updateMinerTimestamp);

export const useUpdateMinerName = () => useFleetStore((state) => state.fleet.updateMinerName);

export const useSetLoading = () => useFleetStore((state) => state.fleet.setLoading);

export const useSetCursor = () => useFleetStore((state) => state.fleet.setCursor);

export const useLastPairingCompletedAt = () => useFleetStore((state) => state.fleet.lastPairingCompletedAt);

export const useNotifyPairingCompleted = () => useFleetStore((state) => state.fleet.notifyPairingCompleted);

export const useStartBatchOperation = () => useFleetStore((state) => state.fleet.startBatchOperation);

export const useCompleteBatchOperation = () => useFleetStore((state) => state.fleet.completeBatchOperation);

export const useRemoveDevicesFromBatch = () => useFleetStore((state) => state.fleet.removeDevicesFromBatch);

export const useCleanupStaleBatches = () => useFleetStore((state) => state.fleet.cleanupStaleBatches);

/**
 * Hook to get the current count of active batch operations
 * @returns Number of active batch operations
 */
export const useBatchOperationCount = () =>
  useFleetStore((state) => Object.keys(state.fleet.batchOperations.byBatchId).length);
