import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { POLL_INTERVAL_MS } from "./constants";
import { useBuildings } from "@/protoFleet/api/buildings";
import {
  type SortConfig,
  SortConfigSchema,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  type MinerListFilter,
  MinerListFilterSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useExportMinerListCsv from "@/protoFleet/api/useExportMinerListCsv";
import useFleet from "@/protoFleet/api/useFleet";
import {
  intersectSiteFilters,
  isMatchNoneSiteFilter,
  siteFilterFromActive,
  useActiveSite,
} from "@/protoFleet/components/PageHeader/SitePicker";
import { useFleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";
import { type MinerColumn } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { MINERS_PAGE_SIZE } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import {
  getColumnForSortField,
  getSortField,
} from "@/protoFleet/features/fleetManagement/components/MinerList/sortConfig";
import { useBatchOperations } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { hasReachedExpectedStatus } from "@/protoFleet/features/fleetManagement/utils/batchStatusCheck";
import { parseFilterFromURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { FLEET_VISIBLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { encodeSortToURL, parseSortFromURL } from "@/protoFleet/features/fleetManagement/utils/sortUrlParams";
import type { FilterLabelSource } from "@/protoFleet/features/fleetManagement/views/viewSummary";
import Miners from "@/protoFleet/features/onboarding/components/Miners";
import { isPathScopable } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import { SORT_ASC, SORT_DESC } from "@/shared/components/List/types";

// Default sort: Name ascending (alphabetical A-Z)
const DEFAULT_SORT_CONFIG: SortConfig = create(SortConfigSchema, {
  field: SortField.NAME,
  direction: SortDirection.ASC,
});

// The path prefix is the view scope, while `?site=` remains a list filter.
// Compose them as scope ∩ filter so `/fleet/miners?site=7` still means
// all-sites scope filtered to 7, and `/8/fleet/miners?site=7` is empty.
const applySiteScopeToMinerFilter = (
  urlFilter: MinerListFilter | undefined,
  scopeSiteIds: bigint[],
  scopeIncludeUnassigned: boolean,
): { filter: MinerListFilter | undefined; matchNone: boolean } => {
  const siteFilter = intersectSiteFilters(
    { siteIds: scopeSiteIds, includeUnassigned: scopeIncludeUnassigned },
    { siteIds: urlFilter?.siteIds ?? [], includeUnassigned: urlFilter?.includeUnassigned ?? false },
  );
  if (isMatchNoneSiteFilter(siteFilter)) {
    return { filter: undefined, matchNone: true };
  }
  const siteFilterIsEmpty = siteFilter.siteIds.length === 0 && !siteFilter.includeUnassigned;

  if (!urlFilter) {
    return { filter: siteFilterIsEmpty ? undefined : create(MinerListFilterSchema, siteFilter), matchNone: false };
  }

  return {
    filter: create(MinerListFilterSchema, {
      ...urlFilter,
      siteIds: siteFilter.siteIds,
      includeUnassigned: siteFilter.includeUnassigned,
    }),
    matchNone: false,
  };
};

const Fleet = () => {
  const navigate = useNavigate();
  // ListDeviceSets (racks + groups) is rack:read-gated; skip those fetches
  // without it so the miner list opens cleanly (filters just degrade to empty).
  const canReadRacks = useHasPermission("rack:read");
  const { listGroups, listRacks } = useDeviceSets();
  const { listAllBuildings } = useBuildings();
  const [availableGroups, setAvailableGroups] = useState<DeviceSet[]>([]);
  const [availableRacks, setAvailableRacks] = useState<DeviceSet[]>([]);
  const {
    sites,
    siteCatalogAccessGranted,
    notifyPairingCompleted,
    notifyMinersChanged,
    minersChangedAt,
    publishViewFilterContext,
  } = useFleetOutletContext();
  // Validate scope against catalog *access* (authoritative now), not
  // sitesLoaded — a mid-session PermissionDenied clears `sites` to [] while
  // sitesLoaded stays true, which would otherwise strip a reachable scoped
  // route.
  const knownSiteIds = useMemo(
    () => (siteCatalogAccessGranted ? buildKnownSiteIds(sites) : undefined),
    [siteCatalogAccessGranted, sites],
  );
  const { activeSite } = useActiveSite({ knownSiteIds });
  const { siteIds: activeSiteIds, includeUnassigned: activeIncludeUnassigned } = useMemo(
    () => siteFilterFromActive(activeSite),
    [activeSite],
  );
  const [availableBuildings, setAvailableBuildings] = useState<FilterLabelSource[]>([]);
  const readableAvailableBuildings = useMemo(
    () => (siteCatalogAccessGranted ? availableBuildings : []),
    [availableBuildings, siteCatalogAccessGranted],
  );

  useEffect(() => {
    if (!canReadRacks) return;
    listGroups({
      onSuccess: (deviceSets) => {
        setAvailableGroups(deviceSets);
      },
    });
    listRacks({
      onSuccess: (deviceSets) => {
        setAvailableRacks(deviceSets);
      },
    });
  }, [canReadRacks, listGroups, listRacks]);

  useEffect(() => {
    if (!siteCatalogAccessGranted) return;

    listAllBuildings({
      onSuccess: (buildings) => {
        setAvailableBuildings(
          buildings
            .filter((row) => row.building !== undefined)
            .map((row) => ({ id: row.building!.id.toString(), label: row.building!.name })),
        );
      },
    });
  }, [listAllBuildings, siteCatalogAccessGranted]);

  const { pathname } = useLocation();
  const insideFleetShell = isPathScopable(pathname);

  // Get filter and sort from URL - memoize to avoid recreating on every render
  const [searchParams] = useSearchParams();
  const urlFilter = useMemo(() => parseFilterFromURL(searchParams), [searchParams]);
  // currentFilter folds the SitePicker's active site into the URL filter so
  // every downstream query (list, count, auth-needed, CSV) is scoped.
  const currentScopedFilter = useMemo(
    () => applySiteScopeToMinerFilter(urlFilter, activeSiteIds, activeIncludeUnassigned),
    [urlFilter, activeSiteIds, activeIncludeUnassigned],
  );
  const currentFilter = currentScopedFilter.filter;
  const siteScopeMatchesNoRows = currentScopedFilter.matchNone;
  const currentSortConfig = useMemo(() => parseSortFromURL(searchParams) ?? DEFAULT_SORT_CONFIG, [searchParams]);

  // Convert proto SortField to MinerColumn for UI component
  const currentSort = useMemo(() => {
    if (!currentSortConfig) return undefined;
    const column = getColumnForSortField(currentSortConfig.field);
    if (!column) return undefined;
    return {
      field: column,
      direction: currentSortConfig.direction === SortDirection.ASC ? SORT_ASC : SORT_DESC,
    } as const;
  }, [currentSortConfig]);

  // Count of miners requiring authentication. Refetched in the polling loop
  // below so the bulk-action gate releases promptly after a fleet-wide auth
  // resolves (otherwise the count is stale-positive until the next manual
  // refresh). `hasInitialLoadCompleted` is sticky across refetches, so it
  // alone can't tell us the count matches the current filter — combine with
  // `isLoading` to gate while any refetch is in flight.
  const {
    totalMiners: totalAuthNeededMiners,
    refetch: refetchAuthNeededMiners,
    hasInitialLoadCompleted: totalAuthNeededMinersInitialLoadCompleted,
    isLoading: totalAuthNeededMinersLoading,
  } = useAuthNeededMiners({
    pageSize: 1,
    filter: currentFilter,
    enabled: !siteScopeMatchesNoRows,
  });
  const totalAuthNeededMinersFresh = totalAuthNeededMinersInitialLoadCompleted && !totalAuthNeededMinersLoading;
  const { exportCsv, isExportingCsv } = useExportMinerListCsv({
    filter: currentFilter,
  });

  // Fetch unfiltered total count for the "X of Y miners" header display.
  // "Unfiltered" here means "no chip / saved-view filter applied"; the
  // active-site path scope still applies so Y reflects the current site.
  // When a `?site=` list filter is also present, the denominator follows
  // the same scope ∩ filter rule as the visible rows.
  const siteScopedTotalFilter = useMemo(() => {
    const urlSiteFilter =
      urlFilter && ((urlFilter.siteIds.length ?? 0) > 0 || urlFilter.includeUnassigned)
        ? create(MinerListFilterSchema, {
            siteIds: urlFilter.siteIds,
            includeUnassigned: urlFilter.includeUnassigned,
          })
        : undefined;
    return applySiteScopeToMinerFilter(urlSiteFilter, activeSiteIds, activeIncludeUnassigned).filter;
  }, [urlFilter, activeSiteIds, activeIncludeUnassigned]);
  const { totalMiners: totalUnfilteredMiners, refreshCurrentPage: refreshUnfilteredCount } = useFleet({
    pageSize: 1,
    filter: siteScopedTotalFilter,
    pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES,
    enabled: !siteScopeMatchesNoRows,
  });

  // Fetch all devices (both paired and unpaired) with a single API call
  const {
    minerIds,
    miners,
    totalMiners,
    hasMore,
    hasInitialLoadCompleted,
    refetch,
    refreshCurrentPage,
    updateMinerWorkerName,
    mergeMiners,
    availableModels,
    availableFirmwareVersions,
    currentPage,
    hasPreviousPage,
    goToNextPage,
    goToPrevPage,
  } = useFleet({
    pageSize: MINERS_PAGE_SIZE,
    filter: currentFilter,
    sort: currentSortConfig,
    pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES,
    enabled: !siteScopeMatchesNoRows,
  });

  // Fetch errors for all loaded miners
  const { errorsByDevice, hasLoaded: errorsLoaded, refetch: refetchErrors } = useDeviceErrors(minerIds);

  // Batch operations (ephemeral UI state)
  const {
    completeBatchOperation,
    removeDevicesFromBatch,
    cleanupStaleBatches,
    getAllBatches,
    getActiveBatches,
    batchStateVersion,
  } = useBatchOperations();

  // Poll for miner and error updates to keep data fresh on the current page.
  // Both are needed: minerIds is stabilized (same-content → same reference),
  // so the useDeviceErrors effect won't re-fire from polling alone.
  useEffect(() => {
    if (!hasInitialLoadCompleted) return;
    const intervalId = setInterval(() => {
      refreshCurrentPage();
      refetchErrors();
      refetchAuthNeededMiners();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasInitialLoadCompleted, refreshCurrentPage, refetchErrors, refetchAuthNeededMiners]);

  // Cleanup stale batch operations at the same interval as polling
  useEffect(() => {
    const interval = setInterval(() => {
      cleanupStaleBatches();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [cleanupStaleBatches]);

  // Remove devices from batches once they've reached expected status.
  // Only checks visible devices (useFleet keeps one page in memory).
  // Off-page devices stay in the batch until they become visible or stale cleanup runs.
  useEffect(() => {
    for (const batch of getAllBatches()) {
      const transitionedIds = batch.deviceIdentifiers.filter((id) => {
        const miner = miners[id];
        return miner && hasReachedExpectedStatus(batch.action, miner.deviceStatus, batch.startedAt);
      });
      if (transitionedIds.length === 0) continue;

      if (transitionedIds.length === batch.deviceIdentifiers.length) {
        // All devices transitioned — complete the entire batch
        completeBatchOperation(batch.batchIdentifier);
      } else {
        // Only some devices transitioned — remove them, keep batch for the rest
        removeDevicesFromBatch(batch.batchIdentifier, transitionedIds);
      }
    }
  }, [miners, batchStateVersion, getAllBatches, completeBatchOperation, removeDevicesFromBatch]);

  // Chrome-level coordination: CompleteSetup lives in FleetLayout and pulses
  // these timestamps. We forward our pairing completion up, and refetch when
  // an in-banner flow (e.g. pool assignment) signals the miner list is stale.
  const availableSites = useMemo<FilterLabelSource[]>(
    () =>
      (sites ?? [])
        .filter((row) => row.site !== undefined)
        .map((row) => ({ id: row.site!.id.toString(), label: row.site!.name })),
    [sites],
  );

  // Push our DeviceSet metadata up to FleetLayout so the saved-view modal
  // (mounted in the top tab strip) can show human-readable labels for any
  // group/rack ids referenced by an active filter.
  useEffect(() => {
    publishViewFilterContext({
      availableGroups,
      availableRacks,
      availableBuildings: readableAvailableBuildings,
      availableSites,
    });
  }, [publishViewFilterContext, availableGroups, availableRacks, readableAvailableBuildings, availableSites]);

  const refetchAll = useCallback(() => {
    refetch();
    refetchErrors();
    refreshUnfilteredCount();
    refetchAuthNeededMiners();
  }, [refetch, refetchErrors, refreshUnfilteredCount, refetchAuthNeededMiners]);

  const refreshVisibleRows = useCallback(() => {
    refreshCurrentPage();
    refetchErrors();
    refetchAuthNeededMiners();
  }, [refreshCurrentPage, refetchErrors, refetchAuthNeededMiners]);

  useEffect(() => {
    if (minersChangedAt > 0) refetchAll();
  }, [minersChangedAt, refetchAll]);

  const [showAddMinersModal, setShowAddMinersModal] = useState(false);

  const handleAddMinersClose = () => {
    refetchAll();
    notifyPairingCompleted();
    setShowAddMinersModal(false);
  };

  const handleSort = useCallback(
    (column: MinerColumn, direction: "asc" | "desc") => {
      const sortField = getSortField(column);
      if (!sortField) return;

      const sortDirection = direction === SORT_ASC ? SortDirection.ASC : SortDirection.DESC;
      const newSortConfig = create(SortConfigSchema, { field: sortField, direction: sortDirection });

      // Update URL with new sort params (preserves existing filter params)
      const params = new URLSearchParams(searchParams);
      encodeSortToURL(params, newSortConfig);
      navigate(`?${params.toString()}`, { replace: true });
    },
    [searchParams, navigate],
  );

  return (
    <>
      <ErrorBoundary>
        <MinerList
          title={insideFleetShell ? undefined : "Miners"}
          minerIds={siteScopeMatchesNoRows ? [] : minerIds}
          miners={siteScopeMatchesNoRows ? {} : miners}
          errorsByDevice={siteScopeMatchesNoRows ? {} : errorsByDevice}
          errorsLoaded={siteScopeMatchesNoRows ? true : errorsLoaded}
          getActiveBatches={getActiveBatches}
          batchStateVersion={batchStateVersion}
          totalMiners={siteScopeMatchesNoRows ? 0 : totalMiners}
          totalUnfilteredMiners={siteScopeMatchesNoRows ? 0 : totalUnfilteredMiners}
          totalDisabledMiners={siteScopeMatchesNoRows ? 0 : totalAuthNeededMiners}
          totalDisabledMinersFresh={siteScopeMatchesNoRows ? true : totalAuthNeededMinersFresh}
          paddingLeft={{
            phone: "24px",
            tablet: "24px",
            laptop: "40px",
            desktop: "40px",
          }}
          // Fleet shell owns the scroll: page scrolls, sticky header pins to it.
          overflowContainer={false}
          onAddMiners={() => setShowAddMinersModal(true)}
          loading={siteScopeMatchesNoRows ? false : !hasInitialLoadCompleted}
          pageSize={MINERS_PAGE_SIZE}
          currentPage={currentPage}
          hasPreviousPage={hasPreviousPage}
          hasNextPage={siteScopeMatchesNoRows ? false : hasMore}
          onNextPage={goToNextPage}
          onPrevPage={goToPrevPage}
          currentSort={currentSort}
          onSort={handleSort}
          availableModels={availableModels}
          availableFirmwareVersions={availableFirmwareVersions}
          availableGroups={availableGroups}
          availableRacks={availableRacks}
          availableSites={availableSites}
          availableBuildings={readableAvailableBuildings}
          currentFilter={currentFilter}
          currentSortConfig={currentSortConfig}
          onExportCsv={siteScopeMatchesNoRows ? () => undefined : exportCsv}
          exportCsvLoading={siteScopeMatchesNoRows ? false : isExportingCsv}
          onRefetchMiners={notifyMinersChanged}
          onRefreshMinersComplete={refreshVisibleRows}
          onWorkerNameUpdated={updateMinerWorkerName}
          onMergeMiners={mergeMiners}
          onPairingCompleted={notifyPairingCompleted}
        />
      </ErrorBoundary>

      {showAddMinersModal ? (
        <Miners
          mode="pairing"
          onExit={handleAddMinersClose}
          pairedMinerIds={minerIds}
          onPairingCompleted={notifyPairingCompleted}
          onRefetchMiners={refetchAll}
        />
      ) : null}
    </>
  );
};

export default Fleet;
