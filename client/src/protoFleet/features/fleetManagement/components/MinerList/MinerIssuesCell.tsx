import { useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import MinerIssues from "./MinerIssues";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { ProtoFleetStatusModal } from "@/protoFleet/components/StatusModal";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import { useMiner, useMinerDeviceStatus } from "@/protoFleet/store";

type MinerIssuesCellProps = {
  deviceIdentifier: string;
};

/**
 * MinerIssuesCell wraps the MinerIssues component and handles the modal state.
 * For miners that need authentication, shows the authenticate miners UI directly.
 * For miners that need a mining pool, shows Fleet auth modal first, then pool selection.
 * For other issues (hardware errors), shows the status modal.
 */
const MinerIssuesCell = ({ deviceIdentifier }: MinerIssuesCellProps) => {
  const [isModalOpen, setModalOpen] = useState(false);
  const [showFleetAuth, setShowFleetAuth] = useState(false);
  const [showPoolSelection, setShowPoolSelection] = useState(false);
  const [fleetCredentials, setFleetCredentials] = useState<{ username: string; password: string }>();
  const miner = useMiner(deviceIdentifier);
  const deviceStatus = useMinerDeviceStatus(deviceIdentifier);

  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;

  const handleIssuesClick = () => {
    setModalOpen(true);
    if (needsMiningPool) {
      setShowFleetAuth(true);
    }
  };

  const handleModalClose = () => {
    setModalOpen(false);
    setShowFleetAuth(false);
    setShowPoolSelection(false);
    setFleetCredentials(undefined);
  };

  const handleFleetAuthenticated = (username: string, password: string) => {
    setFleetCredentials({ username, password });
    setShowFleetAuth(false);
    setShowPoolSelection(true);
  };

  return (
    <>
      <MinerIssues deviceIdentifier={deviceIdentifier} onClick={handleIssuesClick} />
      {isModalOpen && needsAuthentication && <AuthenticateMiners onClose={handleModalClose} />}
      {isModalOpen && !needsAuthentication && needsMiningPool && showFleetAuth && (
        <AuthenticateFleetModal
          show={showFleetAuth}
          onAuthenticated={handleFleetAuthenticated}
          onDismiss={handleModalClose}
        />
      )}
      {isModalOpen && !needsAuthentication && needsMiningPool && showPoolSelection && fleetCredentials && (
        <PoolSelectionPageWrapper
          selectedMiners={[{ deviceIdentifier, deviceStatus }]}
          selectionMode="subset"
          userUsername={fleetCredentials.username}
          userPassword={fleetCredentials.password}
          onSuccess={handleModalClose}
          onError={handleModalClose}
          onDismiss={handleModalClose}
        />
      )}
      {isModalOpen && !needsAuthentication && !needsMiningPool && (
        <ProtoFleetStatusModal show={isModalOpen} onClose={handleModalClose} deviceId={deviceIdentifier} />
      )}
    </>
  );
};

export default MinerIssuesCell;
