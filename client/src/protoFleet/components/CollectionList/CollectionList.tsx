import { type ReactNode, useCallback, useMemo, useRef } from "react";

import { createCollectionColConfig } from "./collectionColConfig";
import { collectionColTitles, type CollectionColumn, DEFAULT_PAGE_SIZE } from "./constants";
import { getDefaultSortDirection, SORTABLE_COLUMNS } from "./sortConfig";
import type { CollectionStats, DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { type SortDirection } from "@/shared/components/List/types";

export type CollectionListItem = {
  id: string;
  collection: DeviceCollection;
  stats?: CollectionStats;
};

const activeCols: CollectionColumn[] = [
  "name",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

type CollectionListProps = {
  collections: DeviceCollection[];
  statsMap: Map<bigint, CollectionStats>;
  renderName: (item: CollectionListItem) => ReactNode;
  renderMiners: (item: CollectionListItem) => ReactNode;
  currentSort: { field: CollectionColumn; direction: SortDirection };
  onSort: (field: CollectionColumn, direction: SortDirection) => void;
  itemName: { singular: string; plural: string };
  loading?: boolean;
  total?: number;
  pageSize?: number;
  currentPage?: number;
  hasPreviousPage?: boolean;
  hasNextPage?: boolean;
  onNextPage?: () => void;
  onPrevPage?: () => void;
};

const CollectionList = ({
  collections,
  statsMap,
  renderName,
  renderMiners,
  currentSort,
  onSort,
  itemName,
  loading,
  total,
  pageSize = DEFAULT_PAGE_SIZE,
  currentPage = 0,
  hasPreviousPage = false,
  hasNextPage = false,
  onNextPage,
  onPrevPage,
}: CollectionListProps) => {
  const topRef = useRef<HTMLDivElement>(null);

  const items: CollectionListItem[] = useMemo(
    () =>
      collections.map((collection) => ({ id: String(collection.id), collection, stats: statsMap.get(collection.id) })),
    [collections, statsMap],
  );

  const colConfig = useMemo(() => createCollectionColConfig({ renderName, renderMiners }), [renderName, renderMiners]);

  const handleNextPage = useCallback(() => {
    onNextPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onNextPage]);

  const handlePrevPage = useCallback(() => {
    onPrevPage?.();
    topRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [onPrevPage]);

  const firstItemIndex = currentPage * pageSize + 1;
  const lastItemIndex = currentPage * pageSize + collections.length;
  const shouldRenderPagination = !loading && total !== undefined && total > 0;

  return (
    <>
      <div ref={topRef} />
      <List<CollectionListItem, string, CollectionColumn>
        activeCols={activeCols}
        colTitles={collectionColTitles}
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
      )}
    </>
  );
};

export default CollectionList;
