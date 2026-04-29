import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { POLL_INTERVAL_MS } from "./constants";
import {
  type SortConfig,
  SortConfigSchema,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import useExportMinerListCsv from "@/protoFleet/api/useExportMinerListCsv";
import useFleet from "@/protoFleet/api/useFleet";
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
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import Miners from "@/protoFleet/features/onboarding/components/Miners";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import { SORT_ASC, SORT_DESC } from "@/shared/components/List/types";

// Default sort: Name ascending (alphabetical A-Z)
const DEFAULT_SORT_CONFIG: SortConfig = create(SortConfigSchema, {
  field: SortField.NAME,
  direction: SortDirection.ASC,
});

const Fleet = () => {
  const navigate = useNavigate();
  const { listGroups, listRacks } = useDeviceSets();
  const [availableGroups, setAvailableGroups] = useState<DeviceSet[]>([]);
  const [availableRacks, setAvailableRacks] = useState<DeviceSet[]>([]);

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
  }, [listGroups, listRacks]);

  // Get filter and sort from URL - memoize to avoid recreating on every render
  const [searchParams] = useSearchParams();
  const currentFilter = useMemo(() => parseFilterFromURL(searchParams), [searchParams]);
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

  // Get count of miners requiring authentication (disabled rows)
  const { totalMiners: totalAuthNeededMiners } = useAuthNeededMiners({ pageSize: 1, filter: currentFilter });
  const { exportCsv, isExportingCsv } = useExportMinerListCsv({
    filter: currentFilter,
  });

  // Fetch unfiltered total count for the "X of Y miners" header display
  // and to guard CompleteSetup rendering (hide when no miners are paired)
  const {
    totalMiners: totalUnfilteredMiners,
    refreshCurrentPage: refreshUnfilteredCount,
    hasInitialLoadCompleted: unfilteredCountLoaded,
  } = useFleet({
    pageSize: 1,
    pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES,
  });

  // Fetch all devices (both paired and unpaired) with a single API call
  const {
    minerIds,
    miners,
    totalMiners,
    hasMore,
    isLoading,
    hasInitialLoadCompleted,
    refetch,
    refreshCurrentPage,
    updateMinerWorkerName,
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
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasInitialLoadCompleted, refreshCurrentPage, refetchErrors]);

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

  // Pairing coordination (local state, replaces fleet slice)
  const [lastPairingCompletedAt, setLastPairingCompletedAt] = useState(0);
  const notifyPairingCompleted = useCallback(() => setLastPairingCompletedAt(Date.now()), []);

  const refetchAll = useCallback(() => {
    refetch();
    refreshUnfilteredCount();
  }, [refetch, refreshUnfilteredCount]);

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
      {!unfilteredCountLoaded || totalUnfilteredMiners > 0 || totalMiners > 0 ? (
        <CompleteSetup
          className="sticky left-0 mb-10 max-w-full px-6 pt-6 laptop:px-10 laptop:pt-10"
          lastPairingCompletedAt={lastPairingCompletedAt}
          onRefetchMiners={refetchAll}
          onPairingCompleted={notifyPairingCompleted}
        />
      ) : null}
      <ErrorBoundary>
        <MinerList
          title="Miners"
          minerIds={minerIds}
          miners={miners}
          errorsByDevice={errorsByDevice}
          errorsLoaded={errorsLoaded}
          getActiveBatches={getActiveBatches}
          batchStateVersion={batchStateVersion}
          totalMiners={totalMiners}
          totalUnfilteredMiners={totalUnfilteredMiners}
          totalDisabledMiners={totalAuthNeededMiners}
          paddingLeft={{
            phone: "24px",
            tablet: "24px",
            laptop: "40px",
            desktop: "40px",
          }}
          onAddMiners={() => setShowAddMinersModal(true)}
          loading={!hasInitialLoadCompleted}
          isRefetching={Boolean(hasInitialLoadCompleted && isLoading)}
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
          currentFilter={currentFilter}
          currentSortConfig={currentSortConfig}
          onExportCsv={exportCsv}
          exportCsvLoading={isExportingCsv}
          onRefetchMiners={refetchAll}
          onWorkerNameUpdated={updateMinerWorkerName}
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
