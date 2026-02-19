import React, { useState } from "react";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { ProtoFleetStatusModal } from "@/protoFleet/components/StatusModal";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import SingleMinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu";
import { useFleetStore, useMiner, useMinerDeviceStatus, useMinerName } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";

type MinerNameProps = {
  deviceIdentifier: string;
};

const MinerName = ({ deviceIdentifier }: MinerNameProps) => {
  const name = useMinerName(deviceIdentifier) || deviceIdentifier;
  const miner = useMiner(deviceIdentifier);
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");
  const [isStatusModalOpen, setStatusModalOpen] = useState(false);
  const [showFleetAuth, setShowFleetAuth] = useState(false);
  const [showPoolSelection, setShowPoolSelection] = useState(false);
  const [fleetCredentials, setFleetCredentials] = useState<{ username: string; password: string }>();

  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const errors = selectErrorsByDevice(deviceIdentifier);

  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatusFromStore === DeviceStatus.NEEDS_MINING_POOL;
  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors);
  const showPoolFlow = isStatusModalOpen && !needsAuthentication && needsMiningPool;

  const handleNameClick = (e: React.MouseEvent) => {
    const row = (e.currentTarget as HTMLElement).closest("tr");
    const checkbox = row?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (checkbox && !checkbox.disabled) {
      checkbox.dispatchEvent(
        new MouseEvent("click", {
          bubbles: true,
          cancelable: true,
          shiftKey: e.shiftKey,
          ctrlKey: e.ctrlKey,
          metaKey: e.metaKey,
        }),
      );
    }
  };

  const handleAlertClick = () => {
    setStatusModalOpen(true);
    if (needsMiningPool) {
      setShowFleetAuth(true);
    }
  };

  const handleFleetAuthenticated = (username: string, password: string) => {
    setFleetCredentials({ username, password });
    setShowFleetAuth(false);
    setShowPoolSelection(true);
  };

  const handleModalClose = () => {
    setStatusModalOpen(false);
    setShowFleetAuth(false);
    setShowPoolSelection(false);
    setFleetCredentials(undefined);
  };

  return (
    <div className="flex w-full items-center justify-between gap-3">
      <div>
        <button type="button" className="cursor-pointer" onClick={handleNameClick}>
          {name}
        </button>
      </div>
      <div className="flex items-center gap-2">
        {needsAttention && !needsAuthentication && (
          <button
            onClick={handleAlertClick}
            className="cursor-pointer transition-opacity hover:opacity-80"
            aria-label="View issues"
          >
            <Alert width="w-4" className="text-red-500" />
          </button>
        )}
        <SingleMinerActionsMenu deviceIdentifier={deviceIdentifier} disabled={needsAuthentication} />
      </div>
      {showPoolFlow && showFleetAuth && (
        <AuthenticateFleetModal
          show={showFleetAuth}
          purpose="pool"
          onAuthenticated={handleFleetAuthenticated}
          onDismiss={handleModalClose}
        />
      )}
      {showPoolFlow && showPoolSelection && fleetCredentials && (
        <PoolSelectionPageWrapper
          selectedMiners={[{ deviceIdentifier, deviceStatus: deviceStatusFromStore }]}
          selectionMode="subset"
          userUsername={fleetCredentials.username}
          userPassword={fleetCredentials.password}
          onSuccess={handleModalClose}
          onError={handleModalClose}
          onDismiss={handleModalClose}
        />
      )}
      {isStatusModalOpen && !needsAuthentication && !needsMiningPool && (
        <ProtoFleetStatusModal show={isStatusModalOpen} onClose={handleModalClose} deviceId={deviceIdentifier} />
      )}
    </div>
  );
};

export default MinerName;
