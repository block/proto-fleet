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
