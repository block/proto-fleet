import { useMemo } from "react";
import { genericStatusTitles, singleErrorStatusTitles } from "./constants";
import { useGroupedErrors, useMinerStore } from "@/protoOS/store";
import type { ErrorSource } from "@/protoOS/store/types";

// Local display names for components
const COMPONENT_DISPLAY_NAMES = {
  singular: {
    hashboard: "hashboard",
    psu: "PSU",
    fan: "fan",
    pool: "pool",
    system: "control board",
  },
};

/**
 * Returns title and subtitle describing the current miner status
 * @returns Object with title and subtitle strings
 */
export const useMinerStatusTitle = (): { title: string; subtitle?: string } => {
  const errors = useMinerStore((state) => state.minerStatus.errors.errors);
  const groupedErrors = useGroupedErrors();

  return useMemo(() => {
    // Check for any errors (treat all equally)
    // Note: MinerStatusModalContent will handle showing "Miner is asleep" title when isSleeping is true
    if (errors.length === 0) {
      return {
        title: "All systems are operational",
      };
    }

    // Count how many different components have errors
    const componentsWithErrors = Object.values(groupedErrors).filter((arr) => arr.length > 0).length;

    // Multiple components have issues
    if (componentsWithErrors > 1) {
      return {
        title: "Multiple issues detected",
        subtitle: "Repair now to prevent downtime",
      };
    }

    // Single component type has issues
    if (componentsWithErrors === 1) {
      // Find which component has issues
      const componentType = Object.keys(groupedErrors).find(
        (key) => groupedErrors[key as keyof typeof groupedErrors].length > 0,
      ) as keyof typeof groupedErrors;

      const componentErrors = groupedErrors[componentType];

      // Multiple issues on same component type
      if (componentErrors.length > 1) {
        const componentName =
          COMPONENT_DISPLAY_NAMES.singular[componentType as keyof typeof COMPONENT_DISPLAY_NAMES.singular];

        return {
          title: `Multiple ${componentName} issues detected`,
          subtitle: "Repair now to prevent downtime",
        };
      }

      // Single issue - provide specific message based on error code
      const error = componentErrors[0];
      const specificMessage = singleErrorStatusTitles[error.errorCode];
      if (specificMessage) {
        return specificMessage;
      }

      // Fallback to generic message based on source
      return (
        genericStatusTitles[error.source as ErrorSource] || {
          title: error.message,
          subtitle: "Check diagnostics for details",
        }
      );
    }

    // Fallback (shouldn't reach here)
    return {
      title: "Your miner is not functioning properly",
      subtitle: "Check diagnostics for details",
    };
  }, [errors, groupedErrors]);
};
