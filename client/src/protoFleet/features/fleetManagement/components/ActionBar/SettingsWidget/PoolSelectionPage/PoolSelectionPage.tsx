import { useState } from "react";
import PoolsList from "./PoolsList/PoolsList";
import { Dismiss } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";

interface PoolSelectionPageProps {
  deviceIdentifiers: string[];
  onAssignPools: (
    defaultPoolId: string | undefined,
    backup1PoolId: string | undefined,
    backup2PoolId: string | undefined,
  ) => Promise<void>;
  onDismiss: () => void;
}

const PoolSelectionPage = ({ deviceIdentifiers, onAssignPools, onDismiss: onCancel }: PoolSelectionPageProps) => {
  const [selectedDefaultPool, setSelectedDefaultPool] = useState<string | undefined>();
  const [selectedBackupPools, setSelectedBackupPools] = useState<[string | undefined, string | undefined]>([
    undefined,
    undefined,
  ]);

  const poolAssignments: Record<string, string> = {};
  if (selectedDefaultPool) {
    poolAssignments[selectedDefaultPool] = "Default";
  }
  if (selectedBackupPools[0]) {
    poolAssignments[selectedBackupPools[0]] = "Backup #1";
  }
  if (selectedBackupPools[1]) {
    poolAssignments[selectedBackupPools[1]] = "Backup #2";
  }

  const handleSelectDefaultPool = (poolId: string) => {
    setSelectedDefaultPool(poolId);
    // Clear conflicting backup pools (and #2 if #1 is cleared since #2 depends on #1)
    setSelectedBackupPools((prev) => {
      const backup1Cleared = prev[0] === poolId;
      const newBackup1 = backup1Cleared ? undefined : prev[0];
      const newBackup2 = backup1Cleared || prev[1] === poolId ? undefined : prev[1];
      return [newBackup1, newBackup2];
    });
  };

  const handleSelectBackupPool = (poolId: string, poolIndex: number) => {
    setSelectedBackupPools((prev) => {
      if (poolIndex === 0) {
        return [poolId, prev[1] === poolId ? undefined : prev[1]];
      }
      return [prev[0], poolId];
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
                onSelect={handleSelectDefaultPool}
                createNewLabel="Add pool"
                excludedPoolIds={selectedBackupPools}
                testId="default-pool"
                selectedPoolId={selectedDefaultPool}
                poolAssignments={poolAssignments}
              />

              {/* Backup pools - side by side */}
              <div className="flex gap-4">
                {[0, 1].map((index) => {
                  const otherBackupIndex = index === 0 ? 1 : 0;
                  const excludedPools = [selectedDefaultPool, selectedBackupPools[otherBackupIndex]];
                  const isDisabled =
                    index === 0 ? !selectedDefaultPool : !selectedDefaultPool || !selectedBackupPools[0];

                  return (
                    <div key={index} className="flex-1">
                      <PoolsList
                        title={`Backup pool #${index + 1}`}
                        subtitle="Optional"
                        onSelect={(poolId) => handleSelectBackupPool(poolId, index)}
                        createNewLabel="Add pool"
                        poolNumber={index + 1}
                        excludedPoolIds={excludedPools}
                        testId={`backup-pool-${index + 1}`}
                        disabled={isDisabled}
                        selectedPoolId={selectedBackupPools[index]}
                        poolAssignments={poolAssignments}
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
