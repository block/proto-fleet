/**
 * Shared hooks for status summary computation
 *
 * These hooks accept normalized error data as parameters (not tied to any specific store).
 * Each app transforms its errors to the shared format before calling these hooks.
 */

import { useMemo } from "react";
import type { ComponentStatusSummary, GroupedStatusErrors, MinerStatusSummary, StatusComponentType } from "./types";
import { analyzeErrors, computeComponentStatusTitle, getComponentDisplayName, getComponentSingularName } from "./utils";

/**
 * Computes the complete miner status summary
 *
 * Returns a unified Summary object with condensed, title, and subtitle fields.
 *
 * Priority for condensed (when no errors):
 * 1. isOffline → "Offline"
 * 2. needsAuthentication → "Needs Authentication"
 * 3. isSleeping → "Sleeping"
 * 4. needsMiningPool → "Needs mining pool"
 * 5. hasErrors → error title
 * 6. default → "Hashing"
 *
 * @param groupedErrors - Errors grouped by component type
 * @param isSleeping - Miner is intentionally sleeping/stopped
 * @param isOffline - Miner is offline/unreachable (defaults to false)
 * @param needsAuthentication - Miner needs authentication (defaults to false)
 * @param needsMiningPool - Miner needs a mining pool configured (defaults to false)
 * @returns Memoized MinerStatusSummary object
 *
 * @example
 * // ProtoOS - always online, just check sleeping
 * const summary = useMinerStatusSummary(groupedErrors, false);
 * // { condensed: "Hashing", title: "All systems are operational", subtitle: undefined }
 *
 * // ProtoFleet - check offline and sleeping
 * const summary = useMinerStatusSummary(groupedErrors, false, true);
 * // { condensed: "Offline", title: "All systems are operational", subtitle: undefined }
 */
export function useMinerStatusSummary(
  groupedErrors: GroupedStatusErrors,
  isSleeping: boolean = false,
  isOffline: boolean = false,
  needsAuthentication: boolean = false,
  needsMiningPool: boolean = false,
): MinerStatusSummary {
  return useMemo(() => {
    const { componentTypesWithErrors } = analyzeErrors(groupedErrors);
    const hasErrors = componentTypesWithErrors.length > 0;

    // Compute title based on errors and connection status
    let title: string;
    let subtitle: string | undefined;
    if (isOffline) {
      title = "Device is offline";
      if (hasErrors) {
        subtitle = computeErrorTitle(componentTypesWithErrors);
      }
    } else if (hasErrors) {
      title = computeErrorTitle(componentTypesWithErrors);
    } else {
      title = "All systems are operational";
    }

    // Compute condensed: priority is offline → needsAuth → sleeping → needsMiningPool → errors → hashing
    let condensed: string;
    if (isOffline) {
      condensed = "Offline";
    } else if (needsAuthentication) {
      condensed = "Needs Authentication";
    } else if (isSleeping) {
      condensed = "Sleeping";
    } else if (needsMiningPool) {
      condensed = "Needs mining pool";
    } else if (hasErrors) {
      condensed = computeErrorTitle(componentTypesWithErrors);
    } else {
      condensed = "Hashing";
    }

    return {
      condensed,
      title,
      subtitle,
    };
  }, [groupedErrors, isSleeping, isOffline, needsAuthentication, needsMiningPool]);
}

/**
 * Helper to compute error title from analyzed errors
 */
function computeErrorTitle(
  componentTypesWithErrors: Array<{
    type: StatusComponentType;
    errors: GroupedStatusErrors[StatusComponentType];
  }>,
): string {
  // Multiple component types have errors
  if (componentTypesWithErrors.length > 1) {
    return "Multiple issues";
  }

  // Single component type has errors
  const { type, errors } = componentTypesWithErrors[0];

  // Multiple errors on this component type
  if (errors.length > 1) {
    const singularName = getComponentSingularName(type);
    return `Multiple ${singularName} issues`;
  }

  // Single error - show specific component
  const error = errors[0];
  const displayName = getComponentDisplayName(type, error.slot);
  return `${displayName} issue`;
}

/**
 * Computes the complete component status summary
 *
 * Returns a unified Summary object with title and subtitle fields.
 *
 * @param componentType - The component type being viewed
 * @param slot - The specific component slot (1-based)
 * @param errorCount - Number of errors for this specific component
 * @returns Memoized ComponentStatusSummary object
 *
 * @example
 * const summary = useComponentStatusSummary("hashboard", 1, 0);
 * // { title: "All systems are operational", subtitle: undefined }
 *
 * const summary = useComponentStatusSummary("hashboard", 1, 1);
 * // { title: null, subtitle: undefined } // null = show error message instead
 *
 * const summary = useComponentStatusSummary("hashboard", 1, 3);
 * // { title: "Hashboard 1 has multiple issues", subtitle: undefined }
 */
export function useComponentStatusSummary(
  componentType: StatusComponentType,
  slot: number | undefined,
  errorCount: number,
): ComponentStatusSummary {
  return useMemo(
    () => ({
      title: computeComponentStatusTitle(componentType, slot, errorCount),
      subtitle: undefined,
    }),
    [componentType, slot, errorCount],
  );
}
