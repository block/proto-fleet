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
import type { MinerStateSnapshot } from "@/protoFleet/store";
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
};

/**
 * Transform ProtoFleet grouped errors to shared format for status computation
 */
export function transformFleetErrorsToShared(groupedErrors: GroupedFleetErrors): GroupedStatusErrors {
  const transformErrors = (errors: ErrorMessage[], componentType: StatusComponentType) =>
    errors.map((e) => {
      const parsed = e.componentId ? parseInt(e.componentId, 10) : NaN;
      return {
        componentType,
        componentIndex: !isNaN(parsed) ? parsed : undefined,
      };
    });

  return {
    hashboard: transformErrors(groupedErrors.hashboard, "hashboard"),
    psu: transformErrors(groupedErrors.psu, "psu"),
    fan: transformErrors(groupedErrors.fan, "fan"),
    controlBoard: transformErrors(groupedErrors.controlBoard, "controlBoard"),
  };
}

/**
 * Get display index from component ID for UI display purposes only
 * Currently componentId is just the index as a string ("0", "1", "2")
 * This will change when componentId becomes a unique ID
 */
export function getComponentDisplayIndex(componentId: string): number {
  // Currently componentId is just the index as a string
  const index = parseInt(componentId, 10);
  return isNaN(index) ? 0 : index;
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

    // Check if error has componentType (required for proper display)
    if (error.componentType && SUPPORTED_COMPONENT_TYPES.has(error.componentType)) {
      const sharedType = mapErrorComponentTypeToShared(error.componentType);

      if (sharedType) {
        // Check if we have componentId for display and onClick
        if (error.componentId) {
          const componentIdValue = error.componentId; // Capture value for closure
          const displayIndex = getComponentDisplayIndex(componentIdValue);
          componentName = `${getComponentName(sharedType)} ${displayIndex + 1}`;

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
    } else if (!error.componentType || !SUPPORTED_COMPONENT_TYPES.has(error.componentType)) {
      // Skip unsupported component types (EEPROM, IO_MODULE)
      return;
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
): ComponentStatusModalProps | undefined {
  if (!miner) return undefined;

  const sharedType = mapErrorComponentTypeToShared(componentType);
  if (!sharedType) return undefined;

  // Get display index for UI
  const displayIndex = getComponentDisplayIndex(componentId);

  // Get component-specific errors (only for supported component types)
  const componentErrors =
    miner.errorStatus?.errors?.filter((error) => {
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
      componentName: `${getComponentName(sharedType)} ${displayIndex + 1}`,
      message: error.summary || "Unknown error",
      timestamp,
    };
  });

  // No component-level telemetry metrics available
  // Backend only collects miner-level aggregated telemetry (DASH-782)
  const telemetry: ComponentMetric[] = [];

  // Build metadata
  const metadata: ComponentMetadata = {
    component: {
      label: "Component",
      value: `${getComponentName(sharedType)} ${displayIndex + 1}`,
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
  const summary = computeComponentStatusTitle(sharedType, displayIndex, componentErrors.length) ?? undefined;

  return {
    componentType: sharedType,
    summary,
    metrics: telemetry,
    errors,
    metadata,
  };
}
