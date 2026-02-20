import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { POLL_INTERVAL_MS } from "./constants";
import {
  type MinerSortConfig,
  MinerSortConfigSchema,
  PairingStatus,
  SortDirection,
  SortField,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import useFleet from "@/protoFleet/api/useFleet";
import { useStreamDeviceErrors } from "@/protoFleet/api/useStreamDeviceErrors";
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
import { useCleanupStaleBatches, useNotifyPairingCompleted } from "@/protoFleet/store";
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
  const {
    minerIds,
    totalMiners,
    hasMore,
    hasInitialLoadCompleted,
    refetch,
    refreshCurrentPage,
    availableModels,
    currentPage,
    hasPreviousPage,
    goToNextPage,
    goToPrevPage,
  } = useFleet({
    scope: "global",
    pageSize: MINERS_PAGE_SIZE,
    filter: currentFilter,
    sort: currentSortConfig,
    pairingStatuses: FLEET_PAIRING_STATUSES,
  });

  // Poll for updates to keep data fresh on the current page
  useEffect(() => {
    if (!hasInitialLoadCompleted) return;
    const intervalId = setInterval(() => {
      refreshCurrentPage();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasInitialLoadCompleted, refreshCurrentPage]);

  // Fetch and stream errors for all loaded miners
  useDeviceErrors(minerIds);
  useStreamDeviceErrors({
    deviceIds: minerIds,
    enabled: hasInitialLoadCompleted && minerIds.length > 0,
  });

  // Cleanup stale batch operations at the same interval as polling
  const cleanupStaleBatches = useCleanupStaleBatches();
  useEffect(() => {
    const interval = setInterval(() => {
      cleanupStaleBatches();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [cleanupStaleBatches]);

  const notifyPairingCompleted = useNotifyPairingCompleted();
  const [showAddMinersModal, setShowAddMinersModal] = useState(false);

  const handleAddMinersClose = () => {
    refetch();
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
