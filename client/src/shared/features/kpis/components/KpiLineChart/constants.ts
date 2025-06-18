import { CurveType } from "recharts/types/shape/Curve";

export const lineProps = {
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

export const nullLineProps = {
  connectNulls: true,
  strokeDasharray: "1 5",
};

export const lineColors = ["#00A4FB", "#38A600", "#783EED"];

export const hashboardColors = [
  {
    text: "--color-intent-info-text",
    colors: [
      "--color-intent-info-fill",
      "--color-intent-info-80",
      "--color-intent-info-60",
    ],
  },
  {
    text: "--color-core-indigo-text",
    colors: [
      "--color-core-indigo-fill",
      "--color-core-indigo-80",
      "--color-core-indigo-60",
    ],
  },
  {
    text: "--color-intent-success-text",
    colors: [
      "--color-intent-success-fill",
      "--color-intent-success-80",
      "--color-intent-success-60",
    ],
  },
];
