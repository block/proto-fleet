import { useState } from "react";
import PoolsList from "./PoolsList/PoolsList";
import { MiningPool } from "./types";
import { Dismiss } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";

interface PoolSelectionPageProps {
  deviceIdentifiers: string[];
  availablePools: MiningPool[];
  onAssignPools: (
    defaultPoolId: string | undefined,
    backup1PoolId: string | undefined,
    backup2PoolId: string | undefined,
  ) => Promise<void>;
  onDismiss: () => void;
}

const PoolSelectionPage = ({
  deviceIdentifiers,
  availablePools,
  onAssignPools,
  onDismiss: onCancel,
}: PoolSelectionPageProps) => {
  const [selectedDefaultPool, setSelectedDefaultPool] = useState<string | undefined>();
  const [selectedBackupPools, setSelectedBackupPools] = useState<[string | undefined, string | undefined]>([
    undefined,
    undefined,
  ]);

  const handleSelectDefaultPool = (poolId: string) => {
    setSelectedDefaultPool(poolId);
  };

  const handleSelectBackupPool = (poolId: string, poolIndex: number) => {
    setSelectedBackupPools((prev) => {
      const newBackupPools: [string | undefined, string | undefined] =
        poolIndex === 0 ? [poolId, prev[1]] : [prev[0], poolId];
      return newBackupPools;
    });
  };

  const handleAssignPoolsClick = async () => {
    try {
      await onAssignPools(selectedDefaultPool, selectedBackupPools[0], selectedBackupPools[1]);
    } catch (error) {
      console.error("Failed to assign pools:", error);
    }
  };

  const numberOfMiners = deviceIdentifiers.length;
  const buttonText = `Assign to ${numberOfMiners} miner${numberOfMiners === 1 ? "" : "s"}`;

  return (
    <PageOverlay show>
      <div className="h-full w-full overflow-auto bg-surface-base p-6">
        <Header
          className="sticky top-0 z-10 pb-14"
          title="Assign pools"
          titleSize="text-heading-200"
          icon={<Dismiss />}
          iconOnClick={onCancel}
          inline
          buttonSize={sizes.base}
          buttons={[
            {
              text: buttonText,
              variant: variants.primary,
              onClick: handleAssignPoolsClick,
              disabled: !selectedDefaultPool,
            },
          ]}
        />

        <div className="mx-auto max-w-4xl">
          <div className="flex flex-col gap-6">
            {/* Page header */}
            <div className="flex flex-col gap-1">
              <h1 className="text-heading-300 text-text-primary">Assign pools to miners</h1>
              <p className="text-body-300 text-text-secondary">
                Your hashrate will contribute to your default mining pool. Add backup pools in case your default pool
                fails. Worker names are automatically assigned based on the miner name defined in Fleet.
              </p>
            </div>

            {/* Cards container */}
            <div className="flex flex-col gap-4">
              {/* Default pool - full width */}
              <PoolsList
                title="Default pool"
                subtitle=""
                availablePools={availablePools}
                onSelect={handleSelectDefaultPool}
                createNewLabel="Add pool"
                excludedPoolIds={selectedBackupPools}
              />

              {/* Backup pools - side by side */}
              <div className="flex gap-4">
                {[0, 1].map((index) => {
                  const otherBackupIndex = index === 0 ? 1 : 0;
                  const excludedPools = [selectedDefaultPool, selectedBackupPools[otherBackupIndex]];

                  return (
                    <div key={index} className="flex-1">
                      <PoolsList
                        title={`Backup pool #${index + 1}`}
                        subtitle="Optional"
                        availablePools={availablePools}
                        onSelect={(poolId) => handleSelectBackupPool(poolId, index)}
                        createNewLabel="Add pool"
                        poolNumber={index + 1}
                        excludedPoolIds={excludedPools}
                      />
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      </div>
    </PageOverlay>
  );
};

export default PoolSelectionPage;
