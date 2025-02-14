import { CurveType } from "recharts/types/shape/Curve";

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
