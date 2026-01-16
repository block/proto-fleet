import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { ComponentType } from "@/shared/components/StatusModal/types";

/**
 * Mapping from error API component types to shared component types
 * Only includes supported component types - unsupported types will return undefined
 */
export const ERROR_COMPONENT_TO_SHARED: Partial<Record<ErrorComponentType, ComponentType>> = {
  [ErrorComponentType.HASH_BOARD]: "hashboard",
  [ErrorComponentType.PSU]: "psu",
  [ErrorComponentType.FAN]: "fan",
  [ErrorComponentType.CONTROL_BOARD]: "controlBoard",
};

/**
 * Mapping from shared component types to error API component types
 */
export const SHARED_TO_ERROR_COMPONENT: Record<ComponentType, ErrorComponentType> = {
  hashboard: ErrorComponentType.HASH_BOARD,
  psu: ErrorComponentType.PSU,
  fan: ErrorComponentType.FAN,
  controlBoard: ErrorComponentType.CONTROL_BOARD,
  other: ErrorComponentType.UNSPECIFIED,
};

/**
 * Component display titles
 */
export const COMPONENT_TITLES: Record<ComponentType, string> = {
  fan: "Fan status",
  hashboard: "Hashboard status",
  psu: "PSU status",
  controlBoard: "Control board status",
  other: "Needs attention",
};

/**
 * Component names (without "status")
 */
export const COMPONENT_NAMES: Record<ComponentType, string> = {
  fan: "Fan",
  hashboard: "Hashboard",
  psu: "PSU",
  controlBoard: "Control board",
  other: "Needs attention",
};

/**
 * Set of component types that are supported in the UI
 * Components not in this set (like EEPROM, IO_MODULE) will be ignored since they are not yet accommodated in the UI
 *
 * TODO: Add support for these component types in the UI
 */
export const SUPPORTED_COMPONENT_TYPES = new Set<ErrorComponentType>([
  ErrorComponentType.HASH_BOARD,
  ErrorComponentType.PSU,
  ErrorComponentType.FAN,
  ErrorComponentType.CONTROL_BOARD,
]);
