import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import FleetGroupActionsMenu from "../FleetGroupActionsMenu";
import { type RowAction } from "../RowActionsMenu";
import { type FleetListStats } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { createSiteColConfig } from "@/protoFleet/features/fleetManagement/components/SiteList/siteColConfig";
import { siteTabHref } from "@/protoFleet/features/fleetManagement/utils/fleetTabLinks";
import { useTemperatureUnit } from "@/protoFleet/store";
import { ArrowRight, Edit } from "@/shared/assets/icons";
import List, { type SelectionMode } from "@/shared/components/List";
import { type ColTitles } from "@/shared/components/List/types";

export type SiteListItem = {
  id: string;
  site: SiteWithCounts;
  stats?: FleetListStats;
};

export type SiteColumn =
  "name" | "buildings" | "racks" | "miners" | "issues" | "hashrate" | "efficiency" | "power" | "temperature" | "health";

const COL_TITLES: ColTitles<SiteColumn> = {
  name: "Name",
  buildings: "Buildings",
  racks: "Racks",
  miners: "Miners",
  issues: "Issues",
  hashrate: "Total Hashrate",
  efficiency: "Avg Efficiency",
  power: "Total Power",
  temperature: "Temperature",
  health: "Health",
};

const ACTIVE_COLS: SiteColumn[] = [
  "name",
  "buildings",
  "racks",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

interface SiteListProps {
  sites: SiteWithCounts[];
  emptyStateRow?: ReactNode;
  onEditSite?: (site: Site) => void;
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
  // Unfiltered site total for the count line; when filters are active and it
  // differs from the displayed count, the line reads "X of Y sites".
  totalUnfiltered?: number;
  hasActiveFilters?: boolean;
}

const SiteList = ({
  sites,
  emptyStateRow,
  onEditSite,
  selectedIds,
  onSelectedIdsChange,
  totalUnfiltered,
  hasActiveFilters,
}: SiteListProps) => {
  const navigate = useNavigate();
  const temperatureUnit = useTemperatureUnit();

  const items: SiteListItem[] = useMemo(
    () =>
      [...sites]
        .sort((a, b) => (a.site?.name ?? "").localeCompare(b.site?.name ?? ""))
        .map((site) => {
          const siteId = site.site?.id ?? 0n;
          return { id: siteId.toString(), site, stats: site.listStats };
        }),
    [sites],
  );

  const buildExtraActions = useCallback(
    (item: SiteListItem): RowAction[] => {
      // Deep-link via `?site=<id>` rather than mutating SitePicker —
      // avoids racing FleetLayout's single-site-redirect effect.
      return [
        { label: "View site", icon: <ArrowRight />, onClick: () => navigate(`/sites/${item.id}`) },
        {
          label: "View buildings",
          icon: <ArrowRight />,
          onClick: () => navigate(siteTabHref("buildings", item.id)),
        },
        { label: "View racks", icon: <ArrowRight />, onClick: () => navigate(siteTabHref("racks", item.id)) },
        {
          label: "View miners",
          icon: <ArrowRight />,
          onClick: () => navigate(siteTabHref("miners", item.id)),
          showGroupDivider: true,
        },
        {
          label: "Edit site",
          icon: <Edit />,
          onClick: () => (item.site.site ? onEditSite?.(item.site.site) : undefined),
          hidden: onEditSite === undefined,
        },
      ];
    },
    [navigate, onEditSite],
  );

  const renderName = useCallback(
    (item: SiteListItem) => {
      const siteId = item.site.site?.id;
      const siteName = item.site.site?.name ?? "(unnamed)";
      return (
        <div className="grid w-full grid-cols-[1fr_auto] items-center gap-2">
          <span className="truncate text-emphasis-300">{siteName}</span>
          {siteId !== undefined && siteId !== 0n ? (
            <FleetGroupActionsMenu
              scopes={[{ kind: "site", id: siteId, name: siteName }]}
              ariaLabel={`Actions for ${siteName}`}
              testIdPrefix={`site-list-row-${item.id}-actions`}
              extraActions={buildExtraActions(item)}
            />
          ) : null}
        </div>
      );
    },
    [buildExtraActions],
  );

  const colConfig = useMemo(() => createSiteColConfig(renderName, temperatureUnit), [renderName, temperatureUnit]);

  const handleRowClick = useCallback((item: SiteListItem) => navigate(`/sites/${item.id}`), [navigate]);
  const isSelectableSite = useCallback((item: SiteListItem) => {
    const siteId = item.site.site?.id;
    return siteId !== undefined && siteId !== 0n;
  }, []);
  const handleSelectionModeChange = useCallback(() => undefined, []);
  const commonProps = {
    activeCols: ACTIVE_COLS,
    colTitles: COL_TITLES,
    colConfig,
    items,
    itemKey: "id" as const,
    total: items.length,
    totalUnfiltered,
    hasActiveFilters,
    itemName: { singular: "site", plural: "sites" },
    onRowClick: handleRowClick,
    emptyStateRow,
    paddingLeft: { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" },
    // Page-scroll mode: the Fleet shell is the single scroll container. An
    // overflow wrapper here would trap the sticky <thead> in a nested scroll
    // context (overflow-x:* computes overflow-y to auto), so the header would
    // not stick to the page. Let wide tables scroll the page horizontally
    // instead — the sticky-left chrome (FilterRow, header) is built for that.
    overflowContainer: false,
  };

  if (selectedIds !== undefined && onSelectedIdsChange !== undefined) {
    const selectionMode: SelectionMode = selectedIds.length > 0 ? "subset" : "none";
    return (
      <List<SiteListItem, string, SiteColumn>
        {...commonProps}
        itemSelectable
        customSelectedItems={selectedIds}
        customSetSelectedItems={onSelectedIdsChange}
        customSelectionMode={selectionMode}
        onSelectionModeChange={handleSelectionModeChange}
        pageScopedSelection
        isRowSelectable={isSelectableSite}
      />
    );
  }

  return <List<SiteListItem, string, SiteColumn> {...commonProps} />;
};

export default SiteList;
