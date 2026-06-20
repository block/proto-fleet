import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";

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
import {
  FILTER_URL_PARAM_KEYS,
  fleetListTelemetryRangesFromURL,
  parseUrlToActiveFilters,
  setTelemetryNumericFilterURLParams,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import {
  TELEMETRY_FILTER_BOUNDS,
  TELEMETRY_FILTER_KEYS,
  type TelemetryFilterKey,
} from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";
import GroupModal from "@/protoFleet/features/groupManagement/components/GroupModal";
import GroupNameCell from "@/protoFleet/features/groupManagement/components/GroupsTable/GroupNameCell";
import { useDeviceSetListState } from "@/protoFleet/hooks/useDeviceSetListState";
import { scopedPath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";

import { Alert, Groups } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import FilterChipsBar, { type FilterChipsBarNumericFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import ProgressCircular from "@/shared/components/ProgressCircular";
import type { NumericRangeValue } from "@/shared/utils/filterValidation";

const GROUPS_PAGE_SIZE = 50;

const TELEMETRY_FILTER_CHIPS: FilterChipsBarNumericFilter[] = TELEMETRY_FILTER_KEYS.map((key) => ({
  key,
  title: TELEMETRY_FILTER_BOUNDS[key].label,
  bounds: TELEMETRY_FILTER_BOUNDS[key],
}));

const GroupsPage = () => {
  const navigate = useNavigate();
  const activeSite = useRouteSiteScope() ?? DEFAULT_ACTIVE_SITE;
  const [searchParams, setSearchParams] = useSearchParams();
  const { listGroups } = useDeviceSets();
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [editGroup, setEditGroup] = useState<DeviceSet | null>(null);
  const selectedIssues = useMemo(
    () =>
      Array.from(
        new Set(
          searchParams
            .getAll("issues")
            .map((v) => v.trim())
            .filter(Boolean),
        ),
      ),
    [searchParams],
  );
  const telemetryRanges = useMemo(() => fleetListTelemetryRangesFromURL(searchParams), [searchParams]);
  const selectedNumericValues = useMemo(() => parseUrlToActiveFilters(searchParams).numericFilters, [searchParams]);
  const telemetryRangesRef = useRef(telemetryRanges);
  useEffect(() => {
    telemetryRangesRef.current = telemetryRanges;
  }, [telemetryRanges]);
  const getTelemetryRanges = useCallback(() => telemetryRangesRef.current, []);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();
  // eslint-disable-next-line react-hooks/refs -- intentional render-time sync so the initial list fetch reads URL-restored filters
  selectedIssuesRef.current = selectedIssues;

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
  } = useDeviceSetListState(
    listGroups,
    GROUPS_PAGE_SIZE,
    getErrorComponentTypes,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    getTelemetryRanges,
  );

  const handleFilterChange = useCallback(
    (key: string, values: string[]) => {
      if (key !== "issues") return;
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete("issues");
          values.forEach((value) => {
            const trimmed = value.trim();
            if (trimmed) next.append("issues", trimmed);
          });
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const handleNumericFilterChange = useCallback(
    (key: string, value: NumericRangeValue) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          setTelemetryNumericFilterURLParams(next, key as TelemetryFilterKey, value);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const filterChipsBarFilters = useMemo(
    () => [
      {
        key: "issues",
        title: "Issues",
        pluralTitle: "issues",
        options: issueOptions,
        selectedValues: selectedIssues,
        showGroupDivider: true,
      },
    ],
    [selectedIssues],
  );

  const hasActiveFilters = selectedIssues.length > 0 || telemetryRanges.length > 0;

  const filterFetchKey = useMemo(
    () => JSON.stringify([selectedIssues, telemetryRanges]),
    [selectedIssues, telemetryRanges],
  );
  const prevFilterFetchKey = useRef<string | null>(null);
  useEffect(() => {
    if (prevFilterFetchKey.current !== null && prevFilterFetchKey.current !== filterFetchKey) {
      resetAndFetch();
    }
    prevFilterFetchKey.current = filterFetchKey;
  }, [filterFetchKey, resetAndFetch]);

  const handleClearFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        FILTER_URL_PARAM_KEYS.forEach((key) => next.delete(key));
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const emptyStateRow: ReactNode = useMemo(() => {
    if (isLoading || totalCount > 0) return undefined;
    return <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />;
  }, [hasActiveFilters, isLoading, totalCount, handleClearFilters]);

  const groupDetailHref = useCallback(
    (label: string) => scopedPath(`/groups/${encodeURIComponent(label)}`, activeSite),
    [activeSite],
  );

  const renderName = useCallback(
    (item: DeviceSetListItem) => (
      <GroupNameCell
        group={item.deviceSet}
        onEdit={setEditGroup}
        onActionComplete={resetAndFetch}
        href={groupDetailHref(item.deviceSet.label)}
      />
    ),
    [groupDetailHref, resetAndFetch],
  );

  const handleRowClick = useCallback(
    (item: DeviceSetListItem) => {
      navigate(groupDetailHref(item.deviceSet.label));
    },
    [groupDetailHref, navigate],
  );

  const renderMiners = useCallback(
    (item: DeviceSetListItem) => (
      <Link
        to={scopedPath(`/fleet/miners?group=${item.deviceSet.id}`, activeSite)}
        className="hover:underline"
        aria-label={`View miners in ${item.deviceSet.label}`}
      >
        {item.deviceSet.deviceCount}
      </Link>
    ),
    [activeSite],
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

  const hasGroups = groups.length > 0 || hasEverLoaded || hasActiveFilters;

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
          <div className="sticky left-0 z-3 px-6 pt-6 laptop:px-10 laptop:pt-10">
            <h1 className="pb-4 text-heading-300 text-text-primary">Groups</h1>
            <div className="flex flex-row flex-wrap items-center gap-2 pb-6">
              <FilterChipsBar
                filters={filterChipsBarFilters}
                onChange={handleFilterChange}
                numericFilters={TELEMETRY_FILTER_CHIPS}
                selectedNumericValues={selectedNumericValues}
                onNumericChange={handleNumericFilterChange}
                onClearAll={handleClearFilters}
              />
              <Button
                className="ml-auto"
                variant={variants.secondary}
                size={sizes.compact}
                onClick={() => setShowGroupModal(true)}
              >
                Add group
              </Button>
            </div>
          </div>
          {error ? (
            <Callout className="mx-6 mb-4 laptop:mx-10" intent="danger" prefixIcon={<Alert />} title={error} />
          ) : null}
          <div className="p-6 pt-0 laptop:p-10 laptop:pt-0">
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

      {showGroupModal ? (
        <GroupModal show={showGroupModal} onDismiss={() => setShowGroupModal(false)} onSuccess={resetAndFetch} />
      ) : null}

      {editGroup ? (
        <GroupModal
          show={!!editGroup}
          group={editGroup}
          onDismiss={() => setEditGroup(null)}
          onSuccess={resetAndFetch}
        />
      ) : null}
    </>
  );
};

export default GroupsPage;
