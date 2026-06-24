import { type MouseEvent, type ReactNode } from "react";
import { Link } from "react-router-dom";

import { buildingTabHref } from "../../utils/fleetTabLinks";
import type { BuildingColumn, BuildingListItem } from "./BuildingList";
import StatCell from "@/protoFleet/components/DeviceSetList/StatCell";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";
import { type ColConfig } from "@/shared/components/List/types";
import type { TemperatureUnit } from "@/shared/features/preferences";
import {
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerMwOrDash,
  formatTempRange,
} from "@/shared/utils/telemetryFormat";

const INACTIVE_PLACEHOLDER = "—";

const stopRowClick = (event: MouseEvent) => event.stopPropagation();

const countLink = (href: string, count: string) => (
  <Link to={href} onClick={stopRowClick} className="hover:underline">
    {count}
  </Link>
);

const issueCount = (item: BuildingListItem) =>
  item.stats
    ? item.stats.controlBoardIssueCount +
      item.stats.fanIssueCount +
      item.stats.hashBoardIssueCount +
      item.stats.psuIssueCount
    : undefined;

export const createBuildingColConfig = (
  renderName: (item: BuildingListItem) => ReactNode,
  temperatureUnit: TemperatureUnit,
  activeSite?: ActiveSite,
): ColConfig<BuildingListItem, string, BuildingColumn> => ({
  name: {
    component: renderName,
    width: "min-w-44",
  },
  site: {
    component: (item) => <span>{item.siteName}</span>,
    width: "min-w-28",
  },
  racks: {
    component: (item) => {
      const id = item.building.building?.id;
      const count = item.stats?.rackCount.toString() ?? item.building.rackCount.toString();
      return id ? countLink(buildingTabHref("racks", id, activeSite), count) : <span>{count}</span>;
    },
    width: "min-w-20",
  },
  miners: {
    component: (item) => {
      const id = item.building.building?.id;
      const count = item.stats?.deviceCount.toString() ?? item.building.deviceCount.toString();
      return id ? countLink(buildingTabHref("miners", id, activeSite), count) : <span>{count}</span>;
    },
    width: "min-w-20",
  },
  issues: {
    component: (item) => {
      const count = issueCount(item);
      if (count === undefined) return <span>{INACTIVE_PLACEHOLDER}</span>;
      if (count === 0) return <span>0</span>;
      return <span className="text-core-negative">{count}</span>;
    },
    width: "min-w-20",
  },
  hashrate: {
    component: (item) => (
      <span>
        {formatHashrateOrDash(item.stats && item.stats.hashrateReportingCount > 0 ? item.stats.totalHashrateThs : null)}
      </span>
    ),
    width: "min-w-28",
  },
  efficiency: {
    component: (item) => {
      if (!item.stats || item.stats.efficiencyReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.efficiencyReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{formatEfficiencyOrDash(item.stats.avgEfficiencyJth)}</span>
        </StatCell>
      );
    },
    width: "min-w-28",
  },
  power: {
    component: (item) => (
      <span>
        {formatPowerMwOrDash(item.stats && item.stats.powerReportingCount > 0 ? item.stats.totalPowerKw : null)}
      </span>
    ),
    width: "min-w-24",
  },
  temperature: {
    component: (item) => {
      if (!item.stats || item.stats.temperatureReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{formatTempRange(item.stats.minTemperatureC, item.stats.maxTemperatureC, temperatureUnit)}</span>;
    },
    width: "min-w-28",
  },
  health: {
    component: (item) => {
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
