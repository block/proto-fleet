import type { ReactNode } from "react";

import { deviceSetCols, type DeviceSetColumn } from "./constants";
import type { DeviceSetListItem } from "./DeviceSetList";
import StatCell from "./StatCell";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import { type ColConfig } from "@/shared/components/List/types";
import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { formatTempRange } from "@/shared/utils/utility";

const INACTIVE_PLACEHOLDER = "—";

const HEALTH_COLOR_MAP = {
  OK: "bg-core-primary-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-accent-fill",
};

type CreateDeviceSetColConfigParams = {
  renderName: (item: DeviceSetListItem) => ReactNode;
  renderMiners: (item: DeviceSetListItem) => ReactNode;
  temperatureUnit: TemperatureUnit;
};

const createDeviceSetColConfig = ({
  renderName,
  renderMiners,
  temperatureUnit,
}: CreateDeviceSetColConfigParams): ColConfig<DeviceSetListItem, string, DeviceSetColumn> => ({
  [deviceSetCols.name]: {
    component: (item: DeviceSetListItem) => renderName(item),
    width: "min-w-44",
  },
  [deviceSetCols.zone]: {
    component: (item: DeviceSetListItem) => {
      if (item.deviceSet.typeDetails.case !== "rackInfo") return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{item.deviceSet.typeDetails.value.zone || INACTIVE_PLACEHOLDER}</span>;
    },
    width: "min-w-28",
  },
  [deviceSetCols.miners]: {
    component: (item: DeviceSetListItem) => renderMiners(item),
    width: "min-w-20",
  },
  [deviceSetCols.issues]: {
    component: (item: DeviceSetListItem) => {
      if (!item.stats) return <span>{INACTIVE_PLACEHOLDER}</span>;
      const count =
        item.stats.controlBoardIssueCount +
        item.stats.fanIssueCount +
        item.stats.hashBoardIssueCount +
        item.stats.psuIssueCount;
      if (count === 0) return <span>0</span>;
      return <span className="text-core-negative">{count}</span>;
    },
    width: "min-w-20",
  },
  [deviceSetCols.hashrate]: {
    component: (item: DeviceSetListItem) => {
      if (!item.stats || item.stats.hashrateReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{getDisplayValue(item.stats.totalHashrateThs)} TH/s</span>;
    },
    width: "min-w-28",
  },
  [deviceSetCols.efficiency]: {
    component: (item: DeviceSetListItem) => {
      if (!item.stats || item.stats.efficiencyReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.efficiencyReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{getDisplayValue(item.stats.avgEfficiencyJth)} J/TH</span>
        </StatCell>
      );
    },
    width: "min-w-28",
  },
  [deviceSetCols.power]: {
    component: (item: DeviceSetListItem) => {
      if (!item.stats || item.stats.powerReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.powerReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{getDisplayValue(item.stats.totalPowerKw)} kW</span>
        </StatCell>
      );
    },
    width: "min-w-24",
  },
  [deviceSetCols.temperature]: {
    component: (item: DeviceSetListItem) => {
      if (!item.stats || item.stats.temperatureReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{formatTempRange(item.stats.minTemperatureC, item.stats.maxTemperatureC, temperatureUnit)}</span>;
    },
    width: "min-w-28",
  },
  [deviceSetCols.health]: {
    component: (item: DeviceSetListItem) => {
      if (!item.stats || item.stats.deviceCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      const { hashingCount, brokenCount, offlineCount, sleepingCount } = item.stats;
      const segments: Segment[] = [
        { name: "Healthy", status: "OK", count: hashingCount },
        { name: "Needs Attention", status: "CRITICAL", count: brokenCount },
        { name: "Offline", status: "NA", count: offlineCount + sleepingCount },
      ];

      return (
        <div className="w-34">
          <CompositionBar segments={segments} height={6} colorMap={HEALTH_COLOR_MAP} />
        </div>
      );
    },
    width: "min-w-32",
  },
});

export { createDeviceSetColConfig };
