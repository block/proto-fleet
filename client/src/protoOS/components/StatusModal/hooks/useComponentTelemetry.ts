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
 * @param componentIndex - The 0-based component index
 * @returns Telemetry data for the component
 */
export function useComponentTelemetry(source: ErrorSource, componentIndex: number | undefined): ComponentTelemetry {
  const componentType = mapErrorSourceToComponentType(source);

  // Get hashboard serial if needed (for hashboard type)
  const hashboardSerial = useMinerStore((state) => {
    if (componentType === "hashboard" && componentIndex !== undefined) {
      const hashboard = state.hardware.getHashboardBySlot(componentIndex + 1);
      return hashboard?.serial;
    }
    return undefined;
  });

  // Telemetry data is now fetched in the parent StatusModal component
  // This hook only reads from the store

  // Fetch telemetry from store based on component type
  const fanTelemetry = useFanTelemetry(
    componentType === "fan" && componentIndex !== undefined ? componentIndex + 1 : -1, // Pass invalid ID if not a fan
  );

  const psuTelemetry = usePsuTelemetry(
    componentType === "psu" && componentIndex !== undefined ? componentIndex + 1 : -1, // Pass invalid ID if not a PSU
  );

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
