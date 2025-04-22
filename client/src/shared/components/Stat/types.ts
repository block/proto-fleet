import { type ReactNode } from "react";

export type StatProps = {
  label: string;
  value?: number | string;
  text?: string;
  units?: string;
  icon?: ReactNode;
  headingLevel?: number;
  size: "small" | "medium" | "large";
  chartPercentage?: number;
  chartStatus?: "neutral" | "warning" | "critical" | "success";
};
