import { CurveType } from "recharts/types/shape/Curve";

export const lineProps = {
  type: "monotone" as CurveType,
  strokeOpacity: 1,
  strokeWidth: 2.5,
  label: false,
  dot: false,
  strokeLinecap: "round" as "round" | "inherit" | "butt" | "square" | undefined,
  strokeLinejoin: "round" as "round" | "inherit" | "miter" | "bevel" | undefined,
  activeDot: false,
  isAnimationActive: false, // Disabled due to Recharts JavascriptAnimate infinite loop bug
  connectNulls: false,
};

export const nullLineProps = {
  connectNulls: true,
  strokeDasharray: "1 5",
};
