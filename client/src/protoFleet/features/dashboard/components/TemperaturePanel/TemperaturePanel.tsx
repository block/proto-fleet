import { useMemo } from "react";
import { generateTemperatureHeadline } from "./utils";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import { Triangle } from "@/shared/assets/icons";
import { Duration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

// Temperature segment configuration
const temperatureSegmentConfig: SegmentConfig = {
  cold: {
    color: "var(--color-intent-info-fill)",
    label: "Cold",
    displayInBreakdown: true,
    index: 2, // Third in order
  },
  ok: {
    color: "var(--color-intent-info-20)",
    label: "Normal",
    displayInBreakdown: true,
    index: 3, // Fourth in order
    percentageLabel: "Within optimal range", // Custom label for normal temperature
  },
  hot: {
    color: "var(--color-intent-warning-fill)",
    label: "Hot",
    displayInBreakdown: true,
    index: 1, // Second in order
  },
  critical: {
    color: "var(--color-intent-critical-fill)",
    label: "Critical",
    displayInBreakdown: true,
    icon: <Triangle />,
    index: 0, // First in order
    buttonVariant: "primary", // Use primary button for critical items
  },
};

interface TemperaturePanelProps {
  duration: Duration;
}

export function TemperaturePanel({ duration }: TemperaturePanelProps) {
  // Memoize the telemetry options to prevent re-renders
  const telemetryOptions = useMemo(
    () => ({
      measurementTypes: [MeasurementType.TEMPERATURE],
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
      measurementTypes: [MeasurementType.TEMPERATURE],
      enabled: true,
    }),
    [],
  );

  // Enable streaming updates
  const { latestData } = useStreamingTelemetryMetrics(streamingOptions);

  // Merge initial data with streaming updates
  const temperatureStatusCounts = useMemo(() => {
    if (!data?.temperatureStatusCounts) return [];

    let counts = data.temperatureStatusCounts;

    // Merge streaming data if available
    if (latestData?.temperatureStatusCounts && latestData.temperatureStatusCounts.length > 0) {
      // Append new counts from streaming, avoiding duplicates by timestamp
      const existingTimestamps = new Set(data.temperatureStatusCounts.map((c) => c.timestamp?.seconds?.toString()));

      const newCounts = latestData.temperatureStatusCounts.filter(
        (c) => !existingTimestamps.has(c.timestamp?.seconds?.toString()),
      );

      counts = [...data.temperatureStatusCounts, ...newCounts];
    }

    return counts;
  }, [data, latestData]);

  if (isLoading) {
    const stat = {
      label: "Temperature",
      value: undefined,
      units: "",
    };

    return (
      <div className="flex w-full flex-row overflow-hidden rounded-xl bg-surface-base phone:flex-col phone:gap-6">
        <ChartWidget stats={stat} className="w-1/2 rounded-none! phone:w-full">
          <SkeletonBar className="h-60 w-full" />
        </ChartWidget>
        <div className="flex w-1/2 flex-col justify-between space-y-3 rounded-xl bg-surface-base p-10 phone:w-full phone:gap-4 phone:p-6 phone:pt-0">
          <SkeletonBar className="h-20 w-full" />
          <SkeletonBar className="h-20 w-full" />
          <SkeletonBar className="h-20 w-full" />
        </div>
      </div>
    );
  }

  return (
    <SegmentedMetricPanel
      title="Temperature"
      headlineGenerator={generateTemperatureHeadline}
      chartData={temperatureStatusCounts}
      segmentConfig={temperatureSegmentConfig}
      duration={duration}
    />
  );
}
