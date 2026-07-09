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
 * @param slot - The 1-based component slot
 * @returns Hardware data for the component
 */
export function useComponentHardware(source: ErrorSource, slot: number | undefined): ComponentHardware {
  const componentType = mapErrorSourceToComponentType(source);

  const hardware = useMinerStore((state) => {
    if (slot === undefined) {
      // For control board or when no slot
      if (componentType === "controlBoard") {
        return state.hardware.controlBoard;
      }
      return undefined;
    }

    switch (componentType) {
      case "fan":
        return state.hardware.fans.get(slot);
      case "psu":
        return state.hardware.psus.get(slot);
      case "hashboard": {
        const hashboard = state.hardware.getHashboardBySlot(slot);
        return hashboard;
      }
      default:
        return undefined;
    }
  });

  return hardware;
}
