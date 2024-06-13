import { CurveType } from "recharts/types/shape/Curve";

import { Hashrates } from "../types";
import { getHashrateValue } from "./utility";

interface ChartDataProps {
  hashrate1: Hashrates;
  hashrate2: Hashrates;
  hashrate3: Hashrates;
  hashrates: Hashrates;
}

export const getChartData = ({
  hashrate1,
  hashrate2,
  hashrate3,
  hashrates,
}: ChartDataProps) => {
  const chartData = hashrates?.map((hashrate) => {
    const hashrate1Value = getHashrateValue({
      datetime: hashrate.datetime,
      hashrates: hashrate1,
    });
    const hashrate2Value = getHashrateValue({
      datetime: hashrate.datetime,
      hashrates: hashrate2,
    });
    const hashrate3Value = getHashrateValue({
      datetime: hashrate.datetime,
      hashrates: hashrate3,
    });

    return {
      avgHashrate: hashrate.value,
      hashrate1: hashrate1Value,
      hashrate2: hashrate2Value,
      hashrate3: hashrate3Value,
      time: hashrate.datetime,
    };
  });

  return chartData || [];
};

export const LineProps = {
  type: "monotone" as CurveType,
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

export const NullLineProps = {
  connectNulls: true,
  strokeDasharray: "1 5",
};

const HashrateProps = {
  strokeOpacity: 1,
  isAnimationActive: false,
};

export const Hashrate1Props = {
  ...HashrateProps,
  dataKey: "hashrate1",
  stroke: "#00A4FB",
};

export const Hashrate2Props = {
  ...HashrateProps,
  dataKey: "hashrate2",
  stroke: "#38A600",
};

export const Hashrate3Props = {
  ...HashrateProps,
  dataKey: "hashrate3",
  stroke: "#783EED",
};
