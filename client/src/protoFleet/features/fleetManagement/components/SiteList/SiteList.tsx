import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles } from "@/shared/components/List/types";
import { formatPowerUsedCapacity } from "@/shared/utils/telemetryFormat";

type SiteListItem = {
  id: string;
  site: SiteWithCounts;
};

type SiteColumn = "name" | "infrastructure" | "power";

const COL_TITLES: ColTitles<SiteColumn> = {
  name: "Site",
  infrastructure: "Infrastructure",
  power: "Power / Efficiency",
};

const ACTIVE_COLS: SiteColumn[] = ["name", "infrastructure", "power"];

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
        component: (item) => {
          const location = formatSiteAddress(item.site.site ?? {}) || "—";
          return (
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-emphasis-300">{item.site.site?.name ?? "(unnamed)"}</span>
              <span className="truncate text-300 text-text-primary-50">{location}</span>
            </div>
          );
        },
        width: "min-w-44",
      },
      infrastructure: {
        component: (item) => (
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate text-300">{item.site.buildingCount.toString()} buildings</span>
            <span className="truncate text-300 text-text-primary-50">{item.site.deviceCount.toString()} miners</span>
          </div>
        ),
        width: "min-w-32",
      },
      power: {
        component: (item) => {
          const powerCapacityMw = item.site.site?.powerCapacityMw ?? 0;
          const power = formatPowerUsedCapacity(null, powerCapacityMw) ?? "—";
          return (
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-300">{power}</span>
              <span className="truncate text-300 text-text-primary-50">—</span>
            </div>
          );
        },
        width: "min-w-32",
      },
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
    />
  );
};

export default SiteList;
