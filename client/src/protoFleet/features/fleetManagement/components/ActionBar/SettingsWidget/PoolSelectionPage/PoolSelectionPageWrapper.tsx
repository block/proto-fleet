import PoolSelectionPage from "./PoolSelectionPage";
import type { MiningPool } from "./types";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import usePools from "@/protoFleet/api/usePools";

interface PoolSelectionPageWrapperProps {
  deviceIdentifiers: string[];
  onSuccess: (batchIdentifier: string) => void;
  onError: (error: string) => void;
  onDismiss: () => void;
}

const PoolSelectionPageWrapper = ({
  deviceIdentifiers,
  onSuccess,
  onError,
  onDismiss: onDismiss,
}: PoolSelectionPageWrapperProps) => {
  const { pools } = usePools();
  const { updateMiningPools } = useMinerCommand();

  const availablePools: MiningPool[] = pools.map((pool) => ({
    poolId: pool.poolId.toString(),
    name: pool.poolName,
    poolUrl: pool.url,
    username: pool.username,
  }));

  const handleAssignPools = async (
    defaultPoolId: string | undefined,
    backup1PoolId: string | undefined,
    backup2PoolId: string | undefined,
  ) => {
    await updateMiningPools({
      deviceIdentifiers,
      poolConfig: {
        defaultPoolId,
        backup1PoolId,
        backup2PoolId,
      },
      onSuccess: (response) => {
        onSuccess(response.batchIdentifier);
        onDismiss();
      },
      onError: (error) => {
        console.error("Failed to assign pools:", error);
        onError("Failed to assign pools");
        onDismiss();
      },
    });
  };

  return (
    <PoolSelectionPage
      deviceIdentifiers={deviceIdentifiers}
      availablePools={availablePools}
      onAssignPools={handleAssignPools}
      onDismiss={onDismiss}
    />
  );
};

export default PoolSelectionPageWrapper;
