import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import FleetGroupActionsMenu from "../FleetGroupActionsMenu";
import { type RowAction } from "../RowActionsMenu";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { ArrowRight, Edit, Plus } from "@/shared/assets/icons";
import List, { type SelectionMode } from "@/shared/components/List";
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
  onEditBuilding?: (building: BuildingWithCounts) => void;
  onAddBuildingToSite?: (building: BuildingWithCounts) => void;
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
}

const BuildingList = ({
  buildings,
  sites,
  emptyStateRow,
  onEditBuilding,
  onAddBuildingToSite,
  selectedIds,
  onSelectedIdsChange,
}: BuildingListProps) => {
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

  const buildExtraActions = useCallback(
    (item: BuildingListItem): RowAction[] => {
      return [
        { label: "View building", icon: <ArrowRight />, onClick: () => navigate(`/buildings/${item.id}`) },
        { label: "View racks", icon: <ArrowRight />, onClick: () => navigate(`/racks?building=${item.id}`) },
        {
          label: "View miners",
          icon: <ArrowRight />,
          onClick: () => navigate(`/miners?building=${item.id}`),
          showGroupDivider: true,
        },
        {
          label: "Edit building",
          icon: <Edit />,
          onClick: () => onEditBuilding?.(item.building),
          hidden: onEditBuilding === undefined,
        },
        {
          label: "Add to site",
          icon: <Plus />,
          onClick: () => onAddBuildingToSite?.(item.building),
          hidden: onAddBuildingToSite === undefined,
        },
      ];
    },
    [navigate, onEditBuilding, onAddBuildingToSite],
  );

  const colConfig = useMemo<ColConfig<BuildingListItem, string, BuildingColumn>>(
    () => ({
      name: {
        component: (item) => {
          const buildingId = item.building.building?.id;
          const buildingName = item.building.building?.name ?? "(unnamed)";
          return (
            <div className="grid w-full grid-cols-[1fr_auto] items-center gap-2">
              <span className="truncate text-emphasis-300">{buildingName}</span>
              {buildingId !== undefined && buildingId !== 0n ? (
                <FleetGroupActionsMenu
                  scopes={[{ kind: "building", id: buildingId, name: buildingName }]}
                  ariaLabel={`Actions for ${buildingName}`}
                  testIdPrefix={`building-list-row-${item.id}-actions`}
                  extraActions={buildExtraActions(item)}
                />
              ) : null}
            </div>
          );
        },
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
    [buildExtraActions],
  );

  const handleRowClick = useCallback((item: BuildingListItem) => navigate(`/buildings/${item.id}`), [navigate]);
  const selectableSelectionMode: SelectionMode = selectedIds && selectedIds.length > 0 ? "subset" : "none";
  const handleSelectionModeChange = useCallback(() => undefined, []);
  const isSelectableBuilding = useCallback((item: BuildingListItem) => {
    const buildingId = item.building.building?.id;
    return buildingId !== undefined && buildingId !== 0n;
  }, []);

  if (selectedIds !== undefined && onSelectedIdsChange !== undefined) {
    return (
      <List<BuildingListItem, string, BuildingColumn>
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
        isRowSelectable={isSelectableBuilding}
        hideTotal
        onRowClick={handleRowClick}
        emptyStateRow={emptyStateRow}
        paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
      />
    );
  }

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
      paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
    />
  );
};

export default BuildingList;
