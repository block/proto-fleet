import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { ActivityFilterSchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { useActivity } from "@/protoFleet/api/useActivity";
import { useActivityFilterOptions } from "@/protoFleet/api/useActivityFilterOptions";
import { useExportActivity } from "@/protoFleet/api/useExportActivity";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import ActivityFilters from "@/protoFleet/features/activity/components/ActivityFilters";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import { formatLabel } from "@/protoFleet/features/activity/utils/formatLabel";
import { Alert, DismissTiny } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { debounce } from "@/shared/utils/utility";

const PAGE_SIZE = 50;

const ActivityPage = () => {
  const [searchText, setSearchText] = useState("");
  const [debouncedSearchText, setDebouncedSearchText] = useState("");
  const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [selectedUsers, setSelectedUsers] = useState<string[]>([]);

  const debouncedSetSearch = useMemo(() => debounce((text: string) => setDebouncedSearchText(text), 300), []);
  useEffect(() => () => debouncedSetSearch.cancel(), [debouncedSetSearch]);

  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchText(value);
      if (value === "") {
        debouncedSetSearch.cancel();
        setDebouncedSearchText("");
      } else {
        debouncedSetSearch(value);
      }
    },
    [debouncedSetSearch],
  );

  const filter = useMemo(
    () =>
      create(ActivityFilterSchema, {
        eventTypes: selectedTypes,
        scopeTypes: selectedScopes,
        userIds: selectedUsers,
        searchText: debouncedSearchText,
      }),
    [selectedTypes, selectedScopes, selectedUsers, debouncedSearchText],
  );

  const { activities, totalCount, isLoading, error, hasMore, loadMore } = useActivity({
    filter,
    pageSize: PAGE_SIZE,
  });
  const { exportCsv, isExportingCsv } = useExportActivity();
  const { eventTypes, scopeTypes, users } = useActivityFilterOptions();

  const [hasLoaded, setHasLoaded] = useState(false);
  const hasStartedLoadingRef = useRef(false);
  useEffect(() => {
    if (isLoading) {
      hasStartedLoadingRef.current = true;
    } else if (hasStartedLoadingRef.current && !hasLoaded) {
      setHasLoaded(true);
    }
  }, [isLoading, hasLoaded]);

  const isInitialLoad = isLoading && activities.length === 0 && !hasLoaded;
  const isLoadingMore = isLoading && activities.length > 0;

  const hasActiveFilters =
    selectedTypes.length > 0 || selectedScopes.length > 0 || selectedUsers.length > 0 || debouncedSearchText !== "";

  const handleClearFilters = useCallback(() => {
    setSearchText("");
    setDebouncedSearchText("");
    debouncedSetSearch.cancel();
    setSelectedTypes([]);
    setSelectedScopes([]);
    setSelectedUsers([]);
  }, [debouncedSetSearch]);

  const handleRemoveType = useCallback((id: string) => setSelectedTypes((prev) => prev.filter((t) => t !== id)), []);

  const handleRemoveScope = useCallback((id: string) => setSelectedScopes((prev) => prev.filter((s) => s !== id)), []);

  const handleRemoveUser = useCallback((id: string) => setSelectedUsers((prev) => prev.filter((u) => u !== id)), []);

  const activeFilterPills = useMemo(() => {
    const pills: { key: string; label: string; onRemove: () => void }[] = [];
    for (const id of selectedTypes) {
      pills.push({ key: `type-${id}`, label: formatLabel(id), onRemove: () => handleRemoveType(id) });
    }
    for (const id of selectedScopes) {
      pills.push({ key: `scope-${id}`, label: formatLabel(id), onRemove: () => handleRemoveScope(id) });
    }
    for (const id of selectedUsers) {
      const user = users.find((u) => u.userId === id);
      pills.push({
        key: `user-${id}`,
        label: user?.username ?? id,
        onRemove: () => handleRemoveUser(id),
      });
    }
    return pills;
  }, [selectedTypes, selectedScopes, selectedUsers, users, handleRemoveType, handleRemoveScope, handleRemoveUser]);

  if (isInitialLoad) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <>
      <div className="sticky left-0 z-3 px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
        <div className="flex items-center justify-between pb-4">
          <Header title="Activity" titleSize="text-heading-300" />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={() => exportCsv(filter)}
            loading={isExportingCsv}
            disabled={isExportingCsv || totalCount === 0}
          >
            Export CSV
          </Button>
        </div>
        <div className="flex flex-col gap-2 pb-6">
          <ActivityFilters
            searchValue={searchText}
            onSearchChange={handleSearchChange}
            eventTypes={eventTypes}
            scopeTypes={scopeTypes}
            users={users}
            selectedTypes={selectedTypes}
            selectedScopes={selectedScopes}
            selectedUsers={selectedUsers}
            onTypesChange={setSelectedTypes}
            onScopesChange={setSelectedScopes}
            onUsersChange={setSelectedUsers}
          />
          {activeFilterPills.length > 0 ? (
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
          ) : null}
        </div>
      </div>

      {error ? (
        <Callout className="mx-10 mb-4 phone:mx-6 tablet:mx-6" intent="danger" prefixIcon={<Alert />} title={error} />
      ) : null}

      <div className="p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
        <ActivityTable
          activities={activities}
          noDataElement={
            isLoading ? (
              <></>
            ) : hasActiveFilters ? (
              <NoFilterResultsEmptyState hasActiveFilters onClearFilters={handleClearFilters} />
            ) : undefined
          }
        />
        {hasMore ? (
          <div className="flex justify-center py-6">
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              onClick={loadMore}
              loading={isLoadingMore}
              disabled={isLoadingMore}
            >
              Load more
            </Button>
          </div>
        ) : null}
      </div>
    </>
  );
};

export default ActivityPage;
