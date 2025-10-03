import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Telemetry Hooks
// =============================================================================

// Entity hooks
export const useMinerTelemetry = () =>
  useMinerStore((state) => state.telemetry.miner);

export const useHashboardsTelemetry = () =>
  useMinerStore(
    useShallow((state) => Array.from(state.telemetry.hashboards.values())),
  );

export const useHashboardTelemetry = (id: string) =>
  useMinerStore((state) => state.telemetry.hashboards.get(id));

export const useAsicsTelemetry = () =>
  useMinerStore(
    useShallow((state) => Array.from(state.telemetry.asics.values())),
  );

export const useAsicTelemetry = (id: string) =>
  useMinerStore((state) => state.telemetry.asics.get(id));

export const useIntervalMs = () =>
  useMinerStore((state) => state.telemetry.intervalMs);

// =============================================================================
// Action Hooks
// =============================================================================

/**
 * Hook to get the updateTelemetryData action
 * Used by API hooks to update the store with fresh telemetry data
 */
export const useUpdateTelemetryData = () =>
  useMinerStore((state) => state.telemetry.updateTelemetryData);

/**
 * Hook to get the updateHashboardTemperatures action
 * Used by API hooks to update hashboard inlet/outlet temperatures
 */
export const useUpdateHashboardTemperatures = () =>
  useMinerStore((state) => state.telemetry.updateHashboardTemperatures);
