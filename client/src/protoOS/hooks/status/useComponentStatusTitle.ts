import { useMemo } from "react";
import { getComponentDisplayName } from "./useComponentDisplayName";
import { useErrorsByComponent } from "@/protoOS/store";
import type { ErrorSource } from "@/protoOS/store/types";

/**
 * Returns title and subtitle for a specific component
 * @param source - The error source
 * @param componentIndex - The component index
 * @returns Object with title and subtitle strings
 */
export const useComponentStatusTitle = (
  source: ErrorSource,
  componentIndex: number,
): { title: string; subtitle?: string } => {
  const errors = useErrorsByComponent(source, componentIndex);

  return useMemo(() => {
    if (errors.length === 0) {
      const componentName = getComponentDisplayName(source, componentIndex);
      return {
        title: `${componentName} is operating normally`,
      };
    }

    // Treat all errors equally
    return {
      title: errors[0].message,
      subtitle:
        errors.length > 1
          ? `Plus ${errors.length - 1} other ${errors.length - 1 === 1 ? "issue" : "issues"}`
          : undefined,
    };
  }, [errors, source, componentIndex]);
};
