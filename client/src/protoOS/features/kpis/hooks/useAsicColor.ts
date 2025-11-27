import { useMemo } from "react";

import { criticalTemp, dangerTemp, warningTemp } from "../constants";
import { type AsicData } from "@/protoOS/store";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { map } from "@/shared/utils/math";

const useAsicColor = (asic: AsicData) => {
  const warningColor = useCssVariable("--color-intent-warning-fill");
  const dangerColor = useCssVariable("--color-intent-critical-fill");
  const coolColor = useCssVariable("--color-intent-info-fill");
  const defaultColor = useCssVariable("--color-core-primary-2");

  const backgroundColor = useMemo(() => {
    const currentTemp = asic?.temperature?.latest?.value;

    if (currentTemp === undefined || currentTemp === null) {
      return defaultColor;
    }

    let opacity =
      currentTemp >= criticalTemp
        ? 1.0
        : currentTemp >= warningTemp
          ? map(currentTemp, warningTemp, criticalTemp, 0.4, 1)
          : map(currentTemp, 30, warningTemp, 0.4, 0.05);

    // round opacity to nearest 0.05
    opacity = Math.round(opacity * 20) / 20;

    const color = currentTemp >= dangerTemp ? dangerColor : currentTemp >= warningTemp ? warningColor : coolColor;

    return color.replace(")", `/ ${opacity})`);
  }, [asic?.temperature, dangerColor, warningColor, defaultColor, coolColor]);

  return backgroundColor;
};

export default useAsicColor;
