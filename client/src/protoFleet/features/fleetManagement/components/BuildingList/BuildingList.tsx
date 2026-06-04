import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";

type BuildingListItem = {
  id: string;
  building: BuildingWithCounts;
  siteName: string;
};

type BuildingColumn =
  | "name"
  | "site"
  | "miners"
  | "issues"
  | "hashrate"
  | "efficiency"
  | "power"
  | "temperature"
  | "health";

const INACTIVE_PLACEHOLDER = "—";

const COL_TITLES: ColTitles<BuildingColumn> = {
  name: "Name",
  site: "Site",
  miners: "Miners",
  issues: "Issues",
  hashrate: "Total Hashrate",
  efficiency: "Avg Efficiency",
  power: "Total Power",
  temperature: "Temperature",
  health: "Health",
};

const ACTIVE_COLS: BuildingColumn[] = [
  "name",
  "site",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

interface BuildingListProps {
  buildings: BuildingWithCounts[];
  sites: SiteWithCounts[];
  emptyStateRow?: ReactNode;
}

const BuildingList = ({ buildings, sites, emptyStateRow }: BuildingListProps) => {
  const navigate = useNavigate();

  const siteNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sites) {
      if (!s.site) continue;
      map.set(s.site.id.toString(), s.site.name);
    }
    return map;
  }, [sites]);

  const items: BuildingListItem[] = useMemo(
    () =>
      [...buildings]
        .sort((a, b) => (a.building?.name ?? "").localeCompare(b.building?.name ?? ""))
        .map((building) => {
          const id = (building.building?.id ?? 0n).toString();
          const siteId = building.building?.siteId;
          const siteName = siteId
            ? (siteNameById.get(siteId.toString()) ?? INACTIVE_PLACEHOLDER)
            : INACTIVE_PLACEHOLDER;
          return { id, building, siteName };
        }),
    [buildings, siteNameById],
  );

  // Telemetry columns are em-dash placeholders until the Phase 1b metric
  // rollup lands; the column shape mirrors the rack list (with site instead
  // of zone, since zones are rack-scoped) so the /fleet/* tabs read
  // consistently.
  const colConfig = useMemo<ColConfig<BuildingListItem, string, BuildingColumn>>(
    () => ({
      name: {
        component: (item) => (
          <span className="truncate text-emphasis-300">{item.building.building?.name ?? "(unnamed)"}</span>
        ),
        width: "min-w-44",
      },
      site: {
        component: (item) => <span>{item.siteName}</span>,
        width: "min-w-28",
      },
      miners: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-20" },
      issues: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-20" },
      hashrate: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-28" },
      efficiency: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-28" },
      power: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-24" },
      temperature: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-28" },
      health: { component: () => <span>{INACTIVE_PLACEHOLDER}</span>, width: "min-w-32" },
    }),
    [],
  );

  const handleRowClick = useCallback((item: BuildingListItem) => navigate(`/buildings/${item.id}`), [navigate]);

  return (
    <List<BuildingListItem, string, BuildingColumn>
      activeCols={ACTIVE_COLS}
      colTitles={COL_TITLES}
      colConfig={colConfig}
      items={items}
      itemKey="id"
      hideTotal
      onRowClick={handleRowClick}
      emptyStateRow={emptyStateRow}
      // Mirrors MinerList so the first column lines up with the
      // FilterRow's horizontal padding on every breakpoint.
      paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
    />
  );
};

export default BuildingList;
