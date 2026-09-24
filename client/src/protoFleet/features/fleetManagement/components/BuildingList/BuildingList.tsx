import { type ReactNode, useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import FleetGroupActionsMenu from "../FleetGroupActionsMenu";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type FleetListStats } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { containerHealth } from "@/protoFleet/features/containers/containerHealth";
import { containerViewTarget, useOpenContainerView } from "@/protoFleet/features/containers/useOpenContainerView";
import { createBuildingColConfig } from "@/protoFleet/features/fleetManagement/components/BuildingList/buildingColConfig";
import { buildingTabHref } from "@/protoFleet/features/fleetManagement/utils/fleetTabLinks";
import { useTemperatureUnit } from "@/protoFleet/store";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";
import { ArrowRight, Edit, Plus } from "@/shared/assets/icons";
import List, { type SelectionMode } from "@/shared/components/List";
import { type ColTitles } from "@/shared/components/List/types";
import { type RowAction } from "@/shared/components/RowActionsMenu";

type BuildingRow = {
  kind: "building";
  id: string;
  name: string;
  building: BuildingWithCounts;
  siteName: string;
  stats?: FleetListStats;
};

type ContainerRow = {
  kind: "container";
  id: string;
  name: string;
  container: Container;
  siteName: string;
  moduleCount: number;
  incomplete: boolean;
  health: { healthy: number; needsAttention: number; offline: number; sleeping: number };
};

export type BuildingListItem = BuildingRow | ContainerRow;

export type BuildingColumn =
  "name" | "site" | "racks" | "miners" | "issues" | "hashrate" | "efficiency" | "power" | "temperature" | "health";

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
  containers?: Container[];
  sites: SiteWithCounts[];
  emptyStateRow?: ReactNode;
  onEditBuilding?: (building: BuildingWithCounts) => void;
  onAddBuildingToSite?: (building: BuildingWithCounts) => void;
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
  activeSite?: ActiveSite;
  // Unfiltered building total for the count line; when filters are active and
  // it differs from the displayed count, the line reads "X of Y buildings".
  totalUnfiltered?: number;
  hasActiveFilters?: boolean;
}

const EMPTY_CONTAINERS: Container[] = [];

const BuildingList = ({
  buildings,
  containers = EMPTY_CONTAINERS,
  sites,
  emptyStateRow,
  onEditBuilding,
  onAddBuildingToSite,
  selectedIds,
  onSelectedIdsChange,
  activeSite,
  totalUnfiltered,
  hasActiveFilters,
}: BuildingListProps) => {
  const navigate = useNavigate();
  const openContainerView = useOpenContainerView();
  const temperatureUnit = useTemperatureUnit();

  const siteNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sites) {
      if (!s.site) continue;
      map.set(s.site.id.toString(), s.site.name);
    }
    return map;
  }, [sites]);

  const items: BuildingListItem[] = useMemo(() => {
    const rows: BuildingListItem[] = buildings.map((building) => {
      const id = (building.building?.id ?? 0n).toString();
      const siteId = building.building?.siteId;
      return {
        kind: "building",
        id,
        name: building.building?.name ?? "(unnamed)",
        building,
        siteName: siteId ? (siteNameById.get(siteId.toString()) ?? INACTIVE_PLACEHOLDER) : INACTIVE_PLACEHOLDER,
        stats: building.listStats,
      };
    });
    for (const container of containers) {
      const { total, incomplete } = containerHealth(container);
      rows.push({
        kind: "container",
        id: `container:${container.deviceIdentifier}`,
        name: container.name,
        container,
        siteName: container.site?.label ?? INACTIVE_PLACEHOLDER,
        moduleCount: total,
        incomplete,
        health: container.tanks.reduce(
          (sum, tank) => ({
            healthy: sum.healthy + tank.hashingCount,
            needsAttention: sum.needsAttention + tank.attentionCount + tank.isolatedCount,
            offline: sum.offline + tank.offlineCount,
            sleeping: sum.sleeping + tank.sleepingCount,
          }),
          { healthy: 0, needsAttention: 0, offline: 0, sleeping: 0 },
        ),
      });
    }
    return rows.sort((a, b) => a.name.localeCompare(b.name));
  }, [buildings, containers, siteNameById]);

  const buildExtraActions = useCallback(
    (item: BuildingRow): RowAction[] => {
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
      const buildingId = item.kind === "building" ? item.building.building?.id : undefined;
      const buildingName = item.name;
      return (
        <div className="grid w-full grid-cols-[1fr_auto] items-center gap-2">
          <div className="min-w-0">
            <span className="block truncate text-emphasis-300">{buildingName}</span>
            {item.kind === "container" && !containerViewTarget(item.container) ? (
              <span className="text-200 text-text-primary-70">Web view unavailable</span>
            ) : null}
          </div>
          {item.kind === "building" && buildingId !== undefined && buildingId !== 0n ? (
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

  const handleRowClick = useCallback(
    (item: BuildingListItem) => {
      if (item.kind === "container") {
        openContainerView(item.container);
      } else {
        navigate(`/buildings/${item.id}`);
      }
    },
    [navigate, openContainerView],
  );
  const isSelectableBuilding = useCallback((item: BuildingListItem) => {
    const buildingId = item.kind === "building" ? item.building.building?.id : undefined;
    return buildingId !== undefined && buildingId !== 0n;
  }, []);
  const handleSelectionModeChange = useCallback(() => undefined, []);
  const commonProps = {
    activeCols: ACTIVE_COLS,
    colTitles: containers.length ? { ...COL_TITLES, racks: "Racks / tanks", miners: "Miners / modules" } : COL_TITLES,
    colConfig,
    items,
    itemKey: "id" as const,
    total: items.length,
    // The building-only denominator cannot describe a mixed filtered list.
    totalUnfiltered: containers.length ? undefined : totalUnfiltered,
    hasActiveFilters,
    itemName: { singular: "building", plural: "buildings" },
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
