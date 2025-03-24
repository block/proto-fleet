import { useMemo } from "react";

import { criticalTemp, dangerTemp, warningTemp } from "../constants";
import type { AsicStats } from "@/protoOS/api/types";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { map } from "@/shared/utils/math";

const AsicCell = ({ asic }: { asic: AsicStats }) => {
  const warningColor = useCssVariable({
    variable: "--color-intent-warning-fill",
  });
  const dangerColor = useCssVariable({
    variable: "--color-intent-critical-fill",
  });
  const defaultColor = useCssVariable({
    variable: "--color-core-primary-2",
  });

  const backgroundColor = useMemo(() => {
    if (asic?.temp_c === undefined || asic.temp_c < warningTemp) {
      return defaultColor;
    }

    const opacity = map(asic.temp_c, warningTemp, criticalTemp, 0.15, 1.0);
    const color = asic.temp_c >= dangerTemp ? dangerColor : warningColor;
    return color.replace(")", `/ ${opacity})`);
  }, [asic.temp_c, dangerColor, warningColor, defaultColor]);

  return (
    <div
      style={{ backgroundColor }}
      className="relative h-4 grow basis-0 rounded-xl border-1 border-core-primary-5"
    />
  );
};

export default AsicCell;
