import { useMemo } from "react";
import { useErrorsByComponent } from "@/protoOS/store";
import type { ErrorSource } from "@/protoOS/store/types";
import {
  type ComponentStatusSummary,
  type StatusComponentType,
  useComponentStatusSummary as useSharedComponentStatusSummary,
} from "@/shared/hooks/useStatusSummary";

/**
 * Map ProtoOS ErrorSource to shared StatusComponentType
 */
function mapErrorSourceToComponentType(source: ErrorSource): StatusComponentType {
  const mapping: Record<ErrorSource, StatusComponentType> = {
    RIG: "controlBoard",
    HASHBOARD: "hashboard",
    PSU: "psu",
    FAN: "fan",
  };
  return mapping[source];
}

/**
 * Returns title for a specific component's status view
 * @param source - The error source type
 * @param slot - The component slot (1-based)
 * @returns Object with title (or null for single error) and optional subtitle
 *
 * Note: Returns null for title when there's 1 error - the UI should show the error message instead
 */
export const useComponentStatusTitle = (
  source: ErrorSource,
  slot: number,
): { title: string | null; subtitle?: string } => {
  const errors = useErrorsByComponent(source, slot);
  const componentType = mapErrorSourceToComponentType(source);

  const summary: ComponentStatusSummary = useSharedComponentStatusSummary(componentType, slot, errors.length);

  return useMemo(
    () => ({
      title: summary.title,
      subtitle: summary.subtitle,
    }),
    [summary.title, summary.subtitle],
  );
};
