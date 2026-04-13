import { useMemo } from "react";
import { Link } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import ChartWidget from "../ChartWidget/ChartWidget";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { encodeFilterToURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { Triangle } from "@/shared/assets/icons";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import SkeletonBar from "@/shared/components/SkeletonBar";

const FleetHealthSkeleton = ({ title = "Your fleet" }: { title?: string }) => (
  <ChartWidget
    stats={[
      { label: title, value: undefined },
      { label: "Healthy", value: undefined },
      { label: "Needs Attention", value: undefined },
      { label: "Offline", value: undefined },
      { label: "Sleeping", value: undefined },
    ]}
    statsGrid="grid-cols-5 phone:grid-cols-2 phone:gap-y-6"
    statsGap="gap-x-10 phone:gap-6"
    statsPadding="pb-10"
    statsSize="large"
  >
    <div className="w-full">
      <div className="mb-4">
        <SkeletonBar className="h-3 w-full" />
      </div>
      <div className="flex flex-wrap items-center gap-6">
        <SkeletonBar className="h-3 w-16" />
        <SkeletonBar className="h-3 w-24" />
        <SkeletonBar className="h-3 w-16" />
        <SkeletonBar className="h-3 w-16" />
      </div>
    </div>
  </ChartWidget>
);

/** undefined = still loading (skeleton), null = loaded but no data (show mdash), number = show value */
type MinerCount = number | null | undefined;

interface FleetHealthProps {
  fleetSize?: MinerCount;
  healthyMiners?: MinerCount;
  needsAttentionMiners?: MinerCount;
  offlineMiners?: MinerCount;
  sleepingMiners?: MinerCount;
  /** Override the default "Your fleet" title (e.g., group name) */
  title?: string;
  /** Extra URL search params to append to miner list links (e.g., "group=123") */
  extraFilterParams?: string;
  /** Link URL for the total miners count (e.g., "/miners?group=123") */
  totalMinersLink?: string;
}

const FleetHealth = ({
  fleetSize,
  healthyMiners,
  needsAttentionMiners,
  offlineMiners,
  sleepingMiners,
  title = "Your fleet",
  extraFilterParams,
  totalMinersLink,
}: FleetHealthProps) => {
  // undefined = still loading (show skeleton), null = loaded but no data (show mdash)
  const isLoading =
    fleetSize === undefined ||
    healthyMiners === undefined ||
    needsAttentionMiners === undefined ||
    offlineMiners === undefined ||
    sleepingMiners === undefined;

  // When any count is null, we've finished loading but have no data (e.g. API error)
  const hasNoData =
    fleetSize === null ||
    healthyMiners === null ||
    needsAttentionMiners === null ||
    offlineMiners === null ||
    sleepingMiners === null;

  // Create enhanced segments with filter URLs
  // Note: useMemo must be called unconditionally (Rules of Hooks)
  const segmentsWithFilters = useMemo(() => {
    // Return empty array during loading or no-data states to satisfy hook requirements
    if (isLoading || hasNoData) return [];

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
          deviceStatus: [
            DeviceStatus.ERROR,
            DeviceStatus.NEEDS_MINING_POOL,
            DeviceStatus.UPDATING,
            DeviceStatus.REBOOT_REQUIRED,
          ],
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
    return segmentConfigs.map((segment) => {
      const params = encodeFilterToURL(segment.filter);
      if (extraFilterParams) {
        new URLSearchParams(extraFilterParams).forEach((value, key) => params.set(key, value));
      }
      return {
        ...segment,
        filterUrl: `/miners?${params.toString()}`,
        percentage: segment.count !== undefined ? Math.round((segment.count / totalMiners) * 100) : undefined,
      };
    });
  }, [
    fleetSize,
    healthyMiners,
    needsAttentionMiners,
    offlineMiners,
    sleepingMiners,
    isLoading,
    hasNoData,
    extraFilterParams,
  ]);

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
  const titleStat = useMemo(
    () => ({
      label: title,
      value:
        fleetSize !== undefined
          ? totalMinersLink
            ? `${fleetSize}\u200B`
            : `${fleetSize} ${fleetSize === 1 ? "miner" : "miners"}`
          : undefined,
      text:
        totalMinersLink && fleetSize !== undefined ? (
          <Link to={totalMinersLink} className="underline">
            View all
          </Link>
        ) : undefined,
    }),
    [fleetSize, title, totalMinersLink],
  );

  if (isLoading) {
    return <FleetHealthSkeleton title={title} />;
  }

  if (hasNoData) {
    return (
      <ChartWidget
        stats={[
          { label: title, value: "\u2014" },
          { label: "Healthy", value: "\u2014" },
          { label: "Needs Attention", value: "\u2014" },
          { label: "Offline", value: "\u2014" },
          { label: "Sleeping", value: "\u2014" },
        ]}
        statsGrid="grid-cols-5 phone:grid-cols-2 phone:gap-y-6"
        statsGap="gap-x-10 phone:gap-6"
        statsPadding="pb-10"
        statsSize="large"
      >
        {null}
      </ChartWidget>
    );
  }

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
