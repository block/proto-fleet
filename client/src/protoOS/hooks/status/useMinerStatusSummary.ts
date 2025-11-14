import { useMemo } from "react";
import { useGroupedErrors, useMinerStore } from "@/protoOS/store";

// Display names for components
const COMPONENT_DISPLAY_NAMES = {
  singular: {
    hashboard: "hashboard",
    psu: "PSU",
    fan: "fan",
    pool: "pool",
    system: "control board",
  },
  capitalized: {
    hashboard: "Hashboard",
    psu: "Power supply",
    fan: "Fan",
    pool: "Pool",
    system: "Control board",
  },
};

/**
 * Generates a holistic status summary based on errors and mining status
 * @returns Status summary text like "Hashing", "Sleeping", "Fan issue", etc.
 */
export const useMinerStatusSummary = (): string => {
  const miningStatus = useMinerStore((state) => state.minerStatus.miningStatus);
  const groupedErrors = useGroupedErrors();
  const isSleeping = /PoweringOff|Stopped/i.test(miningStatus || "");
  const isMining = /Mining/i.test(miningStatus || "");

  return useMemo(() => {
    if (isSleeping) {
      return "Sleeping";
    }

    // Count how many different components have errors
    const componentsWithErrors = Object.entries(groupedErrors).filter(
      ([_, errs]) => errs.length > 0,
    );

    if (componentsWithErrors.length > 0) {
      // Multiple components have issues
      if (componentsWithErrors.length > 1) {
        return "Multiple issues";
      }

      // Single component type has issues
      if (componentsWithErrors.length === 1) {
        const [componentType, componentErrors] = componentsWithErrors[0];

        if (componentErrors.length > 1) {
          const componentName =
            COMPONENT_DISPLAY_NAMES.singular[
              componentType as keyof typeof COMPONENT_DISPLAY_NAMES.singular
            ];
          return `Multiple ${componentName} issues`;
        }

        // Single issue - return specific summary
        const componentName =
          COMPONENT_DISPLAY_NAMES.capitalized[
            componentType as keyof typeof COMPONENT_DISPLAY_NAMES.capitalized
          ];

        // For specific components, try to add more detail
        if (
          componentType === "hashboard" &&
          componentErrors[0].componentIndex !== undefined
        ) {
          return `Hashboard ${componentErrors[0].componentIndex + 1} issue`;
        }
        if (
          componentType === "fan" &&
          componentErrors[0].componentIndex !== undefined
        ) {
          return `Fan ${componentErrors[0].componentIndex + 1} issue`;
        }
        if (
          componentType === "psu" &&
          componentErrors[0].componentIndex !== undefined
        ) {
          return `PSU ${componentErrors[0].componentIndex + 1} issue`;
        }

        return `${componentName} issue`;
      }
    }

    if (isMining) {
      return "Hashing";
    }

    return "Idle";
  }, [groupedErrors, isSleeping, isMining]);
};
