import { useMemo } from "react";
import { transformEfficiencyMetricsToChartData } from "./utils";
import { AggregationType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { Duration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface EfficiencyPanelProps {
  duration: Duration;
}

export function EfficiencyPanel({ duration }: EfficiencyPanelProps) {
  // Memoize the telemetry options to prevent re-renders
  const telemetryOptions = useMemo(
    () => ({
      measurementTypes: [MeasurementType.EFFICIENCY],
      duration: duration,
      enabled: true,
    }),
    [duration],
  );

  // Fetch initial telemetry metrics
  const { data, isLoading } = useTelemetryMetrics(telemetryOptions);

  // Memoize streaming options
  const streamingOptions = useMemo(
    () => ({
      deviceIds: [], // Empty means all devices
      measurementTypes: [MeasurementType.EFFICIENCY],
      enabled: true,
    }),
    [],
  );

  // Enable streaming updates
  const { latestData } = useStreamingTelemetryMetrics(streamingOptions);

  // Transform metrics data to chart format
  const chartData = useMemo(() => {
    if (!data?.metrics) return null;

    let metricsToTransform = data.metrics;

    // Merge streaming data if available
    if (latestData?.metrics && latestData.metrics.length > 0) {
      // Append new metrics from streaming, avoiding duplicates by timestamp
      const existingTimestamps = new Set(data.metrics.map((m) => m.openTime?.seconds?.toString()));

      const newMetrics = latestData.metrics.filter((m) => !existingTimestamps.has(m.openTime?.seconds?.toString()));

      metricsToTransform = [...data.metrics, ...newMetrics];
    }

    return transformEfficiencyMetricsToChartData(metricsToTransform);
  }, [data, latestData]);

  // Get the latest efficiency value for the stat display
  const currentEfficiency = useMemo(() => {
    if (!data?.metrics || data.metrics.length === 0) return null;

    // Get the most recent metric
    const latestMetric = data.metrics[data.metrics.length - 1];

    // Find the AVERAGE aggregation value
    const avgValue = latestMetric.aggregatedValues.find(
      (agg) => agg.aggregationType === AggregationType.AVERAGE,
    )?.value;

    return avgValue ?? null;
  }, [data]);

  if (isLoading) {
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

  const efficiencyDisplayValue = currentEfficiency !== null ? currentEfficiency.toFixed(1) : "N/A";

  const stat = {
    label: "Efficiency",
    value: efficiencyDisplayValue,
    units: "J/TH",
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
      />
    </ChartWidget>
  );
}
