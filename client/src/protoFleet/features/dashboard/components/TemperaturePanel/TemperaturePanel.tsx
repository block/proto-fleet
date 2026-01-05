import { generateTemperatureHeadline } from "./utils";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import { useTemperatureStatusCounts } from "@/protoFleet/store";
import { Triangle } from "@/shared/assets/icons";
import { Duration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

// Temperature segment configuration
const temperatureSegmentConfig: SegmentConfig = {
  cold: {
    color: "var(--color-intent-info-fill)",
    label: "Cold",
    displayInBreakdown: true,
    showButton: false,
    index: 2,
  },
  ok: {
    color: "var(--color-intent-info-20)",
    label: "Healthy",
    displayInBreakdown: true,
    index: 3,
    showButton: false,
  },
  hot: {
    color: "var(--color-intent-warning-fill)",
    label: "Hot",
    displayInBreakdown: true,
    showButton: false,
    index: 1,
  },
  critical: {
    color: "var(--color-intent-critical-fill)",
    label: "Critical",
    displayInBreakdown: true,
    showButton: false,
    icon: <Triangle />,
    index: 0,
    buttonVariant: "primary", // Use primary button for critical items
  },
};

interface TemperaturePanelProps {
  duration: Duration;
}

export function TemperaturePanel({ duration }: TemperaturePanelProps) {
  // Read temperature status counts from store - only subscribes to temperature updates
  // undefined = not loaded yet, array = loaded (empty or populated)
  const temperatureStatusCounts = useTemperatureStatusCounts();

  if (temperatureStatusCounts === undefined) {
    const stat = {
      label: "Temperature",
      value: undefined,
      units: "",
    };

    return (
      <div className="flex w-full flex-row overflow-hidden rounded-xl bg-surface-base dark:bg-core-primary-5 phone:flex-col phone:gap-6">
        <ChartWidget stats={stat} className="w-1/2 rounded-none! bg-transparent dark:bg-transparent phone:w-full">
          <SkeletonBar className="h-60 w-full" />
        </ChartWidget>
        <div className="flex w-1/2 flex-col justify-between space-y-3 rounded-xl bg-transparent p-10 dark:bg-transparent phone:w-full phone:gap-4 phone:p-6 phone:pt-0">
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
