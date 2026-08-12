import { useMemo } from "react";

import { criticalTemp, dangerTemp, warningTemp } from "../constants";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { map } from "@/shared/utils/math";

export interface AsicPalette {
  cool: string;
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
  const warning = useCssVariable("--color-intent-warning-fill");
  const danger = useCssVariable("--color-intent-critical-fill");
  const unknown = useCssVariable("--color-core-primary-2");

  return useMemo(() => ({ cool, warning, danger, unknown }), [cool, warning, danger, unknown]);
};

/** Heatmap colour for a single ASIC temperature, in °C. */
export const getAsicColor = (palette: AsicPalette, temperature: number | null | undefined): string => {
  if (temperature === undefined || temperature === null) {
    return palette.unknown;
  }

  let opacity =
    temperature >= criticalTemp
      ? 1.0
      : temperature >= warningTemp
        ? map(temperature, warningTemp, criticalTemp, 0.4, 1)
        : map(temperature, 30, warningTemp, 0.4, 0.05);

  // round opacity to nearest 0.05
  opacity = Math.round(opacity * 20) / 20;

  const color =
    temperature >= dangerTemp ? palette.danger : temperature >= warningTemp ? palette.warning : palette.cool;

  return color.replace(")", `/ ${opacity})`);
};
