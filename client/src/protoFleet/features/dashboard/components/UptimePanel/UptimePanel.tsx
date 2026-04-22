import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { generateUptimeHeadline } from "./utils";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus, type UptimeStatusCount } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import { encodeFilterToURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

const needsAttentionRoute = `/miners?${encodeFilterToURL(
  create(MinerListFilterSchema, {
    deviceStatus: [
      DeviceStatus.ERROR,
      DeviceStatus.NEEDS_MINING_POOL,
      DeviceStatus.UPDATING,
      DeviceStatus.REBOOT_REQUIRED,
    ],
  }),
).toString()}`;

const notHashingRoute = `/miners?${encodeFilterToURL(
  create(MinerListFilterSchema, {
    deviceStatus: [DeviceStatus.OFFLINE, DeviceStatus.INACTIVE, DeviceStatus.MAINTENANCE],
  }),
).toString()}`;

interface UptimePanelProps {
  duration: FleetDuration;
  /** Uptime status counts — undefined = not loaded yet */
  uptimeStatusCounts: UptimeStatusCount[] | undefined;
}

export function UptimePanel({ duration, uptimeStatusCounts }: UptimePanelProps) {
  const navigate = useNavigate();

  const uptimeSegmentConfig: SegmentConfig = useMemo(
    () => ({
      hashing: {
        color: "var(--color-text-primary)",
        label: "Healthy",
        displayInBreakdown: true,
        showButton: false,
        index: 2,
      },
      broken: {
        color: "var(--color-intent-warning-fill)",
        label: "Degraded",
        displayInBreakdown: true,
        showButton: true,
        buttonVariant: "secondary",
        index: 1,
        onClick: () => navigate(needsAttentionRoute),
      },
      notHashing: {
        color: "var(--color-core-primary-10)",
        label: "Not hashing",
        displayInBreakdown: true,
        showButton: true,
        buttonVariant: "secondary",
        index: 0,
        onClick: () => navigate(notHashingRoute),
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
