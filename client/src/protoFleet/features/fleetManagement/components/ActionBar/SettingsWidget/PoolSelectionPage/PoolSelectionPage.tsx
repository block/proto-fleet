import { useEffect, useRef, useState } from "react";
import PoolsList from "./PoolsList/PoolsList";
import useMinerPoolAssignments from "@/protoFleet/api/useMinerPoolAssignments";
import { Dismiss } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import ProgressCircular from "@/shared/components/ProgressCircular";

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
  const { fetchPoolAssignments, isLoading: isLoadingAssignments } = useMinerPoolAssignments();

  // Track which device we've loaded assignments for to handle device changes
  const loadedDeviceRef = useRef<string | null>(null);

  // When editing a single miner, fetch and pre-populate current pool assignments
  // Also handles resetting state when device selection changes
  useEffect(() => {
    const currentDevice = deviceIdentifiers.length === 1 ? deviceIdentifiers[0] : null;

    // Skip if we've already loaded for this device
    if (loadedDeviceRef.current === currentDevice) {
      return;
    }

    const isDeviceChange = loadedDeviceRef.current !== null;

    const loadExistingPoolAssignments = async () => {
      // Reset selections when switching devices (but not on initial mount)
      if (isDeviceChange) {
        setSelectedDefaultPool(undefined);
        setSelectedBackupPools([undefined, undefined]);
      }

      if (!currentDevice) {
        loadedDeviceRef.current = currentDevice;
        return;
      }

      const assignments = await fetchPoolAssignments(currentDevice);
      if (assignments) {
        if (assignments.defaultPoolId !== undefined) {
          setSelectedDefaultPool(assignments.defaultPoolId.toString());
        }
        const backup1 = assignments.backup1PoolId !== undefined ? assignments.backup1PoolId.toString() : undefined;
        const backup2 = assignments.backup2PoolId !== undefined ? assignments.backup2PoolId.toString() : undefined;
        setSelectedBackupPools([backup1, backup2]);
      }
      loadedDeviceRef.current = currentDevice;
    };

    loadExistingPoolAssignments();
  }, [deviceIdentifiers, fetchPoolAssignments]);

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
    const previousDefaultPool = selectedDefaultPool;

    setSelectedBackupPools((prev) => {
      if (prev[0] === poolId) {
        return [previousDefaultPool, prev[1]];
      }
      if (prev[1] === poolId) {
        return [prev[0], previousDefaultPool];
      }
      return prev;
    });

    setSelectedDefaultPool(poolId);
  };

  const handleSelectBackupPool = (poolId: string, poolIndex: number) => {
    const currentBackup = selectedBackupPools[poolIndex];

    if (poolId === selectedDefaultPool) {
      if (currentBackup !== undefined) {
        setSelectedDefaultPool(currentBackup);
        setSelectedBackupPools((prev) => (poolIndex === 0 ? [poolId, prev[1]] : [prev[0], poolId]));
      }
      return;
    }

    const otherBackupIndex = poolIndex === 0 ? 1 : 0;
    if (poolId === selectedBackupPools[otherBackupIndex]) {
      if (currentBackup !== undefined) {
        setSelectedBackupPools((prev) => (poolIndex === 0 ? [poolId, prev[poolIndex]] : [prev[poolIndex], poolId]));
      }
      return;
    }

    setSelectedBackupPools((prev) => (poolIndex === 0 ? [poolId, prev[1]] : [prev[0], poolId]));
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
  const isSingleMinerEdit = numberOfMiners === 1;
  const isLoadingInitialState = isSingleMinerEdit && isLoadingAssignments;

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
              disabled: !selectedDefaultPool || isLoadingInitialState,
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
            {isLoadingInitialState ? (
              <div className="flex flex-col items-center justify-center gap-3 py-16">
                <ProgressCircular size={32} indeterminate />
                <span className="text-body-300 text-text-secondary">Loading pool configuration...</span>
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                {/* Default pool - full width */}
                <PoolsList
                  title="Default pool"
                  subtitle=""
                  onSelect={handleSelectDefaultPool}
                  createNewLabel="Add pool"
                  testId="default-pool"
                  selectedPoolId={selectedDefaultPool}
                  poolAssignments={poolAssignments}
                />

                {/* Backup pools - side by side */}
                <div className="flex gap-4">
                  {[0, 1].map((index) => {
                    const isDisabled =
                      index === 0 ? !selectedDefaultPool : !selectedDefaultPool || !selectedBackupPools[0];
                    const otherBackupIndex = index === 0 ? 1 : 0;
                    const excludedPools = selectedBackupPools[index]
                      ? []
                      : [selectedDefaultPool, selectedBackupPools[otherBackupIndex]];

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
            )}
          </div>
        </div>
      </div>
    </PageOverlay>
  );
};

export default PoolSelectionPage;
