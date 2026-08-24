import { useMemo } from "react";
import { mapErrorSourceToComponentType } from "../utils";
import { useControlBoard, useFan, useHashboardHardware, useMinerStore, usePsu } from "@/protoOS/store";
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
 * Hook to fetch component hardware data reactively.
 *
 * Delegates to the already-narrowed hardware hooks (mirroring useComponentTelemetry)
 * so it only re-renders when the specific component's sub-map changes, not on every
 * nested hardware write.
 * @param source - The error source type
 * @param slot - The 1-based component slot
 * @returns Hardware data for the component
 */
export function useComponentHardware(source: ErrorSource, slot: number | undefined): ComponentHardware {
  const componentType = mapErrorSourceToComponentType(source);

  // Resolve the hashboard serial from its slot via a scalar selector (only the
  // serial string, so this doesn't re-render on unrelated hardware writes).
  const hashboardSerial = useMinerStore((state) =>
    componentType === "hashboard" && slot !== undefined ? state.hardware.getHashboardBySlot(slot)?.serial : undefined,
  );

  const fan = useFan(componentType === "fan" && slot !== undefined ? slot : -1);
  const psu = usePsu(componentType === "psu" && slot !== undefined ? slot : -1);
  const hashboard = useHashboardHardware(hashboardSerial ?? "");
  const controlBoard = useControlBoard();

  return useMemo(() => {
    if (slot === undefined) {
      // For control board or when no slot
      return componentType === "controlBoard" ? controlBoard : undefined;
    }

    switch (componentType) {
      case "fan":
        return fan;
      case "psu":
        return psu;
      case "hashboard":
        return hashboard;
      default:
        return undefined;
    }
  }, [componentType, slot, fan, psu, hashboard, controlBoard]);
}
