/**
 * Helper functions for status summary computation
 */

import type { GroupedStatusErrors, StatusComponentType } from "./types";

/**
 * Display names for component types
 */
const COMPONENT_DISPLAY_NAMES: Record<StatusComponentType, { singular: string; capitalized: string }> = {
  hashboard: { singular: "hashboard", capitalized: "Hashboard" },
  psu: { singular: "PSU", capitalized: "PSU" },
  fan: { singular: "fan", capitalized: "Fan" },
  controlBoard: { singular: "control board", capitalized: "Control board" },
};

/**
 * Get display name for a component type with optional index
 * @param componentType - The component type
 * @param componentIndex - Optional 0-based index (will be displayed as 1-based)
 * @returns Formatted name like "Hashboard 1", "PSU 2", "Control board"
 */
export function getComponentDisplayName(componentType: StatusComponentType, componentIndex?: number): string {
  const { capitalized } = COMPONENT_DISPLAY_NAMES[componentType];

  if (componentIndex !== undefined) {
    return `${capitalized} ${componentIndex + 1}`;
  }

  return capitalized;
}

/**
 * Get the singular lowercase form of a component name for use in sentences
 * @param componentType - The component type
 * @returns Singular lowercase name like "hashboard", "PSU", "control board"
 */
export function getComponentSingularName(componentType: StatusComponentType): string {
  return COMPONENT_DISPLAY_NAMES[componentType].singular;
}

/**
 * Pure function to compute component status title
 *
 * Used by useComponentStatusTitle hook and can be called directly from non-hook functions.
 *
 * @param componentType - The component type being viewed
 * @param componentIndex - The specific component index (0-based)
 * @param errorCount - Number of errors for this specific component
 * @returns Title string or null (null means don't render title, show error instead)
 */
export function computeComponentStatusTitle(
  componentType: StatusComponentType,
  componentIndex: number | undefined,
  errorCount: number,
): string | null {
  // No errors
  if (errorCount === 0) {
    return "All systems are operational";
  }

  // Single error - return null to indicate the UI should show the error message instead
  if (errorCount === 1) {
    return null;
  }

  // Multiple errors
  const displayName = getComponentDisplayName(componentType, componentIndex);
  return `${displayName} has multiple issues`;
}

/**
 * Analyze grouped errors to determine error distribution
 */
export function analyzeErrors(groupedErrors: GroupedStatusErrors): {
  componentTypesWithErrors: Array<{
    type: StatusComponentType;
    errors: GroupedStatusErrors[StatusComponentType];
  }>;
} {
  const componentTypesWithErrors: Array<{
    type: StatusComponentType;
    errors: GroupedStatusErrors[StatusComponentType];
  }> = [];

  for (const [type, errors] of Object.entries(groupedErrors)) {
    if (errors.length > 0) {
      componentTypesWithErrors.push({
        type: type as StatusComponentType,
        errors,
      });
    }
  }

  return { componentTypesWithErrors };
}
