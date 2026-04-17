import { useMemo } from "react";
import { transformPowerMetricsToChartData } from "./utils";
import { type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { padChartDataWithNulls } from "@/protoFleet/features/dashboard/utils/chartDataPadding";
import { getMinerCountSubtitle } from "@/protoFleet/features/dashboard/utils/minerCountSubtitle";
import { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface PowerPanelProps {
  duration: FleetDuration;
  /** Power metrics — undefined = not loaded yet, empty array = loaded but no data */
  metrics: Metric[] | undefined;
  /** Total miner count for "X of Y miners reporting" subtitle */
  totalMiners: number;
}

export function PowerPanel({ duration, metrics, totalMiners }: PowerPanelProps) {
  // Transform metrics data to chart format (merging already done by store selectors)
  const chartData = useMemo(() => {
    if (metrics === undefined) return undefined; // Not loaded yet
    if (metrics.length === 0) return null; // Loaded but no data

    const transformedData = transformPowerMetricsToChartData(metrics);

    // Pad with null values for the full duration
    return padChartDataWithNulls(transformedData, duration);
  }, [metrics, duration]);

  // Get the latest power value for the stat display
  const currentPower = useMemo(() => {
    if (chartData === undefined) return undefined; // Not loaded yet
    if (chartData === null || chartData.length === 0) return null; // Loaded but no data
    return chartData[chartData.length - 1]?.power ?? null;
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
      label: "Power",
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
      label: "Power",
      value: "No data",
      units: "",
    };

    return <ChartWidget stats={stat}>{null}</ChartWidget>;
  }

  const powerDisplayValue = currentPower !== null && currentPower !== undefined ? currentPower.toFixed(1) : "N/A";

  const subtitle = getMinerCountSubtitle(deviceCount ?? null, totalMiners);
  const stat = {
    label: "Power",
    value: powerDisplayValue,
    units: "kW",
    subtitle,
    tooltipContent: subtitle ? "Some devices do not make this data available to Proto Fleet." : undefined,
  };

  return (
    <ChartWidget stats={stat}>
      <LineChart
        chartData={chartData}
        aggregateKey="power"
        units="kW"
        activeKeys={["power"]}
        heightClass="h-60"
        tickCount={5}
        duration={duration}
      />
    </ChartWidget>
  );
}
