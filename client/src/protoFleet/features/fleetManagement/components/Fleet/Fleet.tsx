import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import useFleet from "@/protoFleet/api/useFleet";
import useStreamMinerListUpdates from "@/protoFleet/api/useStreamMinerListUpdates";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";
import { parseFilterFromURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import Miners from "@/protoFleet/features/onboarding/components/Miners";
import { useVisibleMiners } from "@/protoFleet/hooks";
import Button, { sizes, variants } from "@/shared/components/Button";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const Fleet = () => {
  // Track which miners are currently visible in viewport
  const { visibleMinerIds, registerMiner } = useVisibleMiners({
    rootMargin: "100px", // Preload telemetry for miners 100px before they enter viewport
    debounceMs: 300, // Debounce visibility updates during scroll
  });

  // Get filter from URL - memoize to avoid recreating on every render
  const [searchParams] = useSearchParams();
  const currentFilter = useMemo(
    () => parseFilterFromURL(searchParams),
    [searchParams],
  );

  // Fetch all devices (both paired and unpaired) with a single API call
  // Only subscribe to telemetry for visible miners
  const { minerIds, totalMiners, hasMore, isLoading, loadMore } = useFleet({
    scope: "global",
    pageSize: 100,
    visibleMinerIds,
    mode: "snapshot",
    filter: currentFilter,
  });

  // Stream incremental updates for the current filter
  useStreamMinerListUpdates({
    filter: currentFilter,
  });

  const [showAddMinersModal, setShowAddMinersModal] = useState(false);

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
          paddingLeft={{
            phone: "24px",
            tablet: "24px",
            laptop: "40px",
            desktop: "40px",
          }}
          overflowContainer={false}
          onAddMiners={() => setShowAddMinersModal(true)}
          itemRef={registerMiner}
        />
      </ErrorBoundary>

      {hasMore ? (
        <div className="mt-6 flex justify-center">
          <Button
            variant={variants.secondary}
            size={sizes.base}
            onClick={() => loadMore()}
            loading={isLoading}
            text="Load More"
          />
        </div>
      ) : null}

      {showAddMinersModal && (
        <Miners mode="pairing" onExit={() => setShowAddMinersModal(false)} />
      )}
    </>
  );
};

export default Fleet;
