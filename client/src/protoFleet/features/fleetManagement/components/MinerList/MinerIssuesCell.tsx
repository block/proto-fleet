import { useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import MinerIssues from "./MinerIssues";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { ProtoFleetStatusModal } from "@/protoFleet/components/StatusModal";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import { useMiner, useMinerDeviceStatus } from "@/protoFleet/store";

type MinerIssuesCellProps = {
  deviceIdentifier: string;
};

/**
 * MinerIssuesCell wraps the MinerIssues component and handles the modal state.
 * For miners that need authentication, shows the authenticate miners UI directly.
 * For miners that need a mining pool, shows the pool selection UI directly.
 * For other issues (hardware errors), shows the status modal.
 */
const MinerIssuesCell = ({ deviceIdentifier }: MinerIssuesCellProps) => {
  const [isModalOpen, setModalOpen] = useState(false);
  const miner = useMiner(deviceIdentifier);
  const deviceStatus = useMinerDeviceStatus(deviceIdentifier);

  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;

  const handleIssuesClick = () => {
    setModalOpen(true);
  };

  const handleModalClose = () => {
    setModalOpen(false);
  };

  return (
    <>
      <MinerIssues deviceIdentifier={deviceIdentifier} onClick={handleIssuesClick} />
      {isModalOpen && needsAuthentication && <AuthenticateMiners onClose={handleModalClose} />}
      {isModalOpen && !needsAuthentication && needsMiningPool && (
        <PoolSelectionPageWrapper
          selectedMiners={[{ deviceIdentifier, deviceStatus }]}
          selectionMode="subset"
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
