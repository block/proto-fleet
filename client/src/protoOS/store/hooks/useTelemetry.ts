import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Telemetry Hooks
// =============================================================================

// Entity hooks
export const useMinerTelemetry = () => useMinerStore((state) => state.telemetry.miner);

export const useHashboardsTelemetry = () =>
  useMinerStore(useShallow((state) => Array.from(state.telemetry.hashboards.values())));

export const useHashboardTelemetry = (id: string) => useMinerStore((state) => state.telemetry.hashboards.get(id));

export const useAsicsTelemetry = () => useMinerStore(useShallow((state) => Array.from(state.telemetry.asics.values())));

export const useAsicTelemetry = (id: string) => useMinerStore((state) => state.telemetry.asics.get(id));

export const usePsusTelemetry = () => useMinerStore(useShallow((state) => Array.from(state.telemetry.psus.values())));

export const usePsuTelemetry = (id: number) => useMinerStore((state) => state.telemetry.psus.get(id));

export const useFansTelemetry = () => useMinerStore(useShallow((state) => Array.from(state.telemetry.fans.values())));

export const useFanTelemetry = (id: number) => useMinerStore((state) => state.telemetry.fans.get(id));

export const useCoolingMode = () => useMinerStore((state) => state.telemetry.coolingMode);

export const useIntervalMs = () => useMinerStore((state) => state.telemetry.intervalMs);
