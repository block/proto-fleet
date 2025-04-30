import { type ReactNode } from "react";
import { ChartStatus } from "@/shared/components/Stat/constants";

export type StatProps = {
  label: string;
  value?: number | string;
  text?: string;
  units?: string;
  icon?: ReactNode;
  headingLevel?: number;
  size: "small" | "medium" | "large";
  chartPercentage?: number;
  chartStatus?: ChartStatus;
};
