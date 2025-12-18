import { useMemo } from "react";
import { ComponentType as ErrorComponentType, ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { useFleetStore } from "@/protoFleet/store";

/**
 * Hook to get component-specific errors
 * @param deviceId The device identifier
 * @param componentType The component type
 * @param componentId The component ID (optional, if not provided returns all errors for the component type)
 * @returns Errors for the specific component
 */
export function useComponentErrors(deviceId: string, componentType: ErrorComponentType, componentId?: string) {
  return useFleetStore((state) => {
    // Get errors from normalized store
    const errors = state.fleet.selectErrorsByDevice(deviceId);
    if (!errors || errors.length === 0) return [];

    return errors.filter((error) => {
      // Filter by component type
      if (error.componentType !== componentType) return false;

      // If componentId is provided, filter by it
      if (componentId !== undefined) {
        return error.componentId === componentId;
      }

      return true;
    });
  });
}

/**
 * Hook to get grouped errors by component type
 * @param deviceId The device identifier
 * @returns Errors grouped by component type
 */
export function useGroupedErrors(deviceId: string) {
  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const errors = selectErrorsByDevice(deviceId);

  return useMemo(() => {
    const grouped = {
      hashboard: [] as ErrorMessage[],
      psu: [] as ErrorMessage[],
      fan: [] as ErrorMessage[],
      controlBoard: [] as ErrorMessage[],
    };

    if (!errors || errors.length === 0) return grouped;

    errors.forEach((error) => {
      // Use componentType directly from error
      switch (error.componentType) {
        case ErrorComponentType.HASH_BOARD:
          grouped.hashboard.push(error);
          break;
        case ErrorComponentType.PSU:
          grouped.psu.push(error);
          break;
        case ErrorComponentType.FAN:
          grouped.fan.push(error);
          break;
        case ErrorComponentType.CONTROL_BOARD:
          grouped.controlBoard.push(error);
          break;
      }
    });

    return grouped;
  }, [errors]);
}

export type ComponentHardware = {
  model?: string;
  serialNumber?: string;
  firmwareVersion?: string;
};
