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
  connectNulls: false,
};

export const nullLineProps = {
  connectNulls: true,
  strokeDasharray: "1 5",
};

export const lineColors = ["#00A4FB", "#38A600", "#783EED"];
export const defaultHashboardColor = "--color-text-primary-50";
export const hashboardColors = [
  "--color-extended-sky-fill",
  "--color-extended-taupe-fill",
  "--color-extended-dark-red-fill",
  "--color-core-primary-fill",
  "--color-extended-pink-fill",
  "--color-extended-purple-fill",
  "--color-extended-forest-fill",
  "--color-extended-teal-fill",
  "--color-extended-navy-fill",
];
