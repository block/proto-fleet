import { getRandomFloat } from "common/utils/utility";

import { times } from "components/Chart/constants";

import { CurveType } from "recharts/types/shape/Curve";

export const getChartData = () => {
  const chartData = times.map((time) => {
    const hashrate2 = getRandomFloat(30, 40);
    const hashrate1 = getRandomFloat(30, 40);
    const hashrate3 = getRandomFloat(30, 40);
    const avg = +((hashrate1 + hashrate2 + hashrate3) / 3).toFixed(2);
    return {
      avgHashrate: avg,
      hashrate1,
      hashrate2,
      hashrate3,
      time,
    };
  });

  return chartData;
};

export const LineProps = {
  type: "basis" as CurveType,
  strokeOpacity: 1,
  strokeWidth: 2.5,
  label: false,
  dot: false,
  strokeLinecap: "round" as "round" | "inherit" | "butt" | "square" | undefined,
  strokeLinejoin: "round" as
    | "round"
    | "inherit"
    | "miter"
    | "bevel"
    | undefined,
  activeDot: false,
  isAnimationActive: true,
};
