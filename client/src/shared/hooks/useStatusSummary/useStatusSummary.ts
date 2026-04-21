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
 * Core miner status types (used for Status column in ProtoFleet)
 */
export type MinerStatus = "Hashing" | "Offline" | "Sleeping" | "Needs attention";

/**
 * Issues summary result (used for Issues column in ProtoFleet)
 */
export interface MinerIssues {
  summary: string | null;
  hasIssues: boolean;
}

/**
 * Computes miner status (one of 4 core statuses)
 *
 * Priority:
 * 1. isOffline → "Offline"
 * 2. isSleeping → "Sleeping"
 * 3. needsAttention → "Needs attention"
 * 4. default → "Hashing"
 *
 * Note: isSleeping is already filtered at call site to exclude auth-needed devices
 *
 * @param isOffline - Device is offline/unreachable
 * @param isSleeping - Device is sleeping or in maintenance mode (excludes auth-needed)
 * @param needsAttention - Device has issues (auth, pool, or hardware errors)
 * @returns One of the 4 core miner statuses
 */
export function useMinerStatus(isOffline: boolean, isSleeping: boolean, needsAttention: boolean): MinerStatus {
  return useMemo(() => {
    if (isOffline) return "Offline";
    if (isSleeping) return "Sleeping";
    if (needsAttention) return "Needs attention";
    return "Hashing";
  }, [isOffline, isSleeping, needsAttention]);
}

/**
 * Computes miner issues summary (for Issues column in ProtoFleet)
 *
 * Shows the highest priority issue for a miner.
 * Priority (matches status modal priority):
 * 1. Authentication required (must be resolved first)
 * 2. Pool configuration required (only relevant after authentication)
 * 3. Hardware errors (only if no auth/pool issues):
 *    - Multiple component types → "Multiple failures"
 *    - Multiple errors on one component → "Multiple [component] failures"
 *    - Single error → "[Component] failure"
 *
 * Note: Authentication is prioritized over pool because you must authenticate
 * before you can configure a mining pool. This matches the behavior of useMinerStatusSummary.
 *
 * @param needsAuthentication - Device needs authentication
 * @param needsMiningPool - Device needs mining pool configured
 * @param groupedErrors - Hardware errors grouped by component type
 * @returns MinerIssues object with summary text and hasIssues flag
 */
export function useMinerIssues(
  needsAuthentication: boolean,
  needsMiningPool: boolean,
  groupedErrors: GroupedStatusErrors,
  isUpdating: boolean = false,
  isRebootRequired: boolean = false,
): MinerIssues {
  return useMemo(() => {
    // Prioritize authentication over everything else
    if (needsAuthentication) {
      return { summary: "Authentication required", hasIssues: true };
    }

    // Then check for pool configuration
    if (needsMiningPool) {
      return { summary: "Pool required", hasIssues: true };
    }

    // Firmware update states
    if (isUpdating) {
      return { summary: "Updating firmware", hasIssues: true };
    }
    if (isRebootRequired) {
      return { summary: "Reboot required", hasIssues: true };
    }

    // Finally, check for hardware errors
    const { componentTypesWithErrors } = analyzeErrors(groupedErrors);
    if (componentTypesWithErrors.length > 0) {
      const errorTitle = computeErrorTitle(componentTypesWithErrors);
      return { summary: errorTitle, hasIssues: true };
    }

    // No issues
    return { summary: null, hasIssues: false };
  }, [needsAuthentication, needsMiningPool, groupedErrors, isUpdating, isRebootRequired]);
}

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
      title = "Miner is offline";
      if (hasErrors) {
        subtitle = computeErrorTitle(componentTypesWithErrors);
      }
    } else if (needsAuthentication) {
      title = "Authentication required";
    } else if (needsMiningPool) {
      title = "Mining pool required";
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
    return "Multiple failures";
  }

  // Single component type has errors
  const { type, errors } = componentTypesWithErrors[0];

  // "other" type shows "1 issue" / "N issues" instead of component name
  if (type === "other") {
    const count = errors.length;
    return count === 1 ? "1 issue" : `${count} issues`;
  }

  // Multiple errors on this component type
  if (errors.length > 1) {
    const singularName = getComponentSingularName(type);
    return `Multiple ${singularName} failures`;
  }

  // Single error - show specific component
  const error = errors[0];
  const displayName = getComponentDisplayName(type, error.slot);
  return `${displayName} failure`;
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
