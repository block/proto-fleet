import { type MouseEvent, type ReactNode } from "react";
import { Link } from "react-router-dom";

import { siteTabHref } from "../../utils/fleetTabLinks";
import type { SiteColumn, SiteListItem } from "./SiteList";
import StatCell from "@/protoFleet/components/DeviceSetList/StatCell";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import { type ColConfig } from "@/shared/components/List/types";
import type { TemperatureUnit } from "@/shared/features/preferences";
import {
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerUsedCapacity,
  formatTempRange,
} from "@/shared/utils/telemetryFormat";

const INACTIVE_PLACEHOLDER = "—";

const HEALTH_COLOR_MAP = {
  OK: "bg-core-primary-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-accent-fill",
};

const stopRowClick = (event: MouseEvent) => event.stopPropagation();

const countLink = (href: string, count: string) => (
  <Link to={href} onClick={stopRowClick} className="hover:underline">
    {count}
  </Link>
);

const issueCount = (item: SiteListItem) =>
  item.stats
    ? item.stats.controlBoardIssueCount +
      item.stats.fanIssueCount +
      item.stats.hashBoardIssueCount +
      item.stats.psuIssueCount
    : undefined;

export const createSiteColConfig = (
  renderName: (item: SiteListItem) => ReactNode,
  temperatureUnit: TemperatureUnit,
): ColConfig<SiteListItem, string, SiteColumn> => ({
  name: {
    component: renderName,
    width: "min-w-44",
  },
  buildings: {
    component: (item) => {
      const id = item.site.site?.id;
      const count = item.stats?.buildingCount.toString() ?? item.site.buildingCount.toString();
      return id ? countLink(siteTabHref("buildings", id), count) : <span>{count}</span>;
    },
    width: "min-w-24",
  },
  racks: {
    component: (item) => {
      const count = item.stats?.rackCount.toString() ?? item.site.rackCount.toString();
      return <span>{count}</span>;
    },
    width: "min-w-20",
  },
  miners: {
    component: (item) => {
      const id = item.site.site?.id;
      const count = item.stats?.deviceCount.toString() ?? item.site.deviceCount.toString();
      return id ? countLink(siteTabHref("miners", id), count) : <span>{count}</span>;
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
    component: (item) => {
      const usedKw = item.stats && item.stats.powerReportingCount > 0 ? item.stats.totalPowerKw : null;
      return (
        <span>{formatPowerUsedCapacity(usedKw, item.site.site?.powerCapacityMw ?? 0) ?? INACTIVE_PLACEHOLDER}</span>
      );
    },
    width: "min-w-28",
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
      const segments: Segment[] = [
        { name: "Healthy", status: "OK", count: item.stats.hashingCount },
        { name: "Needs Attention", status: "CRITICAL", count: item.stats.brokenCount },
        { name: "Offline", status: "NA", count: item.stats.offlineCount + item.stats.sleepingCount },
      ];
      return (
        <div className="w-34">
          <CompositionBar segments={segments} height={6} gap={0.25} colorMap={HEALTH_COLOR_MAP} />
        </div>
      );
    },
    width: "min-w-32",
  },
});
