import { useMemo } from "react";
import PoolSelectionPage from "./PoolSelectionPage";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import type { MinerSelection } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";

interface PoolSelectionPageWrapperProps {
  selectedMiners: MinerSelection[];
  onSuccess: (batchIdentifier: string) => void;
  onError: (error: string) => void;
  onDismiss: () => void;
}

const PoolSelectionPageWrapper = ({
  selectedMiners,
  onSuccess,
  onError,
  onDismiss: onDismiss,
}: PoolSelectionPageWrapperProps) => {
  const { updateMiningPools } = useMinerCommand();

  const deviceIdentifiers = useMemo(() => selectedMiners.map((m) => m.deviceIdentifier), [selectedMiners]);

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
