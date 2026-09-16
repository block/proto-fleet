import { useMemo } from "react";

import useCssVariable from "@/shared/hooks/useCssVariable";

export interface AsicPalette {
  cool: string;
  success: string;
  warning: string;
  danger: string;
  unknown: string;
}

/**
 * The theme colours an ASIC heatmap is drawn from.
 *
 * Read this once per grid rather than once per cell: each `useCssVariable` runs
 * `getComputedStyle` on mount, so resolving the palette inside several hundred
 * ASIC cells forces a corresponding number of style recalculations.
 */
export const useAsicPalette = (): AsicPalette => {
  const cool = useCssVariable("--color-intent-info-fill");
  const success = useCssVariable("--color-intent-success-fill");
  const warning = useCssVariable("--color-intent-warning-fill");
  const danger = useCssVariable("--color-intent-critical-fill");
  const unknown = useCssVariable("--color-core-primary-2");

  return useMemo(() => ({ cool, success, warning, danger, unknown }), [cool, success, warning, danger, unknown]);
};
