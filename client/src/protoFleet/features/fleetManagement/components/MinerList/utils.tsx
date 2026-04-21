import { ReactNode } from "react";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { Alert, ControlBoard, Fan, Hashboard, LightningAlt } from "@/shared/assets/icons";

/**
 * Map error component type to icon
 * @param componentType - The error component type from the API
 * @returns React node representing the component icon
 */
export function getComponentIcon(componentType: ErrorComponentType): ReactNode {
  const componentIconMap: Record<ErrorComponentType, ReactNode> = {
    [ErrorComponentType.UNSPECIFIED]: <Alert width="w-4" />,
    [ErrorComponentType.PSU]: <LightningAlt width="w-4" />,
    [ErrorComponentType.HASH_BOARD]: <Hashboard width="w-4" />,
    [ErrorComponentType.FAN]: <Fan width="w-4" />,
    [ErrorComponentType.CONTROL_BOARD]: <ControlBoard width="w-4" />,
    [ErrorComponentType.EEPROM]: <Alert width="w-4" />,
    [ErrorComponentType.IO_MODULE]: <Alert width="w-4" />,
  };

  return componentIconMap[componentType] ?? <Alert width="w-4" />;
}
