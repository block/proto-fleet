import { useMemo } from "react";

import { criticalTemp, dangerTemp, warningTemp } from "../constants";
import { AsicStats } from "@/protoOS/api/types";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { map } from "@/shared/utils/math";

const useAsicColor = (asic: AsicStats) => {
  const warningColor = useCssVariable("--color-intent-warning-fill");
  const dangerColor = useCssVariable("--color-intent-critical-fill");
  const defaultColor = useCssVariable("--color-core-primary-2");

  const backgroundColor = useMemo(() => {
    if (asic?.temp_c === undefined || asic.temp_c < warningTemp) {
      return defaultColor;
    }

    const opacity = map(asic.temp_c, warningTemp, criticalTemp, 0.15, 1.0);
    const color = asic.temp_c >= dangerTemp ? dangerColor : warningColor;
    return color.replace(")", `/ ${opacity})`);
  }, [asic.temp_c, dangerColor, warningColor, defaultColor]);

  return backgroundColor;
};

export default useAsicColor;
