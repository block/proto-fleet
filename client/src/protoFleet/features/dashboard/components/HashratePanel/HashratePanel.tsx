import { useMemo } from "react";
import { transformHashrateMetricsWithUnits } from "./utils";
import { type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { padChartDataWithNulls } from "@/protoFleet/features/dashboard/utils/chartDataPadding";
import { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface HashratePanelProps {
  duration: FleetDuration;
  /** Hashrate metrics — undefined = not loaded yet, empty array = loaded but no data */
  metrics: Metric[] | undefined;
}

export function HashratePanel({ duration, metrics }: HashratePanelProps) {
  // Transform metrics data to chart format with consistent unit scaling
  // Both chart data and unit are derived together to ensure consistency
  const { chartData, hashrateUnits } = useMemo(() => {
    if (metrics === undefined) return { chartData: undefined, hashrateUnits: "" }; // Not loaded yet
    if (metrics.length === 0) return { chartData: null, hashrateUnits: "" }; // Loaded but no data

    const { chartData: transformedData, unit } = transformHashrateMetricsWithUnits(metrics);

    // Pad with null values for the full duration
    return {
      chartData: padChartDataWithNulls(transformedData, duration),
      hashrateUnits: unit,
    };
  }, [metrics, duration]);

  // Get the latest hashrate value from already-transformed chart data
  const currentHashrate = useMemo(() => {
    if (chartData === undefined) return undefined; // Not loaded yet
    if (chartData === null || chartData.length === 0) return null; // Loaded but no data
    return chartData[chartData.length - 1]?.hashrate ?? null;
  }, [chartData]);

  // Show loading skeleton while data hasn't loaded yet
  if (metrics === undefined) {
    const stat = {
      label: "Hashrate",
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
      label: "Hashrate",
      value: "No data",
      units: "",
    };

    return <ChartWidget stats={stat}>{null}</ChartWidget>;
  }

  // Format the current hashrate for display
  const hashrateDisplayValue =
    currentHashrate !== null && currentHashrate !== undefined ? currentHashrate.toFixed(1) : "N/A";

  const stat = {
    label: "Hashrate",
    value: hashrateDisplayValue,
    units: hashrateUnits,
  };

  return (
    <ChartWidget stats={stat}>
      <LineChart
        chartData={chartData}
        aggregateKey="hashrate"
        units={hashrateUnits}
        activeKeys={["hashrate"]}
        heightClass="h-60"
        tickCount={5}
        duration={duration}
      />
    </ChartWidget>
  );
}
