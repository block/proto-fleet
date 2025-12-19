import { useMemo } from "react";
import { Link } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import ChartWidget from "../ChartWidget/ChartWidget";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { encodeFilterToURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { Triangle } from "@/shared/assets/icons";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";

interface FleetHealthProps {
  fleetSize?: number;
  healthyMiners?: number;
  needsAttentionMiners?: number;
  offlineMiners?: number;
  sleepingMiners?: number;
}

const FleetHealth = ({
  fleetSize,
  healthyMiners,
  needsAttentionMiners,
  offlineMiners,
  sleepingMiners,
}: FleetHealthProps) => {
  // Create enhanced segments with filter URLs
  const segmentsWithFilters = useMemo(() => {
    const totalMiners = fleetSize || 1; // prevent division by zero

    // Define segments with their filter configurations
    const segmentConfigs = [
      {
        name: "Healthy",
        status: "OK" as Segment["status"],
        count: healthyMiners,
        filter: create(MinerListFilterSchema, {
          deviceStatus: [DeviceStatus.ONLINE],
        }),
        clickable: false, // Healthy is not clickable
      },
      {
        name: "Needs Attention",
        status: "CRITICAL" as Segment["status"],
        count: needsAttentionMiners,
        filter: create(MinerListFilterSchema, {
          deviceStatus: [DeviceStatus.ERROR, DeviceStatus.NEEDS_MINING_POOL],
        }),
        clickable: true,
      },
      {
        name: "Offline",
        status: "NA" as Segment["status"],
        count: offlineMiners,
        filter: create(MinerListFilterSchema, {
          deviceStatus: [DeviceStatus.OFFLINE],
        }),
        clickable: true,
      },
      {
        name: "Sleeping",
        status: "WARNING" as Segment["status"],
        count: sleepingMiners,
        filter: create(MinerListFilterSchema, {
          deviceStatus: [DeviceStatus.INACTIVE],
        }),
        clickable: true,
      },
    ];

    // Add filter URL and percentage to each segment
    return segmentConfigs.map((segment) => ({
      ...segment,
      filterUrl: `/miners?${encodeFilterToURL(segment.filter).toString()}`,
      percentage: segment.count !== undefined ? Math.round((segment.count / totalMiners) * 100) : undefined,
    }));
  }, [fleetSize, healthyMiners, needsAttentionMiners, offlineMiners, sleepingMiners]);

  // Extract basic segments for CompositionBar (without extra props)
  const segments = useMemo<Segment[]>(
    () =>
      segmentsWithFilters.map(({ name, status, count }) => ({
        name,
        status,
        count,
      })),
    [segmentsWithFilters],
  );

  // Derive stats from segments
  const stats = useMemo(
    () =>
      segmentsWithFilters.map((segment) => {
        // Pluralization helper
        const minerText = segment.count === 1 ? "miner" : "miners";

        // Determine if this segment should have a link
        const shouldHaveLink = segment.clickable && (segment.count ?? 0) > 0;

        return {
          label: segment.name,
          value: segment.percentage !== undefined ? `${segment.percentage}%` : undefined,
          text:
            segment.count !== undefined ? (
              shouldHaveLink ? (
                <Link to={segment.filterUrl} className="underline">
                  {segment.count} {minerText}
                </Link>
              ) : (
                <>
                  {segment.count} {minerText}
                </>
              )
            ) : undefined,
        };
      }),
    [segmentsWithFilters],
  );

  // Create the title stat for ChartWidget title area
  const titleStat = {
    label: "Your fleet",
    value: fleetSize !== undefined ? `${fleetSize} ${fleetSize === 1 ? "miner" : "miners"}` : undefined,
  };

  return (
    <ChartWidget
      stats={[titleStat, ...stats]}
      statsGrid="grid-cols-5 phone:grid-cols-2 phone:gap-y-6"
      statsGap="gap-x-10 phone:gap-6"
      statsPadding="pb-10"
      statsSize="large"
    >
      <div className="w-full">
        {/* Composition Bar */}
        <div className="mb-4">
          <CompositionBar
            segments={segments}
            height={12}
            gap={2}
            colorMap={{
              OK: "bg-core-primary-fill",
              NA: "bg-core-accent-fill",
              WARNING: "bg-core-primary-20",
            }}
          />
        </div>

        {/* Legend */}
        <div className="flex flex-wrap items-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 rounded-full bg-core-primary-fill" />
            <span className="text-grayscale-gray-70">Healthy</span>
          </div>
          <div className="flex items-center gap-2">
            <Triangle className="h-3 w-3 text-intent-critical-fill" />
            <span className="text-grayscale-gray-70">Needs Attention</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 rounded-full bg-core-accent-fill" />
            <span className="text-grayscale-gray-70">Offline</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 rounded-full bg-core-primary-20" />
            <span className="text-grayscale-gray-70">Sleeping</span>
          </div>
        </div>
      </div>
    </ChartWidget>
  );
};

export default FleetHealth;
