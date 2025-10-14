import type { NotificationError } from "@/protoOS/api/generatedApi";
import type {
  ComponentError,
  ComponentType,
} from "@/shared/components/ComponentStatusModal/types";

/**
 * Determines the component type based on error properties
 */
const getComponentType = (error: NotificationError): ComponentType => {
  if (error.component_index !== undefined) {
    return "fan";
  }
  if (error.hashboard_index !== undefined) {
    return "hashboard";
  }
  // Check error code for PSU-related errors
  if (error.error_code?.toLowerCase().includes("psu")) {
    return "psu";
  }
  return "controlBoard";
};

/**
 * Generates a human-readable component name
 */
const getComponentName = (
  error: NotificationError,
  type: ComponentType,
): string => {
  switch (type) {
    case "fan":
      return error.component_index !== undefined
        ? `Fan ${error.component_index + 1}`
        : "Fan";
    case "hashboard":
      return error.hashboard_index !== undefined
        ? `Hashboard ${error.hashboard_index + 1}`
        : "Hashboard";
    case "psu":
      return "PSU";
    case "controlBoard":
      return "Control Board";
  }
};

/**
 * Generates a readable error title from error code
 */
const getErrorTitle = (error: NotificationError): string => {
  if (!error.error_code) {
    return "Unknown error";
  }

  // Convert camelCase or PascalCase to readable format
  return error.error_code
    .replace(/([A-Z])/g, " $1")
    .trim()
    .replace(/^./, (str) => str.toUpperCase());
};

/**
 * Transforms a NotificationError into a ComponentError
 */
export const transformNotificationError = (
  error: NotificationError,
  index: number,
): ComponentError => {
  const componentType = getComponentType(error);
  const componentName = getComponentName(error, componentType);
  const title = getErrorTitle(error);

  return {
    id: `${componentType}-${index}-${error.inserted_at || Date.now()}`,
    componentType,
    componentName,
    title,
    message: error.message || "An error occurred",
    timestamp: error.inserted_at,
    details: error.details,
    notificationError: error,
  };
};

/**
 * Transforms an array of NotificationErrors into ComponentErrors
 */
export const transformNotificationErrors = (
  errors: NotificationError[],
): ComponentError[] => {
  return errors.map((error, index) => transformNotificationError(error, index));
};
