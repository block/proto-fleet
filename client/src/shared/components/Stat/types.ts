import { type ReactNode } from "react";
import { ChartStatus } from "@/shared/components/Stat/constants";

export type StatProps = {
  label: string;
  value?: number | string;
  text?: ReactNode;
  subtitle?: string;
  tooltipContent?: string;
  units?: string;
  icon?: ReactNode;
  headingLevel?: number;
  size: "small" | "medium" | "large";
  chartPercentage?: number;
  chartStatus?: ChartStatus;
  valueClassName?: string;
};
