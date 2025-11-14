import { mapErrorSourceToComponentType } from "../utils";
import { useMinerStore } from "@/protoOS/store";
import type {
  ControlBoardHardwareData,
  ErrorSource,
  FanHardwareData,
  HashboardHardwareData,
  PsuHardwareData,
} from "@/protoOS/store/types";

// Union type for component hardware data
export type ComponentHardware =
  | FanHardwareData
  | PsuHardwareData
  | HashboardHardwareData
  | ControlBoardHardwareData
  | undefined
  | null;

/**
 * Hook to fetch component hardware data reactively
 * @param source - The error source type
 * @param componentIndex - The 0-based component index
 * @returns Hardware data for the component
 */
export function useComponentHardware(
  source: ErrorSource,
  componentIndex: number | undefined,
): ComponentHardware {
  const componentType = mapErrorSourceToComponentType(source);

  // Fetch hardware data based on component type
  const hardware = useMinerStore((state) => {
    if (componentIndex === undefined) {
      // For control board or when no index
      if (componentType === "controlBoard") {
        return state.hardware.controlBoard;
      }
      return undefined;
    }

    switch (componentType) {
      case "fan":
        return state.hardware.fans.get(componentIndex + 1);
      case "psu":
        return state.hardware.psus.get(componentIndex + 1);
      case "hashboard": {
        const hashboard = state.hardware.getHashboardBySlot(componentIndex + 1);
        return hashboard;
      }
      default:
        return undefined;
    }
  });

  return hardware;
}
