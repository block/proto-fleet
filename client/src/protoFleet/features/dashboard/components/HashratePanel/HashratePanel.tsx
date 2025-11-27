import { useMemo } from "react";
import { transformHashrateMetricsToChartData } from "./utils";
import { AggregationType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { Duration } from "@/shared/components/DurationSelector";
import LineChart from "@/shared/components/LineChart";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

interface HashratePanelProps {
  duration: Duration;
}

export function HashratePanel({ duration }: HashratePanelProps) {
  // Memoize the telemetry options to prevent re-renders
  const telemetryOptions = useMemo(
    () => ({
      measurementTypes: [MeasurementType.HASHRATE],
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
      measurementTypes: [MeasurementType.HASHRATE],
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

    return transformHashrateMetricsToChartData(metricsToTransform);
  }, [data, latestData]);

  // Get the latest hashrate value for the stat display
  const currentHashrate = useMemo(() => {
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
    return <div>Loading hashrate data...</div>;
  }

  if (!chartData || chartData.length === 0) {
    return <div>No hashrate data available</div>;
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
      <LineChart chartData={chartData} aggregateKey="hashrate" units={hashrateUnits} activeKeys={["hashrate"]} />
    </ChartWidget>
  );
}
