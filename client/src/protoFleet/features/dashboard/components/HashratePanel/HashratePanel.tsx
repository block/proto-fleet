import { useMemo } from "react";
import { transformHashrateMetricsToChartData } from "./utils";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { padChartDataWithNulls } from "@/protoFleet/features/dashboard/utils/chartDataPadding";
import { usePanelMetrics } from "@/protoFleet/store";
import { Duration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

interface HashratePanelProps {
  duration: Duration;
}

export function HashratePanel({ duration }: HashratePanelProps) {
  // Read hashrate metrics from store - only subscribes to HASHRATE updates
  // undefined = not loaded yet, array = loaded (empty or populated)
  const metrics = usePanelMetrics(MeasurementType.HASHRATE);

  // Transform metrics data to chart format (merging already done by store selectors)
  const chartData = useMemo(() => {
    if (metrics === undefined) return undefined; // Not loaded yet
    if (metrics.length === 0) return null; // Loaded but no data

    const transformedData = transformHashrateMetricsToChartData(metrics);

    // Pad with null values for the full duration
    return padChartDataWithNulls(transformedData, duration);
  }, [metrics, duration]);

  // Get the latest hashrate value for the stat display
  const currentHashrate = useMemo(() => {
    if (metrics === undefined) return undefined; // Not loaded yet
    if (metrics.length === 0) return null; // Loaded but no data

    // Get the most recent metric
    const latestMetric = metrics[metrics.length - 1];

    // Find the AVERAGE aggregation value
    const avgValue = latestMetric.aggregatedValues.find(
      (agg) => agg.aggregationType === AggregationType.AVERAGE,
    )?.value;

    return avgValue ?? null;
  }, [metrics]);

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

  // Format the current hashrate with appropriate units
  const formattedHashrate = currentHashrate ? formatHashrateWithUnit(currentHashrate) : null;

  const hashrateDisplayValue = formattedHashrate ? formattedHashrate.value.toFixed(1) : "N/A";
  const hashrateUnits = formattedHashrate ? formattedHashrate.unit : "";

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
      />
    </ChartWidget>
  );
}
