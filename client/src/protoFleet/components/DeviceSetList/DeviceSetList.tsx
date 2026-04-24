import { type ReactNode, useCallback, useMemo, useRef } from "react";

import { DEFAULT_PAGE_SIZE, deviceSetColTitles, type DeviceSetColumn } from "./constants";
import { createDeviceSetColConfig } from "./deviceSetColConfig";
import { getDefaultSortDirection, SORTABLE_COLUMNS } from "./sortConfig";
import type { DeviceSet, DeviceSetStats } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { type SortDirection } from "@/shared/components/List/types";

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
  deviceSets: DeviceSet[];
  statsMap: Map<bigint, DeviceSetStats>;
  renderName: (item: DeviceSetListItem) => ReactNode;
  renderMiners: (item: DeviceSetListItem) => ReactNode;
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
};

const DeviceSetList = ({
  deviceSets,
  statsMap,
  renderName,
  renderMiners,
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
}: DeviceSetListProps) => {
  const topRef = useRef<HTMLDivElement>(null);
  const temperatureUnit = useTemperatureUnit();

  const items: DeviceSetListItem[] = useMemo(
    () => deviceSets.map((deviceSet) => ({ id: String(deviceSet.id), deviceSet, stats: statsMap.get(deviceSet.id) })),
    [deviceSets, statsMap],
  );

  const colConfig = useMemo(
    () => createDeviceSetColConfig({ renderName, renderMiners, temperatureUnit }),
    [renderName, renderMiners, temperatureUnit],
  );

  const handleNextPage = useCallback(() => {
    onNextPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onNextPage]);

  const handlePrevPage = useCallback(() => {
    onPrevPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onPrevPage]);

  const firstItemIndex = currentPage * pageSize + 1;
  const lastItemIndex = currentPage * pageSize + deviceSets.length;
  const shouldRenderPagination = !loading && total !== undefined && total > 0;

  return (
    <>
      <div ref={topRef} />
      <List<DeviceSetListItem, string, DeviceSetColumn>
        activeCols={columns}
        colTitles={deviceSetColTitles}
        colConfig={colConfig}
        items={items}
        itemKey="id"
        hideTotal
        overflowContainer={false}
        sortableColumns={SORTABLE_COLUMNS}
        currentSort={currentSort}
        onSort={onSort}
        getDefaultSortDirection={getDefaultSortDirection}
        onRowClick={onRowClick}
        emptyStateRow={emptyStateRow}
      />

      {shouldRenderPagination ? (
        <div className="sticky left-0 flex flex-col items-center gap-4 py-6">
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
      ) : null}
    </>
  );
};

export default DeviceSetList;
