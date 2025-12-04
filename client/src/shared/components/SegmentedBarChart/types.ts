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

export interface SegmentedBarChartProps {
  chartData: SegmentedBarChartData[] | null;
  segmentKeys: string[];
  colorMap?: { [key: string]: string };
  units?: string;
  percentageDisplay?: boolean;
  className?: string;
  height?: number;
  barWidth?: number | ResponsiveValue<number>;
  barGap?: number | ResponsiveValue<number>; // Gap between bars in pixels, if omitted bars will be evenly spaced to fill the container
  yAxisTickCount?: number; // Number of horizontal grid lines/ticks (default: 3)
  xAxisTickInterval?: number; // Show tick every N bars (default: 1 = show all)
  showDateLabel?: boolean; // Show date (e.g., "2/11") instead of time on X-axis
  lastTickOverride?: string; // Custom text for the last tick (e.g., "live")
  toolTipKey?: string | null; // Key to display in tooltip, null to hide tooltip, "total" for total value
}
