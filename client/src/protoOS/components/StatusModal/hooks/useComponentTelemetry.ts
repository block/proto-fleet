import { useMemo } from "react";
import { mapErrorSourceToComponentType } from "../utils";
import {
  useFanTelemetry,
  useHashboardTelemetry,
  useMinerStore,
  useMinerTelemetry,
  usePsuTelemetry,
} from "@/protoOS/store";
import type {
  ErrorSource,
  FanTelemetryData,
  HashboardTelemetryData,
  MinerTelemetryData,
  PsuTelemetryData,
} from "@/protoOS/store/types";

// Union type for component telemetry data
export type ComponentTelemetry =
  | FanTelemetryData
  | PsuTelemetryData
  | HashboardTelemetryData
  | MinerTelemetryData
  | undefined
  | null;

/**
 * Hook to fetch component telemetry data reactively
 * @param source - The error source type
 * @param slot - The 1-based component slot
 * @returns Telemetry data for the component
 */
export function useComponentTelemetry(source: ErrorSource, slot: number | undefined): ComponentTelemetry {
  const componentType = mapErrorSourceToComponentType(source);

  // Get hashboard serial if needed (for hashboard type)
  const hashboardSerial = useMinerStore((state) => {
    if (componentType === "hashboard" && slot !== undefined) {
      const hashboard = state.hardware.getHashboardBySlot(slot);
      return hashboard?.serial;
    }
    return undefined;
  });

  // Telemetry data is now fetched in the parent StatusModal component
  // This hook only reads from the store

  const fanTelemetry = useFanTelemetry(componentType === "fan" && slot !== undefined ? slot : -1);

  const psuTelemetry = usePsuTelemetry(componentType === "psu" && slot !== undefined ? slot : -1);

  const hashboardTelemetry = useHashboardTelemetry(
    componentType === "hashboard" && hashboardSerial ? hashboardSerial : "", // Pass empty string if not a hashboard
  );

  const controlBoardTelemetry = useMinerTelemetry();

  // Return the appropriate telemetry data
  return useMemo(() => {
    switch (componentType) {
      case "fan":
        return fanTelemetry;
      case "psu":
        return psuTelemetry;
      case "hashboard":
        return hashboardTelemetry;
      case "controlBoard":
        return controlBoardTelemetry;
      default:
        return undefined;
    }
  }, [componentType, fanTelemetry, psuTelemetry, hashboardTelemetry, controlBoardTelemetry]);
}
