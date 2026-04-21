import type { ReactNode } from "react";

export interface SegmentedBarChartData {
  datetime: number; // Unix timestamp
  [key: string]: number; // Dynamic segment keys
}

export interface ResponsiveValue<T> {
  phone?: T;
  tablet?: T;
  laptop?: T;
  desktop?: T;
}

export interface SegmentConfig {
  [key: string]: {
    color: string;
    label: string;
    icon?: ReactNode;
  };
}

export interface SegmentedBarChartProps {
  chartData: SegmentedBarChartData[] | null;
  segmentKeys: string[];
  colorMap?: { [key: string]: string };
  units?: string | { singular: string; plural: string };
  percentageDisplay?: boolean;
  className?: string;
  height?: number;
  barWidth?: number | ResponsiveValue<number>;
  barGap?: number | ResponsiveValue<number>; // Gap between bars in pixels, if omitted bars will be evenly spaced to fill the container
  yAxisTickCount?: number; // Number of horizontal grid lines/ticks (default: 3)
  xAxisTickInterval?: number; // Show tick every N bars (default: 1 = show all)
  showDateLabel?: boolean; // Show a single centered date label below all bars (for multi-day per-day charts)
  useDateFormat?: boolean; // Use date format (e.g., "1/15") instead of time for x-axis tick labels
  lastTickOverride?: string; // Custom text for the last tick (e.g., "live")
  segmentConfig?: SegmentConfig; // Optional segment configuration for enhanced tooltip display
}
