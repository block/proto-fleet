import { type MouseEvent, type ReactNode } from "react";
import { Link } from "react-router-dom";

import { buildingTabHref } from "../../utils/fleetTabLinks";
import type { BuildingColumn, BuildingListItem } from "./BuildingList";
import StatCell from "@/protoFleet/components/DeviceSetList/StatCell";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { InventoryCell } from "@/protoFleet/features/containers/InventoryCell";
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
  item.kind === "building" && item.stats
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
      if (item.kind === "container") return <span>{item.container.tanks.length || INACTIVE_PLACEHOLDER}</span>;
      const id = item.building.building?.id;
      const count = item.stats?.rackCount.toString() ?? item.building.rackCount.toString();
      return id ? countLink(buildingTabHref("racks", id, activeSite), count) : <span>{count}</span>;
    },
    width: "min-w-20",
  },
  miners: {
    component: (item) => {
      if (item.kind === "container")
        return item.inventory ? (
          countLink(`/fleet/miners?container=${item.inventory.ref?.id}`, item.inventory.assetCount.toString())
        ) : (
          <span>{item.moduleCount || INACTIVE_PLACEHOLDER}</span>
        );
      const id = item.building.building?.id;
      const count = item.stats?.deviceCount.toString() ?? item.building.deviceCount.toString();
      return id ? countLink(buildingTabHref("miners", id, activeSite), count) : <span>{count}</span>;
    },
    width: "min-w-20",
  },
  issues: {
    component: (item) => {
      // The container API reports module states, not an issue count.
      if (item.kind === "container") return <span>{INACTIVE_PLACEHOLDER}</span>;
      const count = issueCount(item);
      if (count === undefined) return <span>{INACTIVE_PLACEHOLDER}</span>;
      if (count === 0) return <span>0</span>;
      return <span className="text-core-negative">{count}</span>;
    },
    width: "min-w-20",
  },
  hashrate: {
    component: (item) =>
      item.kind === "container" && item.inventory ? (
        <InventoryCell item={item.inventory} column="hashrate" />
      ) : (
        <span>
          {formatHashrateOrDash(
            item.kind === "container"
              ? !item.container.stale && item.container.hashrateHs !== undefined
                ? item.container.hashrateHs / 1e12
                : null
              : item.stats && item.stats.hashrateReportingCount > 0
                ? item.stats.totalHashrateThs
                : null,
          )}
        </span>
      ),
    width: "min-w-28",
  },
  efficiency: {
    component: (item) => {
      if (item.kind === "container")
        return item.inventory ? (
          <InventoryCell item={item.inventory} column="efficiency" />
        ) : (
          <span>{INACTIVE_PLACEHOLDER}</span>
        );
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
    component: (item) =>
      item.kind === "container" && item.inventory ? (
        <InventoryCell item={item.inventory} column="power" />
      ) : (
        <span>
          {formatPowerMwOrDash(
            item.kind === "container"
              ? !item.container.stale && item.container.powerW !== undefined
                ? item.container.powerW / 1000
                : null
              : item.stats && item.stats.powerReportingCount > 0
                ? item.stats.totalPowerKw
                : null,
          )}
        </span>
      ),
    width: "min-w-24",
  },
  temperature: {
    component: (item) => {
      if (item.kind === "container") {
        if (item.inventory) return <InventoryCell item={item.inventory} column="temperature" />;
        const value = item.container.temperatureC;
        return (
          <span>
            {!item.container.stale && value !== undefined
              ? formatTempRange(value, value, temperatureUnit)
              : INACTIVE_PLACEHOLDER}
          </span>
        );
      }
      if (!item.stats || item.stats.temperatureReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{formatTempRange(item.stats.minTemperatureC, item.stats.maxTemperatureC, temperatureUnit)}</span>;
    },
    width: "min-w-28",
  },
  health: {
    component: (item) => {
      if (item.kind === "container") {
        if (item.incomplete) {
          return (
            <span className="text-200 text-text-primary-70">
              {item.container.stale ? "Readings unavailable or stale" : "Incomplete module readings"}
            </span>
          );
        }
        return (
          <div className="w-34">
            <HealthBar {...item.health} />
          </div>
        );
      }
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
