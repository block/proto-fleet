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
import { siteFilterFromActive, useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
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
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import { SORT_ASC, SORT_DESC } from "@/shared/components/List/types";

// Default sort: Name ascending (alphabetical A-Z)
const DEFAULT_SORT_CONFIG: SortConfig = create(SortConfigSchema, {
  field: SortField.NAME,
  direction: SortDirection.ASC,
});

// Merge the SitePicker's active-site selection into the URL-derived
// MinerListFilter. URL wins: when the URL already pins specific
// site_ids or include_unassigned (e.g. saved-view chip), the topbar
// selection is ignored — otherwise the picker scopes every query
// (list, count, auth-needed gate, CSV export, "X of Y" denominator)
// through to ListDevices.
const mergeSiteFilter = (
  urlFilter: MinerListFilter | undefined,
  siteIds: bigint[],
  includeUnassigned: boolean,
): MinerListFilter | undefined => {
  const urlHasSiteFilter = (urlFilter?.siteIds.length ?? 0) > 0 || (urlFilter?.includeUnassigned ?? false);
  if (urlHasSiteFilter) return urlFilter;
  if (siteIds.length === 0 && !includeUnassigned) return urlFilter;
  return urlFilter
    ? create(MinerListFilterSchema, { ...urlFilter, siteIds, includeUnassigned })
    : create(MinerListFilterSchema, { siteIds, includeUnassigned });
};

const Fleet = () => {
  const navigate = useNavigate();
  const { listGroups, listRacks } = useDeviceSets();
  const { listAllBuildings } = useBuildings();
  const [availableGroups, setAvailableGroups] = useState<DeviceSet[]>([]);
  const [availableRacks, setAvailableRacks] = useState<DeviceSet[]>([]);
  const { sites } = useFleetOutletContext();
  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  const { siteIds: activeSiteIds, includeUnassigned: activeIncludeUnassigned } = useMemo(
    () => siteFilterFromActive(activeSite),
    [activeSite],
  );
  const [availableBuildings, setAvailableBuildings] = useState<FilterLabelSource[]>([]);

  useEffect(() => {
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
    listAllBuildings({
      onSuccess: (buildings) => {
        setAvailableBuildings(
          buildings
            .filter((row) => row.building !== undefined)
            .map((row) => ({ id: row.building!.id.toString(), label: row.building!.name })),
        );
      },
    });
  }, [listGroups, listRacks, listAllBuildings]);

  const { pathname } = useLocation();
  const insideFleetShell = pathname.startsWith("/fleet/");

  // Get filter and sort from URL - memoize to avoid recreating on every render
  const [searchParams] = useSearchParams();
  const urlFilter = useMemo(() => parseFilterFromURL(searchParams), [searchParams]);
  // currentFilter folds the SitePicker's active site into the URL filter so
  // every downstream query (list, count, auth-needed, CSV) is scoped.
  const currentFilter = useMemo(
    () => mergeSiteFilter(urlFilter, activeSiteIds, activeIncludeUnassigned),
    [urlFilter, activeSiteIds, activeIncludeUnassigned],
  );
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
  });
  const totalAuthNeededMinersFresh = totalAuthNeededMinersInitialLoadCompleted && !totalAuthNeededMinersLoading;
  const { exportCsv, isExportingCsv } = useExportMinerListCsv({
    filter: currentFilter,
  });

  // Fetch unfiltered total count for the "X of Y miners" header display.
  // "Unfiltered" here means "no chip / saved-view filter applied"; the
  // active-site scope still applies so Y reflects the current site
  // (otherwise switching sites would leave Y stuck at the org-wide total).
  // A `?site=` deep link wins over the picker — same precedence as the
  // list's currentFilter — so a deep-linked site view reports its own Y
  // rather than the org-wide or picker-site total.
  const siteScopedTotalFilter = useMemo(() => {
    const urlHasSiteFilter = (urlFilter?.siteIds.length ?? 0) > 0 || (urlFilter?.includeUnassigned ?? false);
    if (urlHasSiteFilter) {
      return create(MinerListFilterSchema, {
        siteIds: urlFilter!.siteIds,
        includeUnassigned: urlFilter!.includeUnassigned,
      });
    }
    return mergeSiteFilter(undefined, activeSiteIds, activeIncludeUnassigned);
  }, [urlFilter, activeSiteIds, activeIncludeUnassigned]);
  const { totalMiners: totalUnfilteredMiners, refreshCurrentPage: refreshUnfilteredCount } = useFleet({
    pageSize: 1,
    filter: siteScopedTotalFilter,
    pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES,
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
  const { sites, notifyPairingCompleted, minersChangedAt, publishViewFilterContext } = useFleetOutletContext();
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
    publishViewFilterContext({ availableGroups, availableRacks, availableBuildings, availableSites });
  }, [publishViewFilterContext, availableGroups, availableRacks, availableBuildings, availableSites]);

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
          minerIds={minerIds}
          miners={miners}
          errorsByDevice={errorsByDevice}
          errorsLoaded={errorsLoaded}
          getActiveBatches={getActiveBatches}
          batchStateVersion={batchStateVersion}
          totalMiners={totalMiners}
          totalUnfilteredMiners={totalUnfilteredMiners}
          totalDisabledMiners={totalAuthNeededMiners}
          totalDisabledMinersFresh={totalAuthNeededMinersFresh}
          paddingLeft={{
            phone: "24px",
            tablet: "24px",
            laptop: "40px",
            desktop: "40px",
          }}
          // Fleet shell owns the scroll: page scrolls, sticky header pins to it.
          overflowContainer={false}
          onAddMiners={() => setShowAddMinersModal(true)}
          loading={!hasInitialLoadCompleted}
          pageSize={MINERS_PAGE_SIZE}
          currentPage={currentPage}
          hasPreviousPage={hasPreviousPage}
          hasNextPage={hasMore}
          onNextPage={goToNextPage}
          onPrevPage={goToPrevPage}
          currentSort={currentSort}
          onSort={handleSort}
          availableModels={availableModels}
          availableFirmwareVersions={availableFirmwareVersions}
          availableGroups={availableGroups}
          availableRacks={availableRacks}
          availableSites={availableSites}
          availableBuildings={availableBuildings}
          currentFilter={currentFilter}
          currentSortConfig={currentSortConfig}
          onExportCsv={exportCsv}
          exportCsvLoading={isExportingCsv}
          onRefetchMiners={refetchAll}
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
