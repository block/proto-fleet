import { useMemo } from "react";
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
  FanHardwareData | PsuHardwareData | HashboardHardwareData | ControlBoardHardwareData | undefined | null;

/**
 * Hook to fetch component hardware data reactively
 * @param source - The error source type
 * @param slot - The 1-based component slot
 * @returns Hardware data for the component
 */
export function useComponentHardware(source: ErrorSource, slot: number | undefined): ComponentHardware {
  const componentType = mapErrorSourceToComponentType(source);
  const hardware = useMinerStore((state) => state.hardware);

  return useMemo(() => {
    if (slot === undefined) {
      // For control board or when no slot
      if (componentType === "controlBoard") {
        return hardware.getControlBoard();
      }
      return undefined;
    }

    switch (componentType) {
      case "fan":
        return hardware.getFan(slot);
      case "psu":
        return hardware.getPsu(slot);
      case "hashboard": {
        const hashboard = hardware.getHashboardBySlot(slot);
        return hashboard;
      }
      default:
        return undefined;
    }
  }, [componentType, hardware, slot]);
}
