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
  other: { singular: "needs attention", capitalized: "Needs attention" },
};

/**
 * Get display name for a component type with optional slot number
 * @param componentType - The component type
 * @param slot - Optional 1-based slot number
 * @returns Formatted name like "Hashboard 1", "PSU 2", "Control board"
 */
export function getComponentDisplayName(componentType: StatusComponentType, slot?: number): string {
  const { capitalized } = COMPONENT_DISPLAY_NAMES[componentType];

  if (slot !== undefined) {
    return `${capitalized} ${slot}`;
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
 * @param slot - The specific component slot (1-based)
 * @param errorCount - Number of errors for this specific component
 * @returns Title string or null (null means don't render title, show error instead)
 */
export function computeComponentStatusTitle(
  componentType: StatusComponentType,
  slot: number | undefined,
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
  const displayName = getComponentDisplayName(componentType, slot);

  // "other" type uses "Needs attention" without "has multiple failures" suffix
  if (componentType === "other") {
    return displayName;
  }

  return `${displayName} has multiple failures`;
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
