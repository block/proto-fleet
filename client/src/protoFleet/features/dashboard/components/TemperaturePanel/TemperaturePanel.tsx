import { generateTemperatureHeadline } from "./utils";
import type { TemperatureStatusCount } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import { useDuration } from "@/protoFleet/store";
import { Triangle } from "@/shared/assets/icons";

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
  temperatureStatusCounts?: TemperatureStatusCount[];
  isLoading?: boolean;
}

export function TemperaturePanel({
  temperatureStatusCounts,
  isLoading = false,
}: TemperaturePanelProps) {
  const duration = useDuration();

  if (isLoading) {
    return <div>Loading temperature data...</div>;
  }

  if (!temperatureStatusCounts || temperatureStatusCounts.length === 0) {
    return <div>No temperature data available</div>;
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
