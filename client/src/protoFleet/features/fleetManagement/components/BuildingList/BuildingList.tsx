import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";
import { formatPowerUsedCapacity, KW_PER_MW } from "@/shared/utils/telemetryFormat";

type BuildingListItem = {
  id: string;
  building: BuildingWithCounts;
  siteName: string;
};

type BuildingColumn = "name" | "site" | "racks" | "power";

const COL_TITLES: ColTitles<BuildingColumn> = {
  name: "Building",
  site: "Site",
  racks: "Racks",
  power: "Power",
};

const ACTIVE_COLS: BuildingColumn[] = ["name", "site", "racks", "power"];

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
          const siteName = siteId ? (siteNameById.get(siteId.toString()) ?? "—") : "—";
          return { id, building, siteName };
        }),
    [buildings, siteNameById],
  );

  const colConfig = useMemo<ColConfig<BuildingListItem, string, BuildingColumn>>(
    () => ({
      name: {
        component: (item) => (
          <span className="truncate text-emphasis-300">{item.building.building?.name ?? "(unnamed)"}</span>
        ),
        width: "min-w-44",
      },
      site: {
        component: (item) => <span className="truncate text-300 text-text-primary-50">{item.siteName}</span>,
        width: "min-w-32",
      },
      racks: {
        component: (item) => <span className="truncate text-300">{item.building.rackCount.toString()}</span>,
        width: "min-w-20",
      },
      power: {
        component: (item) => {
          // formatPowerUsedCapacity expects capacity in MW; building.powerKw
          // is kW, so divide before passing.
          const powerCapacityKw = item.building.building?.powerKw ?? 0;
          const power = formatPowerUsedCapacity(null, powerCapacityKw / KW_PER_MW) ?? "—";
          return <span className="truncate text-300">{power}</span>;
        },
        width: "min-w-28",
      },
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
    />
  );
};

export default BuildingList;
