// Re-export store convenience hooks
export { useDeviceErrors, useMinerData } from "@/protoFleet/store";

// Re-export StatusModal-specific hooks
export { useComponentErrors, useGroupedErrors } from "./useStatusModalHooks";
export type { ComponentHardware } from "./useStatusModalHooks";
