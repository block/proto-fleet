import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import FleetGroupActionsMenu from "../FleetGroupActionsMenu";
import { type RowAction } from "../RowActionsMenu";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type FleetListStats } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { createBuildingColConfig } from "@/protoFleet/features/fleetManagement/components/BuildingList/buildingColConfig";
import { buildingTabHref } from "@/protoFleet/features/fleetManagement/utils/fleetTabLinks";
import { useTemperatureUnit } from "@/protoFleet/store";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";
import { ArrowRight, Edit, Plus } from "@/shared/assets/icons";
import List, { type SelectionMode } from "@/shared/components/List";
import { type ColTitles } from "@/shared/components/List/types";

export type BuildingListItem = {
  id: string;
  building: BuildingWithCounts;
  siteName: string;
  stats?: FleetListStats;
};

export type BuildingColumn =
  | "name"
  | "site"
  | "racks"
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
  racks: "Racks",
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
  "racks",
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
  activeSite?: ActiveSite;
}

const BuildingList = ({
  buildings,
  sites,
  emptyStateRow,
  onEditBuilding,
  onAddBuildingToSite,
  selectedIds,
  onSelectedIdsChange,
  activeSite,
}: BuildingListProps) => {
  const navigate = useNavigate();
  const temperatureUnit = useTemperatureUnit();

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
          const buildingId = building.building?.id ?? 0n;
          const id = buildingId.toString();
          const siteId = building.building?.siteId;
          const siteName = siteId
            ? (siteNameById.get(siteId.toString()) ?? INACTIVE_PLACEHOLDER)
            : INACTIVE_PLACEHOLDER;
          return { id, building, siteName, stats: building.listStats };
        }),
    [buildings, siteNameById],
  );

  const buildExtraActions = useCallback(
    (item: BuildingListItem): RowAction[] => {
      return [
        { label: "View building", icon: <ArrowRight />, onClick: () => navigate(`/buildings/${item.id}`) },
        {
          label: "View racks",
          icon: <ArrowRight />,
          onClick: () => navigate(buildingTabHref("racks", item.id, activeSite)),
        },
        {
          label: "View miners",
          icon: <ArrowRight />,
          onClick: () => navigate(buildingTabHref("miners", item.id, activeSite)),
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
    [activeSite, navigate, onEditBuilding, onAddBuildingToSite],
  );

  const renderName = useCallback(
    (item: BuildingListItem) => {
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
    [buildExtraActions],
  );

  const colConfig = useMemo(
    () => createBuildingColConfig(renderName, temperatureUnit, activeSite),
    [activeSite, renderName, temperatureUnit],
  );

  const handleRowClick = useCallback((item: BuildingListItem) => navigate(`/buildings/${item.id}`), [navigate]);
  const isSelectableBuilding = useCallback((item: BuildingListItem) => {
    const buildingId = item.building.building?.id;
    return buildingId !== undefined && buildingId !== 0n;
  }, []);
  const handleSelectionModeChange = useCallback(() => undefined, []);
  const commonProps = {
    activeCols: ACTIVE_COLS,
    colTitles: COL_TITLES,
    colConfig,
    items,
    itemKey: "id" as const,
    hideTotal: true,
    onRowClick: handleRowClick,
    emptyStateRow,
    paddingLeft: { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" },
    // See SiteList: page-scroll mode so the sticky header isn't trapped in a
    // nested scroll container inside the Fleet shell.
    overflowContainer: false,
  };

  if (selectedIds !== undefined && onSelectedIdsChange !== undefined) {
    const selectionMode: SelectionMode = selectedIds.length > 0 ? "subset" : "none";
    return (
      <List<BuildingListItem, string, BuildingColumn>
        {...commonProps}
        itemSelectable
        customSelectedItems={selectedIds}
        customSetSelectedItems={onSelectedIdsChange}
        customSelectionMode={selectionMode}
        onSelectionModeChange={handleSelectionModeChange}
        pageScopedSelection
        isRowSelectable={isSelectableBuilding}
      />
    );
  }

  return <List<BuildingListItem, string, BuildingColumn> {...commonProps} />;
};

export default BuildingList;
