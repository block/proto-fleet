import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import FleetGroupActionsMenu from "../FleetGroupActionsMenu";
import { type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { ArrowRight, Edit } from "@/shared/assets/icons";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";
import { type RowAction } from "@/shared/components/RowActionsMenu";

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
  // Opens SiteSettingsModal in edit mode against the row — satisfies
  // the "Edit site" Figma action. Hosting lives in FleetSitesPage so
  // the modal stack is shared with the Add site CTA.
  onEditSite?: (site: Site) => void;
}

const SiteList = ({ sites, emptyStateRow, onEditSite }: SiteListProps) => {
  const navigate = useNavigate();

  const items: SiteListItem[] = useMemo(
    () =>
      [...sites]
        .sort((a, b) => (a.site?.name ?? "").localeCompare(b.site?.name ?? ""))
        .map((site) => ({ id: (site.site?.id ?? 0n).toString(), site })),
    [sites],
  );

  // Sites-row extras: navigation + edit only. The bulk fan-outs (sleep,
  // reboot, manage power, etc.) plus "Add to group" sit in
  // FleetGroupActionsMenu's wired sections. Re-parenting actions (Add to
  // building / site) don't exist on site rows — sites live at the top of
  // the hierarchy.
  const buildExtraActions = useCallback(
    (item: SiteListItem): RowAction[] => {
      // View actions deep-link via `?site=<id>` URL params rather than
      // mutating the SitePicker. The picker route would race with
      // FleetLayout's "single-site picker hides the Sites tab" effect
      // and silently bounce the operator to /sites/:id before the
      // pending /miners (or /racks, /fleet/buildings) navigation
      // resolves. URL params decouple the deep-link target from picker
      // state entirely.
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
                  scope={{ kind: "site", id: siteId, name: siteName }}
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
