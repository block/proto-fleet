import { useCallback, useMemo, useRef } from "react";

import { groupCols, groupColTitles, type GroupColumn, GROUPS_PAGE_SIZE } from "./constants";
import { createGroupColConfig } from "./groupColConfig";
import { getDefaultSortDirection, SORTABLE_COLUMNS } from "./sortConfig";
import type { CollectionStats, DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { type SortDirection } from "@/shared/components/List/types";

type GroupListItem = {
  id: string;
  group: DeviceCollection;
  stats?: CollectionStats;
};

const activeCols: GroupColumn[] = [
  groupCols.name,
  groupCols.miners,
  groupCols.issues,
  groupCols.hashrate,
  groupCols.efficiency,
  groupCols.power,
  groupCols.temperature,
  groupCols.health,
];

type GroupsTableProps = {
  groups: DeviceCollection[];
  statsMap: Map<bigint, CollectionStats>;
  onEditGroup: (group: DeviceCollection) => void;
  loading?: boolean;
  totalGroups?: number;
  pageSize?: number;
  currentPage?: number;
  hasPreviousPage?: boolean;
  hasNextPage?: boolean;
  onNextPage?: () => void;
  onPrevPage?: () => void;
  currentSort: { field: GroupColumn; direction: SortDirection };
  onSort: (field: GroupColumn, direction: SortDirection) => void;
};

const GroupsTable = ({
  groups,
  statsMap,
  onEditGroup,
  loading,
  totalGroups,
  pageSize = GROUPS_PAGE_SIZE,
  currentPage = 0,
  hasPreviousPage = false,
  hasNextPage = false,
  onNextPage,
  onPrevPage,
  currentSort,
  onSort,
}: GroupsTableProps) => {
  const topRef = useRef<HTMLDivElement>(null);

  const items: GroupListItem[] = useMemo(
    () => groups.map((group) => ({ id: String(group.id), group, stats: statsMap.get(group.id) })),
    [groups, statsMap],
  );

  const colConfig = useMemo(() => createGroupColConfig({ onEditGroup }), [onEditGroup]);

  const handleNextPage = useCallback(() => {
    onNextPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onNextPage]);

  const handlePrevPage = useCallback(() => {
    onPrevPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onPrevPage]);

  const firstItemIndex = currentPage * pageSize + 1;
  const lastItemIndex = currentPage * pageSize + groups.length;
  const shouldRenderPagination = !loading && totalGroups !== undefined && totalGroups > 0;

  return (
    <>
      <div ref={topRef} />
      <List<GroupListItem, string, GroupColumn>
        activeCols={activeCols}
        colTitles={groupColTitles}
        colConfig={colConfig}
        items={items}
        itemKey="id"
        hideTotal
        overflowContainer={false}
        sortableColumns={SORTABLE_COLUMNS}
        currentSort={currentSort}
        onSort={onSort}
        getDefaultSortDirection={getDefaultSortDirection}
      />

      {shouldRenderPagination && (
        <div className="sticky left-0 flex flex-col items-center gap-4 py-6">
          <span className="text-300 text-text-primary">
            Showing {firstItemIndex}–{lastItemIndex} of {totalGroups} groups
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
      )}
    </>
  );
};

export default GroupsTable;
export type { GroupListItem };
