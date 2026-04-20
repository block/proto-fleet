import { timestampMs } from "@bufbuild/protobuf/wkt";
import {
  COMPONENT_NAMES,
  COMPONENT_TITLES,
  ERROR_COMPONENT_TO_SHARED,
  SHARED_TO_ERROR_COMPONENT,
  SUPPORTED_COMPONENT_TYPES,
} from "./constants";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type {
  ComponentMetadata,
  ComponentMetric,
  ComponentStatusModalProps,
  ComponentType,
  ErrorData,
} from "@/shared/components/StatusModal/types";
import {
  computeComponentStatusTitle,
  type GroupedStatusErrors,
  type StatusComponentType,
} from "@/shared/hooks/useStatusSummary";

/**
 * Get the display title for a component type
 */
export const getComponentTitle = (type: ComponentType): string => {
  return COMPONENT_TITLES[type];
};

/**
 * Get the component name (without "status")
 */
export const getComponentName = (type: ComponentType): string => {
  return COMPONENT_NAMES[type];
};

/**
 * Maps error API component type to shared component type
 */
export function mapErrorComponentTypeToShared(type: ErrorComponentType): ComponentType | null {
  return ERROR_COMPONENT_TO_SHARED[type] ?? null;
}

/**
 * Maps shared component type to error API component type
 */
export function mapSharedToErrorComponentType(type: ComponentType): ErrorComponentType {
  return SHARED_TO_ERROR_COMPONENT[type];
}

/**
 * Type for grouped fleet errors returned by useGroupedErrors hook
 */
export type GroupedFleetErrors = {
  hashboard: ErrorMessage[];
  psu: ErrorMessage[];
  fan: ErrorMessage[];
  controlBoard: ErrorMessage[];
  other: ErrorMessage[];
};

/**
 * Transform ProtoFleet grouped errors to shared format for status computation
 */
export function transformFleetErrorsToShared(groupedErrors: GroupedFleetErrors): GroupedStatusErrors {
  const transformErrors = (errors: ErrorMessage[], componentType: StatusComponentType) =>
    errors.map((e) => {
      // For "other" errors, don't include slot - componentId may be a pool index or other identifier
      if (componentType === "other") {
        return { componentType, slot: undefined };
      }
      const parsed = e.componentId ? parseInt(e.componentId, 10) : NaN;
      // componentId is already 1-based slot from firmware
      return {
        componentType,
        slot: !isNaN(parsed) ? parsed : undefined,
      };
    });

  return {
    hashboard: transformErrors(groupedErrors.hashboard, "hashboard"),
    psu: transformErrors(groupedErrors.psu, "psu"),
    fan: transformErrors(groupedErrors.fan, "fan"),
    controlBoard: transformErrors(groupedErrors.controlBoard, "controlBoard"),
    other: transformErrors(groupedErrors.other, "other"),
  };
}

/**
 * Get display index from component ID for UI display purposes only
 * ComponentId contains 1-based slot values from firmware ("1", "2", "3")
 * This will change when componentId becomes a unique ID
 */
export function getComponentDisplayIndex(componentId: string): number | null {
  // componentId is already 1-based slot from firmware, use as-is for display
  const slot = parseInt(componentId, 10);
  return isNaN(slot) ? null : slot;
}

/**
 * Transforms errors array to ErrorData format for shared/components/StatusModal
 * Groups errors by component and creates ErrorData objects
 * Only creates onClick handlers when componentId exists
 */
export function transformErrorsForModal(
  errors: ErrorMessage[],
  deviceId: string,
  onClick?: (deviceId: string, type: ErrorComponentType, componentId: string) => void,
): ErrorData[] {
  const result: ErrorData[] = [];

  errors.forEach((error) => {
    let componentName = "Unknown Component";
    let componentClickHandler: (() => void) | undefined;

    // Check if error has a supported componentType
    if (error.componentType && SUPPORTED_COMPONENT_TYPES.has(error.componentType)) {
      const sharedType = mapErrorComponentTypeToShared(error.componentType);

      if (sharedType) {
        // Check if we have componentId for display and onClick
        if (error.componentId) {
          const componentIdValue = error.componentId; // Capture value for closure
          const displayIndex = getComponentDisplayIndex(componentIdValue);

          componentName = displayIndex
            ? `${getComponentName(sharedType)} ${displayIndex}`
            : getComponentName(sharedType);

          // Create onClick handler with componentId
          if (onClick) {
            componentClickHandler = () => onClick(deviceId, error.componentType, componentIdValue);
          }
        } else {
          // No componentId - just show component type without index
          componentName = getComponentName(sharedType);
          // No onClick handler since we can't navigate without componentId
        }
      }
    } else {
      // Handle unsupported or missing component types as "other"
      componentName = getComponentName("other");
    }

    // Handle timestamp conversion - convert to seconds for shared formatters
    let timestamp: number | undefined;
    if (error.lastSeenAt) {
      timestamp = Math.floor(timestampMs(error.lastSeenAt) / 1000);
    }

    // Create ErrorData object with the expected structure
    result.push({
      componentName,
      message: error.summary || "Unknown error",
      timestamp,
      onClick: componentClickHandler,
    });
  });

  return result;
}

/**
 * Build component status props from fleet data
 */
export function buildComponentStatusProps(
  miner: MinerStateSnapshot | undefined,
  componentType: ErrorComponentType,
  componentId: string,
  allErrors?: ErrorMessage[], // Pass errors from normalized store
): ComponentStatusModalProps | undefined {
  if (!miner) return undefined;

  const sharedType = mapErrorComponentTypeToShared(componentType);
  if (!sharedType) return undefined;

  // Get display index for UI
  const displayIndex = getComponentDisplayIndex(componentId);

  // Get component-specific errors (only for supported component types)
  const componentErrors =
    allErrors?.filter((error) => {
      return (
        error.componentType === componentType &&
        error.componentId === componentId &&
        SUPPORTED_COMPONENT_TYPES.has(componentType)
      );
    }) || [];

  // Transform errors to the shared format
  const errors: ErrorData[] = componentErrors.map((error) => {
    // Handle timestamp conversion - convert to seconds for shared formatters
    let timestamp: number | undefined;
    if (error.lastSeenAt) {
      timestamp = Math.floor(timestampMs(error.lastSeenAt) / 1000);
    }

    return {
      componentName: `${getComponentName(sharedType)} ${displayIndex}`,
      message: error.summary || "Unknown error",
      timestamp,
    };
  });

  // No component-level telemetry metrics available
  // TODO: Backend only collects miner-level aggregated telemetry
  const telemetry: ComponentMetric[] = [];

  // Build metadata
  const metadata: ComponentMetadata = {
    component: {
      label: "Component",
      value: `${getComponentName(sharedType)} ${displayIndex}`,
    },
    device: {
      label: "Device",
      value: miner.name || miner.deviceIdentifier,
    },
  };

  if (miner.model) {
    metadata.model = { label: "Model", value: miner.model };
  }

  // Compute summary using shared logic
  const summary =
    computeComponentStatusTitle(sharedType, displayIndex ?? undefined, componentErrors.length) ?? undefined;

  return {
    componentType: sharedType,
    summary,
    metrics: telemetry,
    errors,
    metadata,
  };
}
