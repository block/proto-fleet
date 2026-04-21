import type { ReactNode } from "react";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { ButtonVariant } from "@/shared/components/Button";
import type { FleetDuration } from "@/shared/components/DurationSelector";

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
    percentageLabel?: string; // Custom label to show instead of "n% of fleet"
    showButton?: boolean; // Whether to show the button with miner count (defaults to true)
    onClick?: () => void; // Optional click handler for the segment button
  };
}

// Generic status count with timestamp
// Supports Protobuf-generated types with $typeName and $unknown properties
export interface StatusCount {
  timestamp?: Timestamp;
  [key: string]: number | Timestamp | string | unknown[] | undefined;
}

export interface SegmentedMetricPanelProps {
  title: string;
  headline?: string; // Optional static headline
  headlineGenerator?: (processedData: SegmentedBarChartData[][]) => string; // Optional dynamic headline generator
  chartData: StatusCount[];
  segmentConfig: SegmentConfig;
  duration: FleetDuration;
  className?: string;
}
