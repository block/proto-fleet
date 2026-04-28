import { useMemo } from "react";
import PoolSelectionPage from "./PoolSelectionPage";
import { PoolConfig, useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import type { MinerSelection } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import {
  createDeviceSelector,
  type DeviceFilterCriteria,
} from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { type SelectionMode } from "@/shared/components/List";

interface PoolSelectionPageWrapperProps {
  open?: boolean;
  selectionMode: SelectionMode;
  poolNeededCount?: number; // For "all" mode with filter
  filterCriteria?: DeviceFilterCriteria; // For "all" mode with filter
  selectedMiners?: MinerSelection[]; // For "subset" mode
  userUsername?: string;
  userPassword?: string;
  onSuccess: (batchIdentifier: string) => void;
  onError?: (error: string) => void;
  onDismiss: () => void;
}

const PoolSelectionPageWrapper = ({
  open,
  selectionMode,
  poolNeededCount,
  filterCriteria,
  selectedMiners,
  userUsername,
  userPassword,
  onSuccess,
  onError,
  onDismiss: onDismiss,
}: PoolSelectionPageWrapperProps) => {
  const { updateMiningPools } = useMinerCommand();

  const deviceIdentifiers = useMemo(
    () => (selectedMiners ? selectedMiners.map((m) => m.deviceIdentifier) : []),
    [selectedMiners],
  );

  const deviceSelector = useMemo(
    () =>
      selectionMode === "none" ? undefined : createDeviceSelector(selectionMode, deviceIdentifiers, filterCriteria),
    [selectionMode, deviceIdentifiers, filterCriteria],
  );

  const handleAssignPools = async (poolConfig: PoolConfig) => {
    if (!deviceSelector) return;
    await updateMiningPools({
      deviceSelector,
      poolConfig,
      userUsername: userUsername || "",
      userPassword: userPassword || "",
      onSuccess: (response) => {
        onSuccess(response.batchIdentifier);
        onDismiss();
      },
      onError: (error) => {
        console.error("Failed to assign pools:", error);
        onError?.(error);
        onDismiss();
      },
    });
  };

  return (
    <PoolSelectionPage
      open={open}
      deviceIdentifiers={deviceIdentifiers}
      numberOfMiners={selectionMode === "all" ? poolNeededCount : deviceIdentifiers.length}
      currentDevice={deviceIdentifiers.length === 1 ? deviceIdentifiers[0] : null}
      onAssignPools={handleAssignPools}
      onDismiss={onDismiss}
    />
  );
};

export default PoolSelectionPageWrapper;
