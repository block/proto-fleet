import { useMemo } from "react";
import PoolSelectionPage from "./PoolSelectionPage";
import { PoolConfig, useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import type { MinerSelection } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { type SelectionMode } from "@/shared/components/List";

interface PoolSelectionPageWrapperProps {
  selectedMiners: MinerSelection[];
  selectionMode: SelectionMode;
  onSuccess: (batchIdentifier: string) => void;
  onError: (error: string) => void;
  onDismiss: () => void;
}

const PoolSelectionPageWrapper = ({
  selectedMiners,
  selectionMode,
  onSuccess,
  onError,
  onDismiss: onDismiss,
}: PoolSelectionPageWrapperProps) => {
  const { updateMiningPools } = useMinerCommand();

  const deviceIdentifiers = useMemo(() => selectedMiners.map((m) => m.deviceIdentifier), [selectedMiners]);

  const deviceSelector = useMemo(
    () => (selectionMode === "none" ? undefined : createDeviceSelector(selectionMode, deviceIdentifiers)),
    [selectionMode, deviceIdentifiers],
  );

  const handleAssignPools = async (poolConfig: PoolConfig) => {
    if (!deviceSelector) return;
    await updateMiningPools({
      deviceSelector,
      poolConfig,
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
