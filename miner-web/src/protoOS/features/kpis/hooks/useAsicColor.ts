import { useMemo } from "react";

import { criticalTemp, dangerTemp, warningTemp } from "../constants";
import { AsicStats } from "@/protoOS/api/types";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { map } from "@/shared/utils/math";

const useAsicColor = (asic: AsicStats) => {
  const warningColor = useCssVariable("--color-intent-warning-fill");
  const dangerColor = useCssVariable("--color-intent-critical-fill");
  const coolColor = useCssVariable("--color-intent-info-fill");
  const defaultColor = useCssVariable("--color-core-primary-2");

  const backgroundColor = useMemo(() => {
    if (asic?.temp_c === undefined) {
      return defaultColor;
    }

    let opacity =
      asic.temp_c >= criticalTemp
        ? 1.0
        : asic.temp_c >= warningTemp
          ? map(asic.temp_c, warningTemp, criticalTemp, 0.4, 1)
          : map(asic.temp_c, 30, warningTemp, 0.4, 0.05);

    // round opacity to nearest 0.05
    opacity = Math.round(opacity * 20) / 20;

    const color =
      asic.temp_c >= dangerTemp
        ? dangerColor
        : asic.temp_c >= warningTemp
          ? warningColor
          : coolColor;

    return color.replace(")", `/ ${opacity})`);
  }, [asic.temp_c, dangerColor, warningColor, defaultColor, coolColor]);

  return backgroundColor;
};

export default useAsicColor;
