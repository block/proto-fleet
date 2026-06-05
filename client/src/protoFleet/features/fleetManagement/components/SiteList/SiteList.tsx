import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import List from "@/shared/components/List";
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
}

const SiteList = ({ sites, emptyStateRow }: SiteListProps) => {
  const navigate = useNavigate();

  const items: SiteListItem[] = useMemo(
    () =>
      [...sites]
        .sort((a, b) => (a.site?.name ?? "").localeCompare(b.site?.name ?? ""))
        .map((site) => ({ id: (site.site?.id ?? 0n).toString(), site })),
    [sites],
  );

  const colConfig = useMemo<ColConfig<SiteListItem, string, SiteColumn>>(
    () => ({
      name: {
        component: (item) => <span className="truncate text-emphasis-300">{item.site.site?.name ?? "(unnamed)"}</span>,
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
    [],
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
