import { useState } from "react";
import useFleet from "@/protoFleet/api/useFleet";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";
import CompleteSetup from "@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup";
import Miners from "@/protoFleet/features/onboarding/components/Miners";
import Button, { sizes, variants } from "@/shared/components/Button";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const Fleet = () => {
  const { minerIds, hasMore, isLoading, loadMore, setFilter } = useFleet({
    pageSize: 100,
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
          paddingLeft={{
            phone: "24px",
            tablet: "24px",
            laptop: "40px",
            desktop: "40px",
          }}
          overflowContainer={false}
          onFilterChange={setFilter}
          onAddMiners={() => setShowAddMinersModal(true)}
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
