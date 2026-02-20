import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import {
  type MinerSortConfig,
  MinerSortConfigSchema,
  PairingStatus,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import useBatchTelemetry from "@/protoFleet/api/useBatchTelemetry";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import useFleet from "@/protoFleet/api/useFleet";
import { useStreamDeviceErrors } from "@/protoFleet/api/useStreamDeviceErrors";
import useStreamMinerListUpdates from "@/protoFleet/api/useStreamMinerListUpdates";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";
import { type MinerColumn } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { MINERS_PAGE_SIZE } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import {
  getColumnForSortField,
  getSortField,
} from "@/protoFleet/features/fleetManagement/components/MinerList/sortConfig";
import { parseFilterFromURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { encodeSortToURL, parseSortFromURL } from "@/protoFleet/features/fleetManagement/utils/sortUrlParams";
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import Miners from "@/protoFleet/features/onboarding/components/Miners";
import { useVisibleMiners } from "@/protoFleet/hooks";
import {
  useBatchOperationCount,
  useCleanupStaleBatches,
  useLastPairingCompletedAt,
  useNotifyPairingCompleted,
} from "@/protoFleet/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import { SORT_ASC, SORT_DESC } from "@/shared/components/List/types";

// Stable reference to prevent re-renders
const FLEET_PAIRING_STATUSES = [PairingStatus.PAIRED, PairingStatus.AUTHENTICATION_NEEDED];

// Default sort: Name ascending (alphabetical A-Z)
const DEFAULT_SORT_CONFIG: MinerSortConfig = create(MinerSortConfigSchema, {
  field: SortField.NAME,
  direction: SortDirection.ASC,
});

const Fleet = () => {
  const navigate = useNavigate();
  const { visibleMinerIds, registerMiner } = useVisibleMiners({
    rootMargin: "100px", // Preload telemetry for miners 100px before they enter viewport
    debounceMs: 300, // Debounce visibility updates during scroll
  });

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

  // Fetch unfiltered total count for the "X of Y miners" header display
  const { totalMiners: totalUnfilteredMiners } = useFleet({
    scope: "local",
    pageSize: 1,
    pairingStatuses: FLEET_PAIRING_STATUSES,
  });

  // Fetch all devices (both paired and unpaired) with a single API call
  // Metadata only - telemetry is fetched separately via useBatchTelemetry for visible miners
  const {
    minerIds,
    totalMiners,
    hasMore,
    hasInitialLoadCompleted,
    refetch,
    availableModels,
    currentPage,
    hasPreviousPage,
    goToNextPage,
    goToPrevPage,
  } = useFleet({
    scope: "global",
    pageSize: MINERS_PAGE_SIZE,
    visibleMinerIds,
    filter: currentFilter,
    sort: currentSortConfig,
    pairingStatuses: FLEET_PAIRING_STATUSES,
  });

  const { fetchBatchTelemetry, resetFetchedIds } = useBatchTelemetry();

  // Reset telemetry cache when refetch completes (e.g., after delete)
  const prevHasInitialLoadCompletedRef = useRef(hasInitialLoadCompleted);
  useEffect(() => {
    if (hasInitialLoadCompleted && !prevHasInitialLoadCompletedRef.current) {
      resetFetchedIds();
    }
    prevHasInitialLoadCompletedRef.current = hasInitialLoadCompleted;
  }, [hasInitialLoadCompleted, resetFetchedIds]);

  // Fetch and stream errors for all loaded miners
  useDeviceErrors(minerIds);
  useStreamDeviceErrors({
    deviceIds: minerIds,
    enabled: hasInitialLoadCompleted && minerIds.length > 0,
  });

  useEffect(() => {
    if (hasInitialLoadCompleted && visibleMinerIds.size > 0) {
      fetchBatchTelemetry(visibleMinerIds);
    }
  }, [visibleMinerIds, hasInitialLoadCompleted, fetchBatchTelemetry]);

  useEffect(() => {
    resetFetchedIds();
  }, [currentFilter, resetFetchedIds]);

  // Reset telemetry cache and refetch when pairing status changes (e.g., after authentication)
  const lastPairingCompletedAt = useLastPairingCompletedAt();
  useEffect(() => {
    if (lastPairingCompletedAt > 0) {
      resetFetchedIds();
      // Immediately refetch telemetry for visible miners after cache reset
      if (hasInitialLoadCompleted && visibleMinerIds.size > 0) {
        fetchBatchTelemetry(visibleMinerIds);
      }
    }
  }, [lastPairingCompletedAt, resetFetchedIds, hasInitialLoadCompleted, visibleMinerIds, fetchBatchTelemetry]);

  useStreamMinerListUpdates({
    filter: currentFilter,
    sort: currentSortConfig,
  });

  // Reset telemetry cache when batch operations complete to refetch fresh status data
  const batchOperationCount = useBatchOperationCount();
  const prevBatchCountRef = useRef(batchOperationCount);
  useEffect(() => {
    // When batch count decreases, a batch completed - reset cache to get fresh telemetry
    if (batchOperationCount < prevBatchCountRef.current) {
      resetFetchedIds();
      // Immediately refetch for visible miners
      if (visibleMinerIds.size > 0) {
        fetchBatchTelemetry(visibleMinerIds);
      }
    }
    prevBatchCountRef.current = batchOperationCount;
  }, [batchOperationCount, resetFetchedIds, visibleMinerIds, fetchBatchTelemetry]);

  // Cleanup stale batch operations every minute
  const cleanupStaleBatches = useCleanupStaleBatches();
  useEffect(() => {
    const interval = setInterval(() => {
      cleanupStaleBatches();
    }, 60000); // Check every minute

    return () => clearInterval(interval);
  }, [cleanupStaleBatches]);

  const notifyPairingCompleted = useNotifyPairingCompleted();
  const [showAddMinersModal, setShowAddMinersModal] = useState(false);

  const handleAddMinersClose = () => {
    // Refetch fleet data to show newly paired miners
    // The refetchFleet() call in MinersWrapper should have already triggered this,
    // but we call it again here to ensure data freshness when modal closes
    refetch();
    // Reset telemetry cache to allow re-fetching for all miners
    resetFetchedIds();
    // Notify store that pairing operations completed
    // This signals CompleteSetup to refetch auth-needed count
    notifyPairingCompleted();
    setShowAddMinersModal(false);
  };

  const handleSort = useCallback(
    (column: MinerColumn, direction: "asc" | "desc") => {
      const sortField = getSortField(column);
      if (!sortField) return;

      const sortDirection = direction === SORT_ASC ? SortDirection.ASC : SortDirection.DESC;
      const newSortConfig = create(MinerSortConfigSchema, { field: sortField, direction: sortDirection });

      // Update URL with new sort params (preserves existing filter params)
      const params = new URLSearchParams(searchParams);
      encodeSortToURL(params, newSortConfig);
      navigate(`?${params.toString()}`, { replace: true });
    },
    [searchParams, navigate],
  );

  return (
    <>
      <CompleteSetup className="sticky left-0 mb-10 max-w-full px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6" />
      <ErrorBoundary>
        <MinerList
          title="Miners"
          minerIds={minerIds}
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
          itemRef={registerMiner}
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
          currentFilter={currentFilter}
        />
      </ErrorBoundary>

      {showAddMinersModal && <Miners mode="pairing" onExit={handleAddMinersClose} />}
    </>
  );
};

export default Fleet;
