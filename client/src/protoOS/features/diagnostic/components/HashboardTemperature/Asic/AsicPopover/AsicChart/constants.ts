import { CurveType } from "recharts/types/shape/Curve";

const lineProps = {
  type: "monotone" as CurveType,
  strokeWidth: 2.5,
  label: false,
  dot: false,
  strokeLinecap: "round" as "round" | "inherit" | "butt" | "square" | undefined,
  strokeLinejoin: "round" as "round" | "inherit" | "miter" | "bevel" | undefined,
  isAnimationActive: false,
};

export const hashrateLineProps = {
  ...lineProps,
  dataKey: "hashrate_ghs",
  className: "text-core-primary-fill",
  stroke: "currentColor",
};

export const temperatureLineProps = {
  ...lineProps,
  dataKey: "temp_c",
  className: "text-core-accent-fill",
  stroke: "currentColor",
};

export const nullLineProps = {
  connectNulls: true,
  strokeDasharray: "1 5",
};
