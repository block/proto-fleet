import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";

import { ActivityFilterSchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { useActivity } from "@/protoFleet/api/useActivity";
import { useActivityFilterOptions } from "@/protoFleet/api/useActivityFilterOptions";
import { useExportActivity } from "@/protoFleet/api/useExportActivity";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import { siteFilterFromActive, useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import ActivityFilters from "@/protoFleet/features/activity/components/ActivityFilters";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { debounce } from "@/shared/utils/utility";

const PAGE_SIZE = 50;

const ActivityPageContent = () => {
  const [searchText, setSearchText] = useState("");
  const [debouncedSearchText, setDebouncedSearchText] = useState("");
  const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [selectedUsers, setSelectedUsers] = useState<string[]>([]);

  // Path scope (/{site}/activity) → server-side site_ids / include_unassigned,
  // the same additive filter ListBuildings / ListRacks / ListMiners use. The
  // route segment is the source of truth for the active site, so we only read
  // it here — the globally-mounted SitePicker already fetches ListSites and
  // owns the knownSiteIds staleness validation (resetting a deleted/inaccessible
  // site back to all-sites), so this page does not re-fetch sites. Activity has
  // no `?site=` deep-link facet, so the scope filter is passed straight through
  // (no intersectSiteFilters). `/activity` resolves to { kind: "all" } → both
  // empty → org-wide feed, unchanged from before.
  const { activeSite } = useActiveSite({});
  const scopeFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);

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
        siteIds: scopeFilter.siteIds,
        includeUnassigned: scopeFilter.includeUnassigned,
      }),
    [selectedTypes, selectedScopes, selectedUsers, debouncedSearchText, scopeFilter],
  );

  const { activities, totalCount, isLoading, error, hasMore, loadMore } = useActivity({
    filter,
    pageSize: PAGE_SIZE,
  });
  const { exportCsv, isExportingCsv } = useExportActivity();
  const { eventTypes, scopeTypes, users } = useActivityFilterOptions();

  const hasStartedLoadingRef = useRef(false);
  const hasLoadedRef = useRef(false);
  useEffect(() => {
    if (isLoading) {
      hasStartedLoadingRef.current = true;
    } else if (hasStartedLoadingRef.current) {
      hasLoadedRef.current = true;
    }
  }, [isLoading]);

  const isInitialLoad = isLoading && activities.length === 0 && !hasLoadedRef.current;
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

  if (isInitialLoad) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <>
      <div className="sticky left-0 z-3 px-6 pt-6 laptop:px-10 laptop:pt-10">
        <div className="pb-4">
          <Header title="Activity" titleSize="text-heading-300" />
        </div>
        <div className="pb-6">
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
            actions={
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                onClick={() => exportCsv(filter)}
                loading={isExportingCsv}
                disabled={isExportingCsv || totalCount === 0}
              >
                Export CSV
              </Button>
            }
          />
        </div>
      </div>

      {error ? (
        <Callout className="mx-6 mb-4 laptop:mx-10" intent="danger" prefixIcon={<Alert />} title={error} />
      ) : null}

      <div className="p-6 pt-0 laptop:p-10 laptop:pt-0">
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

const ActivityPage = () => {
  const canReadActivity = useHasPermission("activity:read");

  if (!canReadActivity) {
    return <Navigate to="/" replace />;
  }

  return <ActivityPageContent />;
};

export default ActivityPage;
