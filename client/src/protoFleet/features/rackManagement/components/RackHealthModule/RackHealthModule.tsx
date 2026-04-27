import { useMemo } from "react";
import { create } from "@bufbuild/protobuf";

import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import {
  type StatusBreakdownItem,
  StatusBreakdownPanel,
} from "@/protoFleet/features/dashboard/components/StatusBreakdownPanel";
import { encodeFilterToURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { RackDetailGrid } from "@/protoFleet/features/rackManagement/components/RackDetailGrid";
import type {
  NumberingOrigin,
  SlotHealthState,
} from "@/protoFleet/features/rackManagement/components/RackDetailGrid/types";
import { Triangle } from "@/shared/assets/icons";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface RackHealthModuleProps {
  rows: number;
  cols: number;
  slotStates: Record<string, SlotHealthState>;
  numberingOrigin: NumberingOrigin;
  onEmptySlotClick?: (row: number, col: number) => void;
  /** undefined = loading (skeleton), null = loaded but no data (mdash), number = show value */
  hashingCount?: number | null;
  needsAttentionCount?: number | null;
  offlineCount?: number | null;
  sleepingCount?: number | null;
  rackFilterParam?: string;
}

function buildFilterUrl(statuses: DeviceStatus[], rackFilterParam?: string): string {
  const filter = create(MinerListFilterSchema, { deviceStatus: statuses });
  const params = encodeFilterToURL(filter);
  if (rackFilterParam) {
    new URLSearchParams(rackFilterParam).forEach((value, key) => params.set(key, value));
  }
  return `/miners?${params.toString()}`;
}

const formatCount = (count: number): string => {
  return `${count} ${count === 1 ? "miner" : "miners"}`;
};

export const RackHealthModule = ({
  rows,
  cols,
  slotStates,
  numberingOrigin,
  onEmptySlotClick,
  hashingCount,
  needsAttentionCount,
  offlineCount,
  sleepingCount,
  rackFilterParam,
}: RackHealthModuleProps) => {
  const navigate = useNavigate();

  const isLoading =
    hashingCount === undefined &&
    needsAttentionCount === undefined &&
    offlineCount === undefined &&
    sleepingCount === undefined;

  const hasNoData =
    hashingCount === null || needsAttentionCount === null || offlineCount === null || sleepingCount === null;

  const breakdownItems = useMemo<StatusBreakdownItem[]>(() => {
    const hashing = hashingCount ?? 0;
    const sleeping = sleepingCount ?? 0;
    const needsAttention = needsAttentionCount ?? 0;
    const offline = offlineCount ?? 0;

    return [
      {
        key: "healthy",
        color: "--color-text-primary",
        label: "Healthy",
        percentageLabel: formatCount(hashing),
        count: hashing,
        showButton: true,
        buttonVariant: "secondary",
        onClick: () => navigate(buildFilterUrl([DeviceStatus.ONLINE], rackFilterParam)),
      },
      {
        key: "sleeping",
        color: "--color-core-primary-20",
        label: "Sleeping",
        percentageLabel: formatCount(sleeping),
        count: sleeping,
        showButton: true,
        buttonVariant: "secondary",
        onClick: () => navigate(buildFilterUrl([DeviceStatus.INACTIVE], rackFilterParam)),
      },
      {
        key: "needsAttention",
        color: "--color-intent-critical-fill",
        label: "Needs Attention",
        icon: <Triangle className="h-3 w-3" />,
        percentageLabel: formatCount(needsAttention),
        count: needsAttention,
        showButton: true,
        buttonVariant: "secondary",
        onClick: () =>
          navigate(
            buildFilterUrl(
              [DeviceStatus.ERROR, DeviceStatus.NEEDS_MINING_POOL, DeviceStatus.UPDATING, DeviceStatus.REBOOT_REQUIRED],
              rackFilterParam,
            ),
          ),
      },
      {
        key: "offline",
        color: "--color-core-accent-fill",
        label: "Offline",
        percentageLabel: formatCount(offline),
        count: offline,
        showButton: true,
        buttonVariant: "secondary",
        onClick: () => navigate(buildFilterUrl([DeviceStatus.OFFLINE], rackFilterParam)),
      },
    ];
  }, [hashingCount, sleepingCount, needsAttentionCount, offlineCount, rackFilterParam, navigate]);

  return (
    <div className="flex w-full flex-col overflow-hidden rounded-xl bg-surface-base laptop:flex-row dark:bg-core-primary-5">
      {/* Left Panel: Rack Grid */}
      <div className="flex w-full items-center justify-center p-6 laptop:w-1/2 laptop:p-10">
        <RackDetailGrid
          rows={rows}
          cols={cols}
          slotStates={slotStates}
          numberingOrigin={numberingOrigin}
          onEmptySlotClick={onEmptySlotClick}
        />
      </div>

      {/* Right Panel: Status Breakdown */}
      {isLoading ? (
        <div className="flex w-full flex-col justify-center gap-8 p-6 pt-0 laptop:w-1/2 laptop:p-10 laptop:pt-0">
          <SkeletonBar className="h-14 w-full" />
          <SkeletonBar className="h-14 w-full" />
          <SkeletonBar className="h-14 w-full" />
          <SkeletonBar className="h-14 w-full" />
        </div>
      ) : hasNoData ? (
        <div className="flex w-full flex-col justify-center gap-4 p-6 pt-0 text-300 text-text-primary-50 laptop:w-1/2 laptop:p-10 laptop:pt-0">
          <span>Healthy: &mdash;</span>
          <span>Sleeping: &mdash;</span>
          <span>Needs Attention: &mdash;</span>
          <span>Offline: &mdash;</span>
        </div>
      ) : (
        <StatusBreakdownPanel items={breakdownItems} className="w-full laptop:w-1/2" />
      )}
    </div>
  );
};
