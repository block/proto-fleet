export interface SegmentedBarChartData {
  datetime: number; // Unix timestamp
  [key: string]: number; // Dynamic segment keys
}

export interface SegmentedBarChartProps {
  chartData: SegmentedBarChartData[] | null;
  segmentKeys: string[];
  colorMap?: { [key: string]: string };
  units?: string;
  percentageDisplay?: boolean;
  segmentsLabel?: string;
  showTooltip?: boolean;
  animate?: boolean;
  className?: string;
  height?: number;
  barWidth?: number;
  yAxisPadding?: number; // Percentage to extend Y-axis above max value (e.g., 0.1 = 10%)
  yAxisTickCount?: number; // Number of horizontal grid lines/ticks (default: 3)
  xAxisTickInterval?: number; // Show tick every N bars (default: 1 = show all)
  toolTipKey?: string | null; // Key to display in tooltip, null to hide tooltip, undefined for total
}

export interface SegmentedBarTooltipProps {
  active?: boolean;
  payload?: any; // Recharts default payload (we won't use this)
  customPayload?: {
    datetime: number;
    total: number;
    segments: Array<{
      key: string;
      value: number;
      color: string;
    }>;
  };
  units?: string;
  percentageDisplay?: boolean;
  barPosition?: { x: number; y: number; index: number };
  toolTipKey?: string | null;
}
