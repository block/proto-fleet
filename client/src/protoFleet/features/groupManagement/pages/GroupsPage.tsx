import { type ReactNode, useCallback, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import {
  DeviceSetList,
  type DeviceSetListItem,
  issueOptions,
  useIssueFilter,
} from "@/protoFleet/components/DeviceSetList";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import NullState from "@/protoFleet/components/NullState";
import GroupModal from "@/protoFleet/features/groupManagement/components/GroupModal";
import GroupNameCell from "@/protoFleet/features/groupManagement/components/GroupsTable/GroupNameCell";
import { useDeviceSetListState } from "@/protoFleet/hooks/useDeviceSetListState";

import { Alert, DismissTiny, Groups } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import ProgressCircular from "@/shared/components/ProgressCircular";

const GROUPS_PAGE_SIZE = 50;

const GroupsPage = () => {
  const navigate = useNavigate();
  const { listGroups } = useDeviceSets();
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [editGroup, setEditGroup] = useState<DeviceSet | null>(null);
  const [selectedIssues, setSelectedIssues] = useState<string[]>([]);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

  const {
    deviceSets: groups,
    statsMap,
    isLoading,
    hasEverLoaded,
    error,
    currentSort,
    currentPage,
    hasNextPage,
    totalCount,
    handleSort,
    handleNextPage,
    handlePrevPage,
    resetAndFetch,
  } = useDeviceSetListState(listGroups, GROUPS_PAGE_SIZE, getErrorComponentTypes);

  const handleIssuesChange = useCallback(
    (issues: string[]) => {
      setSelectedIssues(issues);
      selectedIssuesRef.current = issues;
      resetAndFetch();
    },
    [resetAndFetch, selectedIssuesRef],
  );

  const handleRemoveIssue = useCallback(
    (issueId: string) => {
      const next = selectedIssues.filter((id) => id !== issueId);
      setSelectedIssues(next);
      selectedIssuesRef.current = next;
      resetAndFetch();
    },
    [selectedIssues, resetAndFetch, selectedIssuesRef],
  );

  const activeFilterPills = useMemo(() => {
    return selectedIssues
      .map((issueId) => {
        const issue = issueOptions.find((o) => o.id === issueId);
        if (!issue) return null;
        return { key: `issue-${issueId}`, label: issue.label, onRemove: () => handleRemoveIssue(issueId) };
      })
      .filter(Boolean) as { key: string; label: string; onRemove: () => void }[];
  }, [selectedIssues, handleRemoveIssue]);

  const hasActiveFilters = selectedIssues.length > 0;

  const handleClearFilters = useCallback(() => {
    setSelectedIssues([]);
    selectedIssuesRef.current = [];
    resetAndFetch();
  }, [resetAndFetch, selectedIssuesRef]);

  const emptyStateRow: ReactNode = useMemo(() => {
    if (isLoading || totalCount > 0) return undefined;
    return <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />;
  }, [hasActiveFilters, isLoading, totalCount, handleClearFilters]);

  const renderName = useCallback(
    (item: DeviceSetListItem) => (
      <GroupNameCell group={item.deviceSet} onEdit={setEditGroup} onActionComplete={resetAndFetch} />
    ),
    [resetAndFetch],
  );

  const handleRowClick = useCallback(
    (item: DeviceSetListItem) => {
      navigate(`/groups/${encodeURIComponent(item.deviceSet.label)}`);
    },
    [navigate],
  );

  const renderMiners = useCallback(
    (item: DeviceSetListItem) => (
      <Link
        to={`/miners?group=${item.deviceSet.id}`}
        className="hover:underline"
        aria-label={`View miners in ${item.deviceSet.label}`}
      >
        {item.deviceSet.deviceCount}
      </Link>
    ),
    [],
  );

  if (isLoading && !hasEverLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (error && !hasEverLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-body-200 text-text-secondary">{error}</p>
      </div>
    );
  }

  const hasGroups = groups.length > 0 || hasEverLoaded;

  return (
    <>
      {!hasGroups ? (
        <NullState
          icon={<Groups width="w-5" />}
          title="Groups"
          description="Organize your miners into groups."
          action={
            <Button variant="primary" onClick={() => setShowGroupModal(true)}>
              Add group
            </Button>
          }
        />
      ) : (
        <>
          <div className="sticky left-0 z-3 px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
            <h1 className="pb-4 text-heading-300 text-text-primary">Groups</h1>
            <div className="flex flex-col gap-2 pb-6">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <DropdownFilter
                    title="Issues"
                    options={issueOptions}
                    selectedOptions={selectedIssues}
                    onSelect={handleIssuesChange}
                    withButtons
                  />
                </div>
                <div className="flex items-center gap-2">
                  <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowGroupModal(true)}>
                    Add group
                  </Button>
                </div>
              </div>
              {activeFilterPills.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {activeFilterPills.map((pill) => (
                    <Button
                      key={pill.key}
                      size={sizes.compact}
                      variant={variants.accent}
                      prefixIcon={<DismissTiny />}
                      onClick={pill.onRemove}
                    >
                      {pill.label}
                    </Button>
                  ))}
                </div>
              )}
            </div>
          </div>
          {error ? (
            <Callout
              className="mx-10 mb-4 phone:mx-6 tablet:mx-6"
              intent="danger"
              prefixIcon={<Alert />}
              title={error}
            />
          ) : null}
          <div className="p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
            <DeviceSetList
              deviceSets={groups}
              statsMap={statsMap}
              renderName={renderName}
              renderMiners={renderMiners}
              currentSort={currentSort}
              onSort={handleSort}
              itemName={{ singular: "group", plural: "groups" }}
              loading={isLoading}
              total={totalCount}
              pageSize={GROUPS_PAGE_SIZE}
              currentPage={currentPage}
              hasPreviousPage={currentPage > 0}
              hasNextPage={hasNextPage}
              onNextPage={handleNextPage}
              onPrevPage={handlePrevPage}
              onRowClick={handleRowClick}
              emptyStateRow={emptyStateRow}
            />
          </div>
        </>
      )}

      {showGroupModal && (
        <GroupModal show={showGroupModal} onDismiss={() => setShowGroupModal(false)} onSuccess={resetAndFetch} />
      )}

      {editGroup && (
        <GroupModal
          show={!!editGroup}
          group={editGroup}
          onDismiss={() => setEditGroup(null)}
          onSuccess={resetAndFetch}
        />
      )}
    </>
  );
};

export default GroupsPage;
