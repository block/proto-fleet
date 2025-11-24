import type { ReactNode } from "react";
import type { TemperatureStatusCount } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { ButtonVariant } from "@/shared/components/Button";
import type { Duration } from "@/shared/components/DurationSelector";

export interface SegmentedBarChartData {
  datetime: number;
  [key: string]: number;
}

export interface SegmentConfig {
  [key: string]: {
    color: string;
    label: string;
    icon?: ReactNode;
    displayInBreakdown?: boolean;
    index?: number; // Controls the order segments appear in the breakdown
    buttonVariant?: ButtonVariant; // Button variant for the segment
    percentageLabel?: string; // Custom label to show instead of "n% of miners"
  };
}

export interface SegmentedMetricPanelProps {
  title: string;
  headline?: string; // Optional static headline
  headlineGenerator?: (processedData: SegmentedBarChartData[][]) => string; // Optional dynamic headline generator
  chartData: TemperatureStatusCount[];
  segmentConfig: SegmentConfig;
  duration: Duration;
  className?: string;
}
