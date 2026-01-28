import { ErrorSource } from "@/protoOS/store/types";

/**
 * Get the display name for a component based on its error source and slot.
 * Used for StatusModal and error-related component naming in ProtoOS.
 *
 * @param source - The error source type
 * @param slot - The 1-based component slot
 * @returns The formatted display name
 */
export function getComponentDisplayName(source: ErrorSource, slot?: number): string {
  const displayNames: Record<ErrorSource, string> = {
    RIG: "System",
    PSU: "Power supply",
    FAN: "Fan",
    HASHBOARD: "Hashboard",
  };

  const baseName = displayNames[source];

  if (slot !== undefined) {
    return `${baseName} ${slot}`;
  }

  return baseName;
}

/**
 * Hook to get component display name.
 * This is a convenience wrapper around getComponentDisplayName for use in React components.
 *
 * @param source - The error source type
 * @param slot - The 1-based component slot
 */
export function useComponentDisplayName(source: ErrorSource, slot?: number): string {
  return getComponentDisplayName(source, slot);
}
