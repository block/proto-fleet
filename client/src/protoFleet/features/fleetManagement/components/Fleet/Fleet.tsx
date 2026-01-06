import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import useBatchTelemetry from "@/protoFleet/api/useBatchTelemetry";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import useFleet from "@/protoFleet/api/useFleet";
import { useStreamDeviceErrors } from "@/protoFleet/api/useStreamDeviceErrors";
import useStreamMinerListUpdates from "@/protoFleet/api/useStreamMinerListUpdates";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";
import { parseFilterFromURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import Miners from "@/protoFleet/features/onboarding/components/Miners";
import { useVisibleMiners } from "@/protoFleet/hooks";
import { useNotifyPairingCompleted } from "@/protoFleet/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

// Stable reference to prevent re-renders
const FLEET_PAIRING_STATUSES = [PairingStatus.PAIRED, PairingStatus.AUTHENTICATION_NEEDED];

const Fleet = () => {
  const { visibleMinerIds, registerMiner } = useVisibleMiners({
    rootMargin: "100px", // Preload telemetry for miners 100px before they enter viewport
    debounceMs: 300, // Debounce visibility updates during scroll
  });

  // Get filter from URL - memoize to avoid recreating on every render
  const [searchParams] = useSearchParams();
  const currentFilter = useMemo(() => parseFilterFromURL(searchParams), [searchParams]);

  // Get count of miners requiring authentication (disabled rows)
  const { totalMiners: totalAuthNeededMiners } = useAuthNeededMiners({ pageSize: 1, filter: currentFilter });

  // Fetch all devices (both paired and unpaired) with a single API call
  // Metadata only - telemetry is fetched separately via useBatchTelemetry for visible miners
  const { minerIds, totalMiners, hasMore, isLoading, hasInitialLoadCompleted, loadMore, refetch } = useFleet({
    scope: "global",
    pageSize: 50,
    visibleMinerIds,
    filter: currentFilter,
    pairingStatuses: FLEET_PAIRING_STATUSES,
  });

  const { fetchBatchTelemetry, resetFetchedIds } = useBatchTelemetry();

  // Reset telemetry cache when refetch completes (e.g., after unpair)
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

  useStreamMinerListUpdates({
    filter: currentFilter,
  });

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

  return (
    <>
      <div className="sticky left-0 mb-10 max-w-full px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
        <CompleteSetup />
      </div>
      <ErrorBoundary>
        <MinerList
          title="Miners"
          minerIds={minerIds}
          totalMiners={totalMiners}
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
          onLoadMore={loadMore}
          hasMore={hasMore}
          isLoadingMore={isLoading}
        />
      </ErrorBoundary>

      {showAddMinersModal && <Miners mode="pairing" onExit={handleAddMinersClose} />}
    </>
  );
};

export default Fleet;
