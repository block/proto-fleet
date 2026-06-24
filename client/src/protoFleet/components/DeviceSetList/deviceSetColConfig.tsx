import type { ReactNode } from "react";

import { deviceSetCols, type DeviceSetColumn } from "./constants";
import type { DeviceSetListItem } from "./DeviceSetList";
import StatCell from "./StatCell";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { type ColConfig } from "@/shared/components/List/types";
import type { TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { formatTempRange } from "@/shared/utils/telemetryFormat";

const INACTIVE_PLACEHOLDER = "—";

type CreateDeviceSetColConfigParams = {
  renderName: (item: DeviceSetListItem) => ReactNode;
  renderMiners: (item: DeviceSetListItem) => ReactNode;
  // Optional renderers for the new site / building columns. Default to em-dash
  // so callers that don't pass them (or rows whose rack has no site/building)
  // get the inactive placeholder without each caller needing to wire it.
  renderSite?: (item: DeviceSetListItem) => ReactNode;
  renderBuilding?: (item: DeviceSetListItem) => ReactNode;
  temperatureUnit: TemperatureUnit;
};

const createDeviceSetColConfig = ({
  renderName,
  renderMiners,
  renderSite,
  renderBuilding,
  temperatureUnit,
}: CreateDeviceSetColConfigParams): ColConfig<DeviceSetListItem, string, DeviceSetColumn> => ({
  [deviceSetCols.name]: {
    component: (item: DeviceSetListItem) => renderName(item),
    width: "min-w-44",
  },
  [deviceSetCols.site]: {
    component: (item: DeviceSetListItem) => (renderSite ? renderSite(item) : <span>{INACTIVE_PLACEHOLDER}</span>),
    width: "min-w-28",
  },
  [deviceSetCols.building]: {
    component: (item: DeviceSetListItem) =>
      renderBuilding ? renderBuilding(item) : <span>{INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-28",
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
      return (
        <div className="w-34">
          <HealthBar
            healthy={item.stats.hashingCount}
            needsAttention={item.stats.brokenCount}
            offline={item.stats.offlineCount}
            sleeping={item.stats.sleepingCount}
          />
        </div>
      );
    },
    width: "min-w-32",
  },
});

export { createDeviceSetColConfig };
