import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import FleetGroupActionsMenu from "../FleetGroupActionsMenu";
import { type RowAction } from "../RowActionsMenu";
import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { ArrowRight, Edit } from "@/shared/assets/icons";
import List, { type SelectionMode } from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";

type SiteListItem = {
  id: string;
  site: SiteWithCounts;
};

type SiteColumn = "name" | "miners" | "issues" | "hashrate" | "efficiency" | "power" | "temperature" | "health";

const INACTIVE_PLACEHOLDER = "—";

const COL_TITLES: ColTitles<SiteColumn> = {
  name: "Name",
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
}

const SiteList = ({ sites, emptyStateRow, onEditSite, selectedIds, onSelectedIdsChange }: SiteListProps) => {
  const navigate = useNavigate();

  const items: SiteListItem[] = useMemo(
    () =>
      [...sites]
        .sort((a, b) => (a.site?.name ?? "").localeCompare(b.site?.name ?? ""))
        .map((site) => ({ id: (site.site?.id ?? 0n).toString(), site })),
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
          onClick: () => navigate(`/fleet/buildings?site=${item.id}`),
        },
        { label: "View racks", icon: <ArrowRight />, onClick: () => navigate(`/racks?site=${item.id}`) },
        {
          label: "View miners",
          icon: <ArrowRight />,
          onClick: () => navigate(`/miners?site=${item.id}`),
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

  const colConfig = useMemo<ColConfig<SiteListItem, string, SiteColumn>>(
    () => ({
      name: {
        component: (item) => {
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
        width: "min-w-44",
      },
      miners: {
        component: (item) => <span>{item.site.deviceCount.toString()}</span>,
        width: "min-w-20",
      },
      issues: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-20" },
      hashrate: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-28" },
      efficiency: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-28" },
      power: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-24" },
      temperature: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-28" },
      health: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-32" },
    }),
    [buildExtraActions],
  );

  const handleRowClick = useCallback((item: SiteListItem) => navigate(`/sites/${item.id}`), [navigate]);
  const selectableSelectionMode: SelectionMode = selectedIds && selectedIds.length > 0 ? "subset" : "none";
  const handleSelectionModeChange = useCallback(() => undefined, []);
  const isSelectableSite = useCallback((item: SiteListItem) => {
    const siteId = item.site.site?.id;
    return siteId !== undefined && siteId !== 0n;
  }, []);

  if (selectedIds !== undefined && onSelectedIdsChange !== undefined) {
    return (
      <List<SiteListItem, string, SiteColumn>
        activeCols={ACTIVE_COLS}
        colTitles={COL_TITLES}
        colConfig={colConfig}
        items={items}
        itemKey="id"
        itemSelectable
        customSelectedItems={selectedIds}
        customSetSelectedItems={onSelectedIdsChange}
        customSelectionMode={selectableSelectionMode}
        onSelectionModeChange={handleSelectionModeChange}
        pageScopedSelection
        isRowSelectable={isSelectableSite}
        hideTotal
        onRowClick={handleRowClick}
        emptyStateRow={emptyStateRow}
        paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
      />
    );
  }

  return (
    <List<SiteListItem, string, SiteColumn>
      activeCols={ACTIVE_COLS}
      colTitles={COL_TITLES}
      colConfig={colConfig}
      items={items}
      itemKey="id"
      hideTotal
      onRowClick={handleRowClick}
      emptyStateRow={emptyStateRow}
      paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
    />
  );
};

export default SiteList;
