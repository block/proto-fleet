import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { generateUptimeHeadline } from "./utils";
import { type UptimeStatusCount } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface UptimePanelProps {
  duration: FleetDuration;
  /** Uptime status counts — undefined = not loaded yet */
  uptimeStatusCounts: UptimeStatusCount[] | undefined;
}

export function UptimePanel({ duration, uptimeStatusCounts }: UptimePanelProps) {
  const navigate = useNavigate();

  // Uptime segment configuration with navigation handler
  const uptimeSegmentConfig: SegmentConfig = useMemo(
    () => ({
      hashing: {
        color: "var(--color-text-primary)",
        label: "Hashing",
        displayInBreakdown: true,
        showButton: false,
        index: 1,
      },
      notHashing: {
        color: "var(--color-core-primary-10)",
        label: "Not hashing",
        displayInBreakdown: true,
        showButton: true,
        buttonVariant: "secondary",
        index: 0,
        onClick: () => {
          // Navigate to miners page with offline, sleeping, and needs-attention status filters
          navigate("/miners?status=offline,sleeping,needs-attention");
        },
      },
    }),
    [navigate],
  );

  if (uptimeStatusCounts === undefined) {
    const stat = {
      label: "Uptime",
      value: undefined,
      units: "",
    };

    return (
      <div className="flex w-full flex-row overflow-hidden rounded-xl bg-surface-base dark:bg-core-primary-5 phone:flex-col phone:gap-6">
        <ChartWidget stats={stat} className="w-1/2 rounded-none! bg-transparent dark:bg-transparent phone:w-full">
          <SkeletonBar className="h-60 w-full" />
        </ChartWidget>
        <div className="flex w-1/2 flex-col justify-center gap-16 space-y-3 rounded-xl bg-transparent p-10 dark:bg-transparent phone:w-full phone:gap-4 phone:p-6 phone:pt-0">
          <SkeletonBar className="h-20 w-full" />
          <SkeletonBar className="h-20 w-full" />
        </div>
      </div>
    );
  }

  return (
    <SegmentedMetricPanel
      title="Uptime"
      headlineGenerator={generateUptimeHeadline}
      chartData={uptimeStatusCounts}
      segmentConfig={uptimeSegmentConfig}
      duration={duration}
    />
  );
}
