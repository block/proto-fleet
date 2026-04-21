import { useMemo } from "react";
import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";
import SharedLineChart, { type LineChartProps as SharedLineChartProps } from "@/shared/components/LineChart";

export type LineChartProps = SharedLineChartProps & {
  heightClass?: string;
  duration?: FleetDuration;
};

const LineChart = ({ heightClass = "h-100", duration, ...props }: LineChartProps) => {
  const { chartData } = props;

  const xAxisDomainOverride = useMemo((): [number, number] | undefined => {
    if (!duration || !chartData?.length) return undefined;
    const durationMs = getFleetDurationMs(duration);
    const startTime = chartData[0].datetime;
    return [startTime, startTime + durationMs];
  }, [duration, chartData]);

  const fleetProps = {
    ...props,
    xAxisDomainOverride,
    connectNulls: true,
    yAxisTickYOffset: -8, // Move labels up to position above grid lines
    visibleTickIndices: [0, 2, 4], // Show labels on lines 1, 3, and 5
    chartMarginTop: 20, // Add top margin to prevent label cutoff
    xAxisLabelCount: 4, // Show 4 timestamp positions (last one will be empty)
  };

  return (
    <div className={`flex w-full *:min-h-0! ${heightClass} [&_.recharts-cartesian-axis-line]:hidden`}>
      <SharedLineChart {...fleetProps} />
    </div>
  );
};

export default LineChart;
