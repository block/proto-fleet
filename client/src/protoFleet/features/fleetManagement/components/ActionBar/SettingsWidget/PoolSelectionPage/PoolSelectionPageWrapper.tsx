import PoolSelectionPage from "./PoolSelectionPage";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";

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
  const { updateMiningPools } = useMinerCommand();

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
    <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={handleAssignPools} onDismiss={onDismiss} />
  );
};

export default PoolSelectionPageWrapper;
