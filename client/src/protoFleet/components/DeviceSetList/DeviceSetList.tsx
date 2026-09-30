import { type ReactNode, useCallback, useMemo, useRef } from "react";
import clsx from "clsx";

import { DEFAULT_PAGE_SIZE, deviceSetColTitles, type DeviceSetColumn } from "./constants";
import { createDeviceSetColConfig } from "./deviceSetColConfig";
import { getDefaultSortDirection, SORTABLE_COLUMNS } from "./sortConfig";
import type { DeviceSet, DeviceSetStats } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { EntityKind, type InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import { InventoryCell } from "@/protoFleet/features/containers/InventoryCell";
import { useTemperatureUnit } from "@/protoFleet/store";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List, { type SelectionMode } from "@/shared/components/List";
import { type SortDirection } from "@/shared/components/List/types";
import type { ColConfig } from "@/shared/components/List/types";
import { type Breakpoint } from "@/shared/constants/breakpoints";

type EquipmentItem = { id: string } & ({ kind: "rack"; row: DeviceSetListItem } | { kind: "tank"; row: InventoryItem });

export type DeviceSetListItem = {
  id: string;
  deviceSet: DeviceSet;
  stats?: DeviceSetStats;
};

const DEFAULT_ACTIVE_COLS: DeviceSetColumn[] = [
  "name",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

type DeviceSetListProps = {
  inventoryItems?: InventoryItem[];
  onTankClick?: (item: InventoryItem) => void;
  deviceSets: DeviceSet[];
  statsMap: Map<bigint, DeviceSetStats>;
  renderName: (item: DeviceSetListItem) => ReactNode;
  renderMiners: (item: DeviceSetListItem) => ReactNode;
  renderSite?: (item: DeviceSetListItem) => ReactNode;
  renderBuilding?: (item: DeviceSetListItem) => ReactNode;
  currentSort: { field: DeviceSetColumn; direction: SortDirection };
  onSort: (field: DeviceSetColumn, direction: SortDirection) => void;
  itemName: { singular: string; plural: string };
  columns?: DeviceSetColumn[];
  loading?: boolean;
  total?: number;
  pageSize?: number;
  currentPage?: number;
  hasPreviousPage?: boolean;
  hasNextPage?: boolean;
  onNextPage?: () => void;
  onPrevPage?: () => void;
  onRowClick?: (item: DeviceSetListItem, index: number) => void;
  emptyStateRow?: ReactNode;
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
  /**
   * Left padding for row content, applied inside cells so row dividers still
   * span the full table width. Use this instead of wrapping the list in a
   * horizontally-padded container (which leaves white gaps beside the rules).
   */
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  /**
   * When false, the list does not create its own scroll container — the page
   * scrolls instead and the sticky header pins to the page. See List's
   * `overflowContainer`.
   */
  overflowContainer?: boolean;
};

const DeviceSetList = ({
  inventoryItems,
  onTankClick,
  deviceSets,
  statsMap,
  renderName,
  renderMiners,
  renderSite,
  renderBuilding,
  currentSort,
  onSort,
  itemName,
  columns = DEFAULT_ACTIVE_COLS,
  loading,
  total,
  pageSize = DEFAULT_PAGE_SIZE,
  currentPage = 0,
  hasPreviousPage = false,
  hasNextPage = false,
  onNextPage,
  onPrevPage,
  onRowClick,
  emptyStateRow,
  selectedIds,
  onSelectedIdsChange,
  paddingLeft,
  overflowContainer,
}: DeviceSetListProps) => {
  const topRef = useRef<HTMLDivElement>(null);
  const temperatureUnit = useTemperatureUnit();

  const items: DeviceSetListItem[] = useMemo(
    () => deviceSets.map((deviceSet) => ({ id: String(deviceSet.id), deviceSet, stats: statsMap.get(deviceSet.id) })),
    [deviceSets, statsMap],
  );

  const colConfig = useMemo(
    () => createDeviceSetColConfig({ renderName, renderMiners, renderSite, renderBuilding, temperatureUnit }),
    [renderName, renderMiners, renderSite, renderBuilding, temperatureUnit],
  );

  const equipmentItems = useMemo<EquipmentItem[]>(() => {
    const byID = new Map(items.map((item) => [item.id, item]));
    if (!inventoryItems) return items.map((row) => ({ id: row.id, kind: "rack", row }));
    return inventoryItems.flatMap<EquipmentItem>((item) => {
      if (item.ref?.kind === EntityKind.TANK) return [{ id: `tank:${item.ref.id}`, kind: "tank", row: item }];
      const row = byID.get(item.ref?.id.toString() ?? "");
      return row ? [{ id: row.id, kind: "rack", row }] : [];
    });
  }, [items, inventoryItems]);
  const equipmentColumns = useMemo<ColConfig<EquipmentItem, string, DeviceSetColumn>>(() => {
    const result: ColConfig<EquipmentItem, string, DeviceSetColumn> = {};
    for (const column of Object.keys(colConfig) as DeviceSetColumn[]) {
      const config = colConfig[column];
      if (!config) continue;
      result[column] = {
        ...config,
        component: (item, selected) =>
          item.kind === "rack" ? (
            config.component?.(item.row, selected)
          ) : (
            <InventoryCell item={item.row} column={column} />
          ),
      };
    }
    return result;
  }, [colConfig]);

  const handleNextPage = useCallback(() => {
    onNextPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onNextPage]);

  const handlePrevPage = useCallback(() => {
    onPrevPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onPrevPage]);

  const firstItemIndex = currentPage * pageSize + 1;
  const lastItemIndex = currentPage * pageSize + equipmentItems.length;
  const shouldRenderPagination = !loading && total !== undefined && total > 0;
  const handleSelectionModeChange = useCallback(() => undefined, []);
  const isRowSelectable = useCallback(
    (item: EquipmentItem) => item.kind === "rack" && item.row.deviceSet.id !== 0n,
    [],
  );

  const commonListProps = {
    activeCols: columns,
    colTitles: deviceSetColTitles,
    colConfig: equipmentColumns,
    items: equipmentItems,
    itemKey: "id" as const,
    hideTotal: true,
    sortableColumns: SORTABLE_COLUMNS,
    currentSort,
    onSort,
    getDefaultSortDirection,
    onRowClick: (item: EquipmentItem, index: number) =>
      item.kind === "rack" ? onRowClick?.(item.row, index) : onTankClick?.(item.row),
    emptyStateRow,
    paddingLeft,
    overflowContainer,
  };
  const pagination = shouldRenderPagination ? (
    // In page-scroll mode pin to the viewport so the centered Prev/Next don't
    // stretch across the full table (and off-screen) under the w-max subtree.
    <div
      className={clsx(
        "sticky left-0 flex flex-col items-center gap-4 py-6",
        overflowContainer === false && PAGE_SCROLL_CHROME_WIDTH,
      )}
    >
      <span className="text-300 text-text-primary">
        Showing {firstItemIndex}–{lastItemIndex} of {total} {itemName.plural}
      </span>
      <div className="flex gap-3">
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Previous page"
          prefixIcon={<ChevronDown className="rotate-90" />}
          onClick={handlePrevPage}
          disabled={!hasPreviousPage}
        />
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Next page"
          prefixIcon={<ChevronDown className="rotate-270" />}
          onClick={handleNextPage}
          disabled={!hasNextPage}
        />
      </div>
    </div>
  ) : null;

  if (selectedIds !== undefined && onSelectedIdsChange !== undefined) {
    const selectionMode: SelectionMode = selectedIds.length > 0 ? "subset" : "none";
    return (
      <>
        <div ref={topRef} />
        <List<EquipmentItem, string, DeviceSetColumn>
          {...commonListProps}
          itemSelectable
          customSelectedItems={selectedIds}
          customSetSelectedItems={onSelectedIdsChange}
          customSelectionMode={selectionMode}
          onSelectionModeChange={handleSelectionModeChange}
          pageScopedSelection
          isRowSelectable={isRowSelectable}
        />
        {pagination}
      </>
    );
  }

  return (
    <>
      <div ref={topRef} />
      <List<EquipmentItem, string, DeviceSetColumn> {...commonListProps} />
      {pagination}
    </>
  );
};

export default DeviceSetList;
