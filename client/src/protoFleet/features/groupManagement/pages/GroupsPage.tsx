import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import type { CollectionStats, DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import {
  SortDirection as ProtoSortDirection,
  type SortConfig,
  SortConfigSchema,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import { useCollections } from "@/protoFleet/api/useCollections";
import GroupModal from "@/protoFleet/features/groupManagement/components/GroupModal";
import { GroupsTable } from "@/protoFleet/features/groupManagement/components/GroupsTable";
import { groupCols, GROUPS_PAGE_SIZE } from "@/protoFleet/features/groupManagement/components/GroupsTable/constants";
import type { GroupColumn } from "@/protoFleet/features/groupManagement/components/GroupsTable/constants";

import { Groups } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { SORT_ASC, type SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";

const SORT_FIELD_MAP: Partial<Record<GroupColumn, SortField>> = {
  [groupCols.name]: SortField.NAME,
  [groupCols.miners]: SortField.DEVICE_COUNT,
};

const SORT_FIELD_REVERSE_MAP: Partial<Record<SortField, GroupColumn>> = {
  [SortField.NAME]: groupCols.name,
  [SortField.DEVICE_COUNT]: groupCols.miners,
};

function toProtoSort(field: GroupColumn, direction: SortDirection): SortConfig {
  return create(SortConfigSchema, {
    field: SORT_FIELD_MAP[field] ?? SortField.NAME,
    direction: direction === SORT_ASC ? ProtoSortDirection.ASC : ProtoSortDirection.DESC,
  });
}

function fromProtoSort(sort: SortConfig): { field: GroupColumn; direction: SortDirection } {
  const field = SORT_FIELD_REVERSE_MAP[sort.field] ?? groupCols.name;
  const direction: SortDirection = sort.direction === ProtoSortDirection.DESC ? "desc" : "asc";
  return { field, direction };
}

const DEFAULT_SORT = toProtoSort(groupCols.name, SORT_ASC);

const GroupsPage = () => {
  const { listGroups, getCollectionStats } = useCollections();
  const [groups, setGroups] = useState<DeviceCollection[]>([]);
  const [statsMap, setStatsMap] = useState<Map<bigint, CollectionStats>>(new Map());
  const [isLoading, setIsLoading] = useState(true);
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [editGroup, setEditGroup] = useState<DeviceCollection | null>(null);

  // Pagination state
  const [currentPage, setCurrentPage] = useState(0);
  const [cursorHistory, setCursorHistory] = useState<(string | undefined)[]>([undefined]);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [totalGroups, setTotalGroups] = useState(0);

  // Sort state (proto format, passed directly to API)
  const [sortConfig, setSortConfig] = useState<SortConfig>(DEFAULT_SORT);
  const sortRef = useRef(sortConfig);
  useEffect(() => {
    sortRef.current = sortConfig;
  }, [sortConfig]);

  const listRequestId = useRef(0);
  const statsRequestId = useRef(0);

  const fetchStats = useCallback(
    (collections: DeviceCollection[]) => {
      if (collections.length === 0) return;
      const requestId = ++statsRequestId.current;
      const ids = collections.map((c) => c.id);
      getCollectionStats({
        collectionIds: ids,
        onSuccess: (stats) => {
          if (requestId !== statsRequestId.current) return;
          const map = new Map<bigint, CollectionStats>();
          for (const s of stats) {
            map.set(s.collectionId, s);
          }
          setStatsMap(map);
        },
      });
    },
    [getCollectionStats],
  );

  const fetchPage = useCallback(
    (page: number, pageToken?: string) => {
      const requestId = ++listRequestId.current;
      setIsLoading(true);
      listGroups({
        pageSize: GROUPS_PAGE_SIZE,
        pageToken,
        sort: sortRef.current,
        onSuccess: (collections, nextPageToken, totalCount) => {
          if (requestId !== listRequestId.current) return;
          setGroups(collections);
          fetchStats(collections);
          setCurrentPage(page);
          setHasNextPage(!!nextPageToken);
          setTotalGroups(totalCount);
          if (nextPageToken) {
            setCursorHistory((prev) => {
              const next = [...prev];
              next[page + 1] = nextPageToken;
              return next;
            });
          }
        },
        onFinally: () => {
          if (requestId !== listRequestId.current) return;
          setIsLoading(false);
        },
      });
    },
    [listGroups, fetchStats],
  );

  const fetchGroups = useCallback(() => {
    setCurrentPage(0);
    setCursorHistory([undefined]);
    setHasNextPage(false);
    fetchPage(0, undefined);
  }, [fetchPage]);

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    fetchGroups();
  }, [fetchGroups]);
  /* eslint-enable react-hooks/set-state-in-effect */

  const handleSort = useCallback(
    (field: GroupColumn, direction: SortDirection) => {
      const newSort = toProtoSort(field, direction);
      setSortConfig(newSort);
      sortRef.current = newSort;
      // Reset pagination and re-fetch from first page
      setCurrentPage(0);
      setCursorHistory([undefined]);
      setHasNextPage(false);
      fetchPage(0, undefined);
    },
    [fetchPage],
  );

  const handleNextPage = useCallback(() => {
    const nextCursor = cursorHistory[currentPage + 1];
    if (nextCursor) {
      fetchPage(currentPage + 1, nextCursor);
    }
  }, [cursorHistory, currentPage, fetchPage]);

  const handlePrevPage = useCallback(() => {
    if (currentPage > 0) {
      fetchPage(currentPage - 1, cursorHistory[currentPage - 1]);
    }
  }, [currentPage, cursorHistory, fetchPage]);

  const currentSort = fromProtoSort(sortConfig);

  if (isLoading && groups.length === 0) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  const hasGroups = groups.length > 0 || currentPage > 0;

  return (
    <>
      {!hasGroups ? (
        <div className="flex h-full flex-col justify-center p-6 sm:p-10">
          <div className="flex h-full w-full flex-col justify-center rounded-xl bg-surface-5 px-6 py-10 sm:px-20 sm:py-10 dark:bg-surface-base">
            <div className="flex flex-col gap-6">
              <div className="flex flex-col gap-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-core-primary-5">
                  <Groups width="w-5" />
                </div>
                <Header title="Groups" titleSize="text-display-200" description="Organize your miners into groups." />
              </div>
              <div>
                <Button variant="primary" onClick={() => setShowGroupModal(true)}>
                  Add group
                </Button>
              </div>
            </div>
          </div>
        </div>
      ) : (
        <>
          <div className="sticky left-0 flex items-center justify-between px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
            <h1 className="text-heading-300 text-text-primary">Groups</h1>
            <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowGroupModal(true)}>
              Add group
            </Button>
          </div>
          <div className="p-10 pt-6 phone:p-6 phone:pt-6 tablet:p-6 tablet:pt-6">
            <GroupsTable
              groups={groups}
              statsMap={statsMap}
              onEditGroup={setEditGroup}
              loading={isLoading}
              totalGroups={totalGroups}
              pageSize={GROUPS_PAGE_SIZE}
              currentPage={currentPage}
              hasPreviousPage={currentPage > 0}
              hasNextPage={hasNextPage}
              onNextPage={handleNextPage}
              onPrevPage={handlePrevPage}
              currentSort={currentSort}
              onSort={handleSort}
            />
          </div>
        </>
      )}

      {showGroupModal && <GroupModal onDismiss={() => setShowGroupModal(false)} onSuccess={fetchGroups} />}

      {editGroup && <GroupModal group={editGroup} onDismiss={() => setEditGroup(null)} onSuccess={fetchGroups} />}
    </>
  );
};

export default GroupsPage;
