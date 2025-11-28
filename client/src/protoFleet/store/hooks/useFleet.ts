import { useShallow } from "zustand/react/shallow";
import { useFleetStore } from "../useFleetStore";

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

export const useMinerComponentStatus = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.status);

export const useMinerDeviceStatus = (deviceId: string) =>
  useFleetStore((state) => state.fleet.miners[deviceId]?.deviceStatus);

export const useMinerHashrate = (deviceId: string) =>
  useFleetStore((state) => {
    const miner = state.fleet.miners[deviceId];
    return state.fleet.isHashing(deviceId) ? miner?.hashrate : undefined;
  });

export const useMinerEfficiency = (deviceId: string) =>
  useFleetStore((state) => {
    const miner = state.fleet.miners[deviceId];
    return state.fleet.isHashing(deviceId) ? miner?.efficiency : undefined;
  });

export const useMinerPowerUsage = (deviceId: string) =>
  useFleetStore((state) => {
    const miner = state.fleet.miners[deviceId];
    return state.fleet.isHashing(deviceId) ? miner?.powerUsage : undefined;
  });

export const useMinerTemperature = (deviceId: string) =>
  useFleetStore((state) => {
    const miner = state.fleet.miners[deviceId];
    return state.fleet.isHashing(deviceId) ? miner?.temperature : undefined;
  });

export const useMinerUrl = (deviceId: string) => useFleetStore((state) => state.fleet.miners[deviceId]?.url);

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

export const useUpdateMinerComponentStatus = () => useFleetStore((state) => state.fleet.updateMinerComponentStatus);

export const useUpdateMinerDeviceStatus = () => useFleetStore((state) => state.fleet.updateMinerDeviceStatus);

export const useUpdateMinerTimestamp = () => useFleetStore((state) => state.fleet.updateMinerTimestamp);

export const useSetLoading = () => useFleetStore((state) => state.fleet.setLoading);

export const useSetStreaming = () => useFleetStore((state) => state.fleet.setStreaming);

export const useSetCursor = () => useFleetStore((state) => state.fleet.setCursor);

export const useLastPairingCompletedAt = () => useFleetStore((state) => state.fleet.lastPairingCompletedAt);

export const useNotifyPairingCompleted = () => useFleetStore((state) => state.fleet.notifyPairingCompleted);
