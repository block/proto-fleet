import { Link } from "react-router-dom";
import { groupCols, type GroupColumn } from "./constants";
import GroupNameCell from "./GroupNameCell";
import type { GroupListItem } from "./GroupsTable";
import StatCell from "./StatCell";
import type { DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import { type ColConfig } from "@/shared/components/List/types";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const INACTIVE_PLACEHOLDER = "\u2014";

const HEALTH_COLOR_MAP = {
  OK: "bg-core-primary-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-accent-fill",
};

type CreateGroupColConfigParams = {
  onEditGroup: (group: DeviceCollection) => void;
  onActionComplete?: () => void;
};

const formatTempRange = (min: number, max: number): string => {
  return `${getDisplayValue(min)}\u00B0\u2013${getDisplayValue(max)}\u00B0C`;
};

const createGroupColConfig = ({
  onEditGroup,
  onActionComplete,
}: CreateGroupColConfigParams): ColConfig<GroupListItem, string, GroupColumn> => ({
  [groupCols.name]: {
    component: (item: GroupListItem) => (
      <GroupNameCell group={item.group} onEdit={onEditGroup} onActionComplete={onActionComplete} />
    ),
    width: "min-w-44",
  },
  [groupCols.miners]: {
    component: (item: GroupListItem) => (
      <Link
        to={`/miners?group=${item.group.id}`}
        className="hover:underline"
        aria-label={`View miners in ${item.group.label}`}
      >
        {item.group.deviceCount}
      </Link>
    ),
    width: "min-w-20",
  },
  [groupCols.issues]: {
    component: (item: GroupListItem) => {
      if (!item.stats) return <span>{INACTIVE_PLACEHOLDER}</span>;
      const count = item.stats.brokenCount;
      if (count === 0) return <span>0</span>;
      return <span className="text-core-negative">{count}</span>;
    },
    width: "min-w-20",
  },
  [groupCols.hashrate]: {
    component: (item: GroupListItem) => {
      if (!item.stats || item.stats.hashrateReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{getDisplayValue(item.stats.totalHashrateThs)} TH/s</span>;
    },
    width: "min-w-28",
  },
  [groupCols.efficiency]: {
    component: (item: GroupListItem) => {
      if (!item.stats || item.stats.efficiencyReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.efficiencyReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{getDisplayValue(item.stats.avgEfficiencyJth)} J/TH</span>
        </StatCell>
      );
    },
    width: "min-w-28",
  },
  [groupCols.power]: {
    component: (item: GroupListItem) => {
      if (!item.stats || item.stats.powerReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.powerReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{getDisplayValue(item.stats.totalPowerKw)} kW</span>
        </StatCell>
      );
    },
    width: "min-w-24",
  },
  [groupCols.temperature]: {
    component: (item: GroupListItem) => {
      if (!item.stats || item.stats.temperatureReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{formatTempRange(item.stats.minTemperatureC, item.stats.maxTemperatureC)}</span>;
    },
    width: "min-w-28",
  },
  [groupCols.health]: {
    component: (item: GroupListItem) => {
      if (!item.stats || item.stats.deviceCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      const { hashingCount, brokenCount, offlineCount, sleepingCount } = item.stats;
      const segments: Segment[] = [
        { name: "Healthy", status: "OK", count: hashingCount },
        { name: "Needs Attention", status: "CRITICAL", count: brokenCount },
        { name: "Offline", status: "NA", count: offlineCount + sleepingCount },
      ];

      return (
        <div className="w-34">
          <CompositionBar segments={segments} height={6} gap={1} colorMap={HEALTH_COLOR_MAP} />
        </div>
      );
    },
    width: "min-w-32",
  },
});

export { createGroupColConfig };
