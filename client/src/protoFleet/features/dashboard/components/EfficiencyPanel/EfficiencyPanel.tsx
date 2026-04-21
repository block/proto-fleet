import { useMemo } from "react";
import { transformEfficiencyMetricsToChartData } from "./utils";
import { type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { padChartDataWithNulls } from "@/protoFleet/features/dashboard/utils/chartDataPadding";
import { getMinerCountSubtitle } from "@/protoFleet/features/dashboard/utils/minerCountSubtitle";
import { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface EfficiencyPanelProps {
  duration: FleetDuration;
  /** Efficiency metrics — undefined = not loaded yet, empty array = loaded but no data */
  metrics: Metric[] | undefined;
  /** Total miner count for "X of Y miners reporting" subtitle */
  totalMiners: number;
}

export function EfficiencyPanel({ duration, metrics, totalMiners }: EfficiencyPanelProps) {
  // Transform metrics data to chart format (merging already done by store selectors)
  const chartData = useMemo(() => {
    if (metrics === undefined) return undefined; // Not loaded yet
    if (metrics.length === 0) return null; // Loaded but no data

    const transformedData = transformEfficiencyMetricsToChartData(metrics);

    // Pad with null values for the full duration
    return padChartDataWithNulls(transformedData, duration);
  }, [metrics, duration]);

  // Get the latest efficiency value for the stat display
  const currentEfficiency = useMemo(() => {
    if (chartData === undefined) return undefined; // Not loaded yet
    if (chartData === null || chartData.length === 0) return null; // Loaded but no data
    return chartData[chartData.length - 1]?.efficiency ?? null;
  }, [chartData]);

  // Use max device count across all buckets — the last bucket may be incomplete
  // and fluctuate as new data arrives.
  const deviceCount = useMemo(() => {
    if (metrics === undefined) return undefined;
    if (metrics.length === 0) return null;
    return Math.max(...metrics.map((m) => m.deviceCount));
  }, [metrics]);

  // Show loading skeleton while data hasn't loaded yet
  if (metrics === undefined) {
    const stat = {
      label: "Efficiency",
      value: undefined,
      units: "",
    };

    return (
      <ChartWidget stats={stat}>
        <SkeletonBar className="h-60 w-full" />
      </ChartWidget>
    );
  }

  // Handle no data case - still show the widget with header but no chart
  if (!chartData || chartData.length === 0) {
    const stat = {
      label: "Efficiency",
      value: "No data",
      units: "",
    };

    return <ChartWidget stats={stat}>{null}</ChartWidget>;
  }

  const efficiencyDisplayValue =
    currentEfficiency !== null && currentEfficiency !== undefined ? currentEfficiency.toFixed(1) : "N/A";

  const subtitle = getMinerCountSubtitle(deviceCount ?? null, totalMiners);
  const stat = {
    label: "Efficiency",
    value: efficiencyDisplayValue,
    units: "J/TH",
    subtitle,
    tooltipContent: subtitle ? "Some devices do not make this data available to Proto Fleet." : undefined,
  };

  return (
    <ChartWidget stats={stat}>
      <LineChart
        chartData={chartData}
        aggregateKey="efficiency"
        units="J/TH"
        activeKeys={["efficiency"]}
        heightClass="h-60"
        tickCount={5}
        duration={duration}
      />
    </ChartWidget>
  );
}
