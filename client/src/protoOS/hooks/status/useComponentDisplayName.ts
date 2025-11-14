import { ErrorSource } from "@/protoOS/store/types";

/**
 * Get the display name for a component based on its source and index.
 * This is the single source of truth for component naming in the UI.
 *
 * @param source - The error source type (ASIC is already transformed to HASHBOARD)
 * @param componentIndex - The component index (0-based)
 * @returns The formatted display name
 */
export function getComponentDisplayName(
  source: ErrorSource,
  componentIndex?: number,
): string {
  // Simple direct mapping of source to display names
  const displayNames: Record<ErrorSource, string> = {
    PSU: "Power supply",
    FAN: "Fan",
    HASHBOARD: "Hashboard",
    ASIC: "Hashboard", // Kept for type completeness, but ASIC is transformed to HASHBOARD upstream
    SYSTEM: "Control board",
    POOL: "Pool",
  };

  const baseName = displayNames[source];

  // For components with an index (all indices are 0-based)
  if (componentIndex !== undefined) {
    return `${baseName} ${componentIndex + 1}`;
  }

  // For components without indices (e.g., SYSTEM/Control board, or when index is undefined)
  return baseName;
}

/**
 * Hook to get component display name.
 * This is a convenience wrapper around getComponentDisplayName for use in React components.
 */
export function useComponentDisplayName(
  source: ErrorSource,
  componentIndex?: number,
): string {
  return getComponentDisplayName(source, componentIndex);
}
