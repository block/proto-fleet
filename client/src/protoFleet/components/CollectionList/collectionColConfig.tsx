import type { ReactNode } from "react";

import type { CollectionListItem } from "./CollectionList";
import { collectionCols, type CollectionColumn } from "./constants";
import StatCell from "./StatCell";
import CompositionBar, { type Segment } from "@/shared/components/CompositionBar";
import { type ColConfig } from "@/shared/components/List/types";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const INACTIVE_PLACEHOLDER = "—";

const HEALTH_COLOR_MAP = {
  OK: "bg-core-primary-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-core-accent-fill",
};

const formatTempRange = (min: number, max: number): string => {
  return `${getDisplayValue(min)}°–${getDisplayValue(max)}°C`;
};

type CreateCollectionColConfigParams = {
  renderName: (item: CollectionListItem) => ReactNode;
  renderMiners: (item: CollectionListItem) => ReactNode;
};

const createCollectionColConfig = ({
  renderName,
  renderMiners,
}: CreateCollectionColConfigParams): ColConfig<CollectionListItem, string, CollectionColumn> => ({
  [collectionCols.name]: {
    component: (item: CollectionListItem) => renderName(item),
    width: "min-w-44",
  },
  [collectionCols.location]: {
    component: (item: CollectionListItem) => {
      if (item.collection.typeDetails.case !== "rackInfo") return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{item.collection.typeDetails.value.location || INACTIVE_PLACEHOLDER}</span>;
    },
    width: "min-w-28",
  },
  [collectionCols.miners]: {
    component: (item: CollectionListItem) => renderMiners(item),
    width: "min-w-20",
  },
  [collectionCols.issues]: {
    component: (item: CollectionListItem) => {
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
  [collectionCols.hashrate]: {
    component: (item: CollectionListItem) => {
      if (!item.stats || item.stats.hashrateReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{getDisplayValue(item.stats.totalHashrateThs)} TH/s</span>;
    },
    width: "min-w-28",
  },
  [collectionCols.efficiency]: {
    component: (item: CollectionListItem) => {
      if (!item.stats || item.stats.efficiencyReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.efficiencyReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{getDisplayValue(item.stats.avgEfficiencyJth)} J/TH</span>
        </StatCell>
      );
    },
    width: "min-w-28",
  },
  [collectionCols.power]: {
    component: (item: CollectionListItem) => {
      if (!item.stats || item.stats.powerReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return (
        <StatCell metricReportingCount={item.stats.powerReportingCount} deviceCount={item.stats.deviceCount}>
          <span>{getDisplayValue(item.stats.totalPowerKw)} kW</span>
        </StatCell>
      );
    },
    width: "min-w-24",
  },
  [collectionCols.temperature]: {
    component: (item: CollectionListItem) => {
      if (!item.stats || item.stats.temperatureReportingCount === 0) return <span>{INACTIVE_PLACEHOLDER}</span>;
      return <span>{formatTempRange(item.stats.minTemperatureC, item.stats.maxTemperatureC)}</span>;
    },
    width: "min-w-28",
  },
  [collectionCols.health]: {
    component: (item: CollectionListItem) => {
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

export { createCollectionColConfig };
