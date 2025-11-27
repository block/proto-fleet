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
  unhealthyMiners?: number;
  offlineMiners?: number;
}

const FleetHealth = ({ fleetSize, healthyMiners, unhealthyMiners, offlineMiners }: FleetHealthProps) => {
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
      },
      {
        name: "Unhealthy",
        status: "CRITICAL" as Segment["status"],
        count: unhealthyMiners,
        filter: create(MinerListFilterSchema, {
          deviceStatus: [DeviceStatus.ERROR, DeviceStatus.INACTIVE],
        }),
      },
      {
        name: "Offline",
        status: "NA" as Segment["status"],
        count: offlineMiners,
        filter: create(MinerListFilterSchema, {
          deviceStatus: [DeviceStatus.OFFLINE],
        }),
      },
    ];

    // Add filter URL and percentage to each segment
    return segmentConfigs.map((segment) => ({
      ...segment,
      filterUrl: `/miners?${encodeFilterToURL(segment.filter).toString()}`,
      percentage: segment.count !== undefined ? Math.round((segment.count / totalMiners) * 100) : undefined,
    }));
  }, [fleetSize, healthyMiners, unhealthyMiners, offlineMiners]);

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
      segmentsWithFilters.map((segment) => ({
        label: segment.name,
        value: segment.percentage !== undefined ? `${segment.percentage}%` : undefined,
        text:
          segment.count !== undefined ? (
            <Link to={segment.filterUrl} className="hover:underline">
              {segment.count} miners
            </Link>
          ) : undefined,
      })),
    [segmentsWithFilters],
  );

  // Create the title stat for ChartWidget title area
  const titleStat = {
    label: "Your fleet",
    value: fleetSize !== undefined ? `${fleetSize} miners` : undefined,
  };

  return (
    <ChartWidget
      stats={[titleStat, ...stats]}
      statsGrid="grid-cols-4 phone:grid-cols-2"
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
              NA: "bg-core-primary-20",
            }}
          />
        </div>

        {/* Legend */}
        <div className="flex items-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 rounded-full bg-core-primary-fill" />
            <span className="text-grayscale-gray-70">Healthy</span>
          </div>
          <div className="flex items-center gap-2">
            <Triangle className="h-3 w-3 text-intent-critical-fill" />
            <span className="text-grayscale-gray-70">Unhealthy</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 rounded-full bg-core-primary-20" />
            <span className="text-grayscale-gray-70">Offline</span>
          </div>
        </div>
      </div>
    </ChartWidget>
  );
};

export default FleetHealth;
